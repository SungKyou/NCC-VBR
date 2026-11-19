// Package vmtest 는 vbr_vm 시험 도구다: 가짜 XAPI 서버와 시험용 XVA 파일.
// 1단계 go/internal/agent/xen_test.go 의 fakeXAPI 를 넓혔다(전원 상태, 느린 내보내기, 취소, 결과 VM).
package vmtest

import (
	"archive/tar"
	"bytes"
	"crypto/sha1" //nolint:gosec // XVA 형식
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
)

// 시험에 쓰는 uuid·참조.
const (
	VMUUID    = "42097417-b9ee-2f97-8eeb-3612efbcb25d"
	SnapUUID  = "676ffe7e-c3f4-8913-f0ed-e39eb31d0d7a"
	NewVMUUID = "cd5498f2-0342-db8e-749e-53a5e86744dd"
	SRUUID    = "e4138437-7e20-480f-08b1-25e5b8b6027b"
	HostUUID  = "9ad088e0-d03d-0269-0b7f-e40006aa5014"
	Password  = "vbr-Secret-#1" // 시험이 로그·기록·응답에서 이 문자열을 찾는다(남으면 안 된다)
	newVMRef  = "OpaqueRef:a1b2c3d4-0000-1111-2222-333344445555"
)

// Fake 는 시험용 XAPI 서버다. 호출을 기록한다.
type Fake struct {
	Server *httptest.Server

	mu          sync.Mutex
	calls       []string
	destroyed   map[string]bool
	PowerState  string        // VM.get_record 의 power_state(기본 halted)
	ExportBody  []byte        // /export 응답(기본 XVA())
	ExportHTTP  int           // 0 이 아니면 /export 가 이 상태로 실패
	TaskStatus  string        // task.get_status(기본 success)
	SlowExport  chan struct{} // nil 이 아니면 /export 가 이 채널이 닫히거나 요청이 끊길 때까지 기다린다
	ExportStart chan struct{} // nil 이 아니면 /export 를 받으면 한 번 닫는다
	Imported    []byte
	ImportQuery string
	canceled    bool
	startOnce   sync.Once
}

// New 는 TLS 가짜 서버를 띄운다. 끝나면 Close 한다.
func New() *Fake {
	f := &Fake{destroyed: map[string]bool{}, PowerState: "halted", TaskStatus: "success"}
	f.ExportBody = XVA()
	f.Server = httptest.NewTLSServer(f)
	return f
}

// Close 는 서버를 닫는다.
func (f *Fake) Close() { f.Server.Close() }

// Addr 는 masterIp 로 쓸 "호스트:포트" 다.
func (f *Fake) Addr() string { return strings.TrimPrefix(f.Server.URL, "https://") }

// Calls 는 기록된 호출이다(세션·task 조회 제외).
func (f *Fake) Calls() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, "\n")
}

// Destroyed 는 지운 참조 목록이다.
func (f *Fake) Destroyed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for k := range f.destroyed {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var vbdRecords = map[string]map[string]any{
	"OpaqueRef:svbd0": {"uuid": "svbd0", "type": "Disk", "userdevice": "0", "VDI": "OpaqueRef:svdi0"},
	"OpaqueRef:svbd1": {"uuid": "svbd1", "type": "Disk", "userdevice": "1", "VDI": "OpaqueRef:svdi1"},
	"OpaqueRef:svbd3": {"uuid": "svbd3", "type": "CD", "userdevice": "3", "VDI": "OpaqueRef:NULL"},
}

func (f *Fake) rpc(method string, args []string) (any, string) {
	switch method {
	case "session.login_with_password":
		if len(args) < 2 || args[1] != Password {
			return nil, "SESSION_AUTHENTICATION_FAILED|" + args[0] + "|Authentication failure"
		}
		return "OpaqueRef:sess", ""
	case "session.logout":
		return "", ""
	case "VM.get_by_uuid":
		switch args[1] {
		case VMUUID:
			return "OpaqueRef:vm", ""
		case SnapUUID:
			return "OpaqueRef:snap", ""
		}
		return nil, "UUID_INVALID|VM|" + args[1]
	case "VM.snapshot":
		return "OpaqueRef:snap", ""
	case "VM.get_record":
		switch args[1] {
		case "OpaqueRef:vm":
			return map[string]any{"uuid": VMUUID, "power_state": f.PowerState, "is_a_snapshot": false, "name_label": "vm1"}, ""
		case "OpaqueRef:snap":
			return map[string]any{"uuid": SnapUUID, "power_state": "halted", "is_a_snapshot": true, "name_label": "snap"}, ""
		case newVMRef:
			return map[string]any{"uuid": NewVMUUID, "is_a_template": true}, ""
		}
		return nil, "HANDLE_INVALID|VM|" + args[1]
	case "VM.get_VBDs":
		return []string{"OpaqueRef:svbd0", "OpaqueRef:svbd1", "OpaqueRef:svbd3"}, ""
	case "VBD.get_record":
		return vbdRecords[args[1]], ""
	case "VDI.get_is_a_snapshot":
		if f.destroyed[args[1]] {
			return nil, "HANDLE_INVALID|VDI|" + args[1]
		}
		return true, ""
	case "VDI.destroy", "VM.destroy":
		f.destroyed[args[1]] = true
		return "", ""
	case "task.create":
		return "OpaqueRef:task", ""
	case "task.get_status":
		if f.canceled {
			return "cancelled", ""
		}
		return f.TaskStatus, ""
	case "task.get_progress":
		return "0.5", ""
	case "task.get_error_info":
		return []string{"VDI_IO_ERROR", "Device I/O errors"}, ""
	case "task.get_result":
		return "<value><array><data><value>" + newVMRef + "</value></data></array></value>", ""
	case "task.cancel":
		f.canceled = true
		return "", ""
	case "SR.get_by_uuid":
		if args[1] != SRUUID {
			return nil, "UUID_INVALID|SR|" + args[1]
		}
		return "OpaqueRef:sr", ""
	case "host.get_by_uuid":
		if args[1] != HostUUID {
			return nil, "UUID_INVALID|host|" + args[1]
		}
		return "OpaqueRef:host", ""
	case "VM.set_is_a_template", "VM.set_ha_always_run", "VBD.remove_from_other_config",
		"VBD.add_to_other_config", "task.destroy", "VM.update_allowed_operations",
		"VM.set_name_label", "VM.set_affinity":
		return "", ""
	}
	return nil, "MESSAGE_METHOD_UNKNOWN|" + method
}

type xmlCall struct {
	Method string `xml:"methodName"`
	Params []struct {
		Value struct {
			String *string `xml:"string"`
			Bool   *string `xml:"boolean"`
		} `xml:"value"`
	} `xml:"params>param"`
}

// ServeHTTP 는 XML-RPC 와 /export, /import 를 받는다.
func (f *Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/export":
		f.serveExport(w, r)
		return
	case "/import":
		data, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, "HTTP import "+r.URL.Query().Get("restore"))
		f.ImportQuery = r.URL.RawQuery
		f.Imported = data
		f.mu.Unlock()
		return
	}
	var c xmlCall
	body, _ := io.ReadAll(r.Body)
	if err := xml.Unmarshal(body, &c); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	var args []string
	for _, p := range c.Params {
		switch {
		case p.Value.String != nil:
			args = append(args, *p.Value.String)
		case p.Value.Bool != nil:
			args = append(args, *p.Value.Bool)
		default:
			args = append(args, "")
		}
	}
	f.mu.Lock()
	if !strings.HasPrefix(c.Method, "session.") && !strings.HasPrefix(c.Method, "task.get_") {
		rest := ""
		if len(args) > 1 {
			rest = strings.Join(args[1:], " ")
		}
		f.calls = append(f.calls, strings.TrimSpace(c.Method+" "+rest))
	}
	val, fail := f.rpc(c.Method, args)
	f.mu.Unlock()
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0"?><methodResponse><params><param><value><struct>`)
	if fail != "" {
		sb.WriteString(`<member><name>Status</name><value><string>Failure</string></value></member>`)
		sb.WriteString(`<member><name>ErrorDescription</name><value>`)
		writeXMLValue(&sb, strings.Split(fail, "|"))
	} else {
		sb.WriteString(`<member><name>Status</name><value><string>Success</string></value></member>`)
		sb.WriteString(`<member><name>Value</name><value>`)
		writeXMLValue(&sb, val)
	}
	sb.WriteString(`</value></member></struct></value></param></params></methodResponse>`)
	_, _ = io.WriteString(w, sb.String())
}

func (f *Fake) serveExport(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls = append(f.calls, "HTTP export "+r.URL.Query().Get("uuid")+" gzip="+r.URL.Query().Get("use_compression"))
	status, body, slow, start := f.ExportHTTP, f.ExportBody, f.SlowExport, f.ExportStart
	f.mu.Unlock()
	if start != nil {
		f.startOnce.Do(func() { close(start) })
	}
	if status != 0 {
		http.Error(w, "VDI_IO_ERROR", status)
		return
	}
	if slow != nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body[:len(body)/2])
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		select {
		case <-slow:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write(body[len(body)/2:])
		return
	}
	_, _ = w.Write(body)
}

func writeXMLValue(sb *strings.Builder, v any) {
	switch x := v.(type) {
	case string:
		sb.WriteString("<string>")
		_ = xml.EscapeText(sb, []byte(x))
		sb.WriteString("</string>")
	case bool:
		if x {
			sb.WriteString("<boolean>1</boolean>")
		} else {
			sb.WriteString("<boolean>0</boolean>")
		}
	case []string:
		sb.WriteString("<array><data>")
		for _, s := range x {
			sb.WriteString("<value>")
			writeXMLValue(sb, s)
			sb.WriteString("</value>")
		}
		sb.WriteString("</data></array>")
	case map[string]any:
		sb.WriteString("<struct>")
		for k, val := range x {
			fmt.Fprintf(sb, "<member><name>%s</name><value>", k)
			writeXMLValue(sb, val)
			sb.WriteString("</value></member>")
		}
		sb.WriteString("</struct>")
	default:
		sb.WriteString("<string></string>")
	}
}

// XVA 는 검증을 통과하는 작은 XVA(tar: ova.xml, 청크 하나와 그 .checksum)다.
func XVA() []byte { return xvaWith(false) }

// BadXVA 는 청크 체크섬이 틀린 XVA 다.
func BadXVA() []byte { return xvaWith(true) }

func xvaWith(bad bool) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	add := func(name string, data []byte) {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))})
		_, _ = tw.Write(data)
	}
	chunk := bytes.Repeat([]byte("vbr-vm-chunk "), 4096)
	sum := sha1.Sum(chunk) //nolint:gosec // XVA 형식
	hexSum := hex.EncodeToString(sum[:])
	if bad {
		hexSum = strings.Repeat("0", 40)
	}
	add("ova.xml", []byte("<value><struct></struct></value>"))
	add("Ref:1/00000000", chunk)
	add("Ref:1/00000000.checksum", []byte(hexSum))
	_ = tw.Close()
	return buf.Bytes()
}
