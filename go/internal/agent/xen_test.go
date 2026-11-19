package agent

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"ncc-vbr/internal/logx"
)

// fakeXAPI 는 시험용 XAPI 서버다. 호출 순서를 기록한다.
type fakeXAPI struct {
	mu        sync.Mutex
	calls     []string
	destroyed map[string]bool
	exportErr bool
	exportGot string
	importQ   string
	imported  []byte
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

const (
	snapUUID = "676ffe7e-c3f4-8913-f0ed-e39eb31d0d7a"
	newUUID  = "cd5498f2-0342-db8e-749e-53a5e86744dd"
	srUUID   = "9ad088e0-d03d-0269-0b7f-e40006aa5014"
)

var vbdRecords = map[string]map[string]any{
	"OpaqueRef:svbd0": {"uuid": "svbd0", "type": "Disk", "userdevice": "0", "VDI": "OpaqueRef:svdi0"},
	"OpaqueRef:svbd1": {"uuid": "svbd1", "type": "Disk", "userdevice": "1", "VDI": "OpaqueRef:svdi1"},
	"OpaqueRef:svbd3": {"uuid": "svbd3", "type": "CD", "userdevice": "3", "VDI": "OpaqueRef:NULL"},
}

func (f *fakeXAPI) rpc(method string, args []string) (any, string) {
	switch method {
	case "session.login_with_password":
		return "OpaqueRef:sess", ""
	case "session.get_this_host":
		return "OpaqueRef:host", ""
	case "VM.get_by_uuid":
		if args[1] != vmUUID {
			return nil, "UUID_INVALID|VM|" + args[1]
		}
		return "OpaqueRef:vm", ""
	case "VM.snapshot":
		return "OpaqueRef:snap", ""
	case "VM.get_record":
		switch args[1] {
		case "OpaqueRef:snap":
			return map[string]any{"uuid": snapUUID, "is_a_template": true}, ""
		case "OpaqueRef:a1b2c3d4-0000-1111-2222-333344445555":
			return map[string]any{"uuid": newUUID, "is_a_template": true}, ""
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
		return "success", ""
	case "task.get_result":
		return "<value><array><data><value>OpaqueRef:a1b2c3d4-0000-1111-2222-333344445555</value></data></array></value>", ""
	case "SR.get_by_uuid":
		return "OpaqueRef:sr", ""
	case "VM.set_is_a_template", "VM.set_ha_always_run", "VBD.remove_from_other_config",
		"VBD.add_to_other_config", "task.destroy", "task.cancel", "VM.update_allowed_operations":
		return "", ""
	}
	return nil, "MESSAGE_METHOD_UNKNOWN|" + method
}

func (f *fakeXAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/export":
		f.mu.Lock()
		f.calls = append(f.calls, "HTTP export "+r.URL.Query().Get("uuid")+" compression="+r.URL.Query().Get("use_compression"))
		f.exportGot = r.URL.Query().Get("session_id")
		fail := f.exportErr
		f.mu.Unlock()
		if fail {
			http.Error(w, "VDI_IO_ERROR", http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, "XVA-DATA")
		return
	case "/import":
		data, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, "HTTP import")
		f.importQ = r.URL.RawQuery
		f.imported = data
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
	if c.Method != "session.get_this_host" && !strings.HasPrefix(c.Method, "task.get_") {
		f.calls = append(f.calls, strings.TrimSpace(c.Method+" "+strings.Join(args[1:], " ")))
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

func newXenHarness(t *testing.T) (*fakeXAPI, *Xen, *StepLog, string) {
	t.Helper()
	f := &fakeXAPI{destroyed: map[string]bool{}}
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	log := logx.New(dir)
	cfg := Settings{XAPIAddress: srv.URL, XAPIUser: "root", BackupRoots: []string{dir}}
	return f, NewXen(cfg, "pw", log), &StepLog{log: log}, dir
}

func (f *fakeXAPI) joined() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, "\n")
}

// T-AG-U20 백업: 원본 단계 순서와 스냅샷 디스크 정리(NX-02)
func TestXenBackup(t *testing.T) {
	f, x, step, dir := newXenHarness(t)
	file := filepath.Join(dir, "17.xva")
	if err := os.WriteFile(file, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := x.Backup(context.Background(), BackupRequest{VMUUID: vmUUID, SnapshotName: "17", FileName: file}, step)
	if err != nil || snap != snapUUID {
		t.Fatalf("backup: %q %v", snap, err)
	}
	want := strings.Join([]string{
		"session.login_with_password pw 1.0 ncc-vbr-agent", // 첫 인자(root)는 세션 자리로 보고 기록하지 않았다
		"VM.get_by_uuid " + vmUUID,
		"VM.snapshot OpaqueRef:vm 17",
		"VM.get_record OpaqueRef:snap",
		"VM.set_is_a_template OpaqueRef:snap 0",
		"VM.set_ha_always_run OpaqueRef:snap 0",
		"task.create NCC-VBR VM Export vbr_agent export " + snapUUID,
		"HTTP export " + snapUUID + " compression=true",
		"task.destroy OpaqueRef:task",
		"VM.get_VBDs OpaqueRef:snap",
		"VBD.get_record OpaqueRef:svbd0",
		"VBD.get_record OpaqueRef:svbd1",
		"VBD.get_record OpaqueRef:svbd3",
		"VDI.get_is_a_snapshot OpaqueRef:svdi0",
		"VDI.destroy OpaqueRef:svdi0",
		"VDI.get_is_a_snapshot OpaqueRef:svdi1",
		"VDI.destroy OpaqueRef:svdi1",
		"VM.destroy OpaqueRef:snap",
	}, "\n")
	got := f.joined()
	if got != want {
		t.Fatalf("calls\n got:\n%s\nwant:\n%s", got, want)
	}
	data, _ := os.ReadFile(file)
	if string(data) != "XVA-DATA" {
		t.Fatalf("file %q", data)
	}
	if _, err := os.Stat(file + ".partial"); !os.IsNotExist(err) {
		t.Fatal(".partial must not remain")
	}
	logData, _ := os.ReadFile(filepath.Join(dir, "backup.log"))
	for _, s := range []string{"]SNAPSHOT create: VM-UUID=" + vmUUID + " filename=" + file + "\n\tVM Snapshot success, uuid=" + snapUUID,
		"]SNAPSHOT VM property Change: VM-UUID=" + snapUUID, "]VM BACKUP: VM-UUID=" + snapUUID, "\tVM export succeeded.",
		"]SNAPSHOT destroy: VM-UUID=" + snapUUID} {
		if !strings.Contains(string(logData), s) {
			t.Errorf("backup.log missing %q\n%s", s, logData)
		}
	}
}

// T-AG-U21 부분 백업: 자리 값 0 인 디스크를 내보내기 전에 지운다
func TestXenPartialBackup(t *testing.T) {
	f, x, step, dir := newXenHarness(t)
	file := filepath.Join(dir, "p.xva")
	_, err := x.Backup(context.Background(), BackupRequest{VMUUID: vmUUID, SnapshotName: "p", FileName: file,
		Partial: true, DiskSeq: "100000000000000"}, step)
	if err != nil {
		t.Fatal(err)
	}
	calls := f.joined()
	remove := strings.Index(calls, "VDI.destroy OpaqueRef:svdi1")
	export := strings.Index(calls, "HTTP export")
	if remove < 0 || export < 0 || remove > export {
		t.Fatalf("disk 1 must be removed before export:\n%s", calls)
	}
	if !strings.Contains(calls, "VBD.remove_from_other_config OpaqueRef:svbd1 owner\nVBD.add_to_other_config OpaqueRef:svbd1 owner false") {
		t.Fatalf("owner=false not set:\n%s", calls)
	}
	if strings.Count(calls, "VDI.destroy OpaqueRef:svdi1") != 1 || !strings.Contains(calls, "VDI.destroy OpaqueRef:svdi0") {
		t.Fatalf("cleanup must destroy remaining snapshot disk only once each:\n%s", calls)
	}
}

// T-AG-U22 내보내기 실패: 파일을 남기지 않고 스냅샷을 정리한다
func TestXenExportFailure(t *testing.T) {
	f, x, step, dir := newXenHarness(t)
	f.exportErr = true
	file := filepath.Join(dir, "e.xva")
	_, err := x.Backup(context.Background(), BackupRequest{VMUUID: vmUUID, SnapshotName: "e", FileName: file}, step)
	if err == nil || !strings.HasPrefix(err.Error(), "[ERROR]export HTTP 500") {
		t.Fatalf("err %v", err)
	}
	if _, serr := os.Stat(file); !os.IsNotExist(serr) {
		t.Fatal("file must not exist")
	}
	if !f.destroyed["OpaqueRef:snap"] || !f.destroyed["OpaqueRef:svdi0"] {
		t.Fatalf("snapshot must be cleaned:\n%s", f.joined())
	}
}

// T-AG-U23 스냅샷 실패: XAPI 오류 문장을 [ERROR] 뒤에 붙인다
func TestXenSnapshotFailure(t *testing.T) {
	f, x, step, dir := newXenHarness(t)
	_, err := x.Backup(context.Background(), BackupRequest{VMUUID: "00000000-0000-0000-0000-000000000000",
		SnapshotName: "s", FileName: filepath.Join(dir, "s.xva")}, step)
	if err == nil || err.Error() != "[ERROR]XAPI Error: UUID_INVALID | VM | 00000000-0000-0000-0000-000000000000" {
		t.Fatalf("err %v", err)
	}
	if len(f.destroyed) != 0 {
		t.Fatal("nothing to clean")
	}
}

// T-AG-U24 복구: preserve 가 restore 파라미터로, 결과는 새 VM uuid, 템플릿 플래그 후처리
func TestXenRestore(t *testing.T) {
	f, x, step, dir := newXenHarness(t)
	file := filepath.Join(dir, "r.xva")
	if err := os.WriteFile(file, []byte("XVA-IN"), 0o644); err != nil {
		t.Fatal(err)
	}
	uuid, err := x.Restore(context.Background(), RestoreRequest{FileName: file, Preserve: true, SRUUID: srUUID}, step)
	if err != nil || uuid != newUUID {
		t.Fatalf("restore %q %v", uuid, err)
	}
	if string(f.imported) != "XVA-IN" || !strings.Contains(f.importQ, "restore=true") || !strings.Contains(f.importQ, "sr_id=OpaqueRef%3Asr") {
		t.Fatalf("import %q %q", f.imported, f.importQ)
	}
	if !strings.Contains(f.joined(), "VM.set_is_a_template OpaqueRef:a1b2c3d4-0000-1111-2222-333344445555 0\nVM.update_allowed_operations OpaqueRef:a1b2c3d4-0000-1111-2222-333344445555") {
		t.Fatalf("template fix missing:\n%s", f.joined())
	}
	logData, _ := os.ReadFile(filepath.Join(dir, "backup.log"))
	if !strings.Contains(string(logData), "]VM IMPORT: SR-UUID="+srUUID+" filename="+file+"\n\tVM import success, vm-uuid="+newUUID) {
		t.Fatalf("backup.log %s", logData)
	}
}
