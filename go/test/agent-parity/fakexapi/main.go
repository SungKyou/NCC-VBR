// fakexapi 는 agent 비교 시험용 가짜 XAPI 서버다. fake-bin/xe 와 같은 시험 자료를 낸다.
//
//	fakexapi -listen :8443 -xva /p/xva-template.tar.gz -log /p/xapi.log
package main

import (
	"encoding/hex"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	vmUUID   = "22acc256-b431-f9df-6c44-b1f71e7912fc"
	newUUID  = "cd5498f2-0342-db8e-749e-53a5e86744dd"
	newVMRef = "OpaqueRef:a1b2c3d4-0000-1111-2222-333344445555"
)

type server struct {
	mu      sync.Mutex
	xva     []byte
	logFile *os.File
	names   map[string]string // 스냅샷 uuid → 이름
	refs    map[string]string // 스냅샷 ref → uuid
	tasks   map[string]bool   // 취소된 task
	n       int
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

// snapUUID 는 fake-bin/xe 의 hexname 과 같은 규칙이다.
func snapUUID(name string) string {
	h := hex.EncodeToString([]byte(name))
	if len(h) > 12 {
		h = h[:12]
	}
	return "676ffe7e-c3f4-8913-f0ed-" + h + strings.Repeat("0", 12-len(h))
}

func delay(name string) time.Duration {
	switch name {
	case "slow":
		return 12 * time.Second
	}
	return 0
}

func (s *server) record(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintln(s.logFile, line)
}

func (s *server) rpc(method string, a []string) (any, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	arg := func(i int) string {
		if i < len(a) {
			return a[i]
		}
		return ""
	}
	switch method {
	case "session.login_with_password":
		return "OpaqueRef:0000aaaa-0000-0000-0000-000000000001", ""
	case "session.get_this_host":
		return "OpaqueRef:0000bbbb-0000-0000-0000-000000000001", ""
	case "VM.get_by_uuid":
		if arg(1) != vmUUID {
			return nil, "UUID_INVALID|VM|" + arg(1)
		}
		return "OpaqueRef:0000cccc-0000-0000-0000-000000000001", ""
	case "VM.snapshot":
		u := snapUUID(arg(2))
		s.n++
		ref := fmt.Sprintf("OpaqueRef:5aa5%04d-0000-0000-0000-000000000000", s.n)
		s.refs[ref] = u
		s.names[u] = arg(2)
		return ref, ""
	case "VM.get_record":
		if u, ok := s.refs[arg(1)]; ok {
			return map[string]any{"uuid": u, "is_a_template": true}, ""
		}
		if arg(1) == newVMRef {
			return map[string]any{"uuid": newUUID, "is_a_template": false}, ""
		}
		return nil, "HANDLE_INVALID|VM|" + arg(1)
	case "VM.get_VBDs":
		return []string{"OpaqueRef:b0000000-0000-0000-0000-000000000000", "OpaqueRef:b0000000-0000-0000-0000-000000000001"}, ""
	case "VBD.get_record":
		d := arg(1)[len(arg(1))-1:]
		return map[string]any{"uuid": "5b0d6a66-0000-4000-8000-00000000000" + d, "type": "Disk", "userdevice": d,
			"VDI": "OpaqueRef:d0000000-0000-0000-0000-00000000000" + d}, ""
	case "VDI.get_is_a_snapshot":
		return true, ""
	case "task.create":
		s.n++
		return fmt.Sprintf("OpaqueRef:7a5c%04d-0000-0000-0000-000000000000", s.n), ""
	case "task.get_status":
		if s.tasks[arg(1)] {
			return "cancelled", ""
		}
		return "success", ""
	case "task.get_error_info":
		return []string{"TASK_CANCELLED"}, ""
	case "task.cancel":
		s.tasks[arg(1)] = true
		return "", ""
	case "task.get_result":
		return "<value><array><data><value>" + newVMRef + "</value></data></array></value>", ""
	case "SR.get_by_uuid":
		return "OpaqueRef:5r000000-0000-0000-0000-000000000000", ""
	case "VM.set_is_a_template", "VM.set_ha_always_run", "VBD.remove_from_other_config", "VBD.add_to_other_config",
		"VDI.destroy", "VM.destroy", "task.destroy", "VM.update_allowed_operations":
		return "", ""
	}
	return nil, "MESSAGE_METHOD_UNKNOWN|" + method
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/export":
		u := r.URL.Query().Get("uuid")
		s.mu.Lock()
		name := s.names[u]
		s.mu.Unlock()
		s.record("HTTP export uuid=" + u + " use_compression=" + r.URL.Query().Get("use_compression"))
		time.Sleep(delay(name))
		_, _ = w.Write(s.xva)
		return
	case "/import":
		n, _ := io.Copy(io.Discard, r.Body)
		s.record(fmt.Sprintf("HTTP import restore=%s bytes=%d", r.URL.Query().Get("restore"), n))
		return
	}
	body, _ := io.ReadAll(r.Body)
	var c xmlCall
	if err := xml.Unmarshal(body, &c); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	if !strings.HasPrefix(c.Method, "session.") && !strings.HasPrefix(c.Method, "task.get_") {
		s.record(c.Method + " " + strings.Join(args[1:], " "))
	}
	val, fail := s.rpc(c.Method, args)
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0"?><methodResponse><params><param><value><struct>`)
	if fail != "" {
		sb.WriteString(`<member><name>Status</name><value><string>Failure</string></value></member><member><name>ErrorDescription</name><value>`)
		writeValue(&sb, strings.Split(fail, "|"))
	} else {
		sb.WriteString(`<member><name>Status</name><value><string>Success</string></value></member><member><name>Value</name><value>`)
		writeValue(&sb, val)
	}
	sb.WriteString(`</value></member></struct></value></param></params></methodResponse>`)
	_, _ = io.WriteString(w, sb.String())
}

func writeValue(sb *strings.Builder, v any) {
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
		for _, e := range x {
			sb.WriteString("<value>")
			writeValue(sb, e)
			sb.WriteString("</value>")
		}
		sb.WriteString("</data></array>")
	case map[string]any:
		sb.WriteString("<struct>")
		for k, e := range x {
			fmt.Fprintf(sb, "<member><name>%s</name><value>", k)
			writeValue(sb, e)
			sb.WriteString("</value></member>")
		}
		sb.WriteString("</struct>")
	default:
		sb.WriteString("<string></string>")
	}
}

func main() {
	listen := flag.String("listen", ":8443", "listen address")
	xvaPath := flag.String("xva", "/p/xva-template.tar.gz", "xva bytes for export")
	logPath := flag.String("log", "/p/xapi.log", "call log")
	flag.Parse()
	xva, err := os.ReadFile(*xvaPath)
	if err != nil {
		log.Fatal(err)
	}
	lf, err := os.Create(*logPath)
	if err != nil {
		log.Fatal(err)
	}
	s := &server{xva: xva, logFile: lf, names: map[string]string{}, refs: map[string]string{}, tasks: map[string]bool{}}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(s)
	srv.Listener = ln
	srv.StartTLS()
	log.Printf("fake XAPI listening on %s", srv.URL)
	select {}
}
