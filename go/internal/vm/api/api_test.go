package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"ncc-vbr/internal/jobstore"
	"ncc-vbr/internal/secret"
	"ncc-vbr/internal/vm"
	"ncc-vbr/internal/vm/vmtest"
	"ncc-vbr/internal/vmconfig"
)

const token = "test-token"

// cfgSource 는 시험에서 바꿔 끼울 수 있는 설정이다.
type cfgSource struct {
	p atomic.Pointer[vmconfig.Loaded]
}

func (c *cfgSource) Current() *vmconfig.Loaded { return c.p.Load() }

func (c *cfgSource) update(fn func(l *vmconfig.Loaded)) {
	cp := *c.p.Load()
	fn(&cp)
	c.p.Store(&cp)
}

type harness struct {
	t     *testing.T
	fake  *vmtest.Fake
	srv   *httptest.Server
	svc   *vm.Service
	store *jobstore.Store
	cfg   *cfgSource
	dir   string
}

func newHarness(t *testing.T, tune func(l *vmconfig.Loaded)) *harness {
	t.Helper()
	dir := t.TempDir()
	l := &vmconfig.Loaded{}
	l.Server.Port = 8090
	l.APIAuth.Enabled = true
	l.APIToken = token
	l.Locale = "ko"
	l.Backup.BasePath = filepath.Join(dir, "backup")
	l.Backup.TimeoutHours = 1
	l.Concurrency.MaxJobs = 2
	l.Concurrency.MaxQueue = 100
	l.XAPI.ProgressIntervalSec = 1
	l.TaskStore.Dir = filepath.Join(dir, "tasks")
	l.TaskStore.RetentionDays = 30
	if tune != nil {
		tune(l)
	}
	cs := &cfgSource{}
	cs.p.Store(l)
	store, _, err := jobstore.Open(l.TaskStore.Dir, 30*24*time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := vmtest.New()
	svc := vm.New(cs, store, nil)
	svc.Start()
	srv := httptest.NewServer(NewRouter(svc, cs))
	h := &harness{t: t, fake: fake, srv: srv, svc: svc, store: store, cfg: cs, dir: dir}
	t.Cleanup(func() {
		svc.Shutdown(5 * time.Second)
		srv.Close()
		fake.Close()
		_ = store.Close()
	})
	return h
}

func (h *harness) do(method, path string, body interface{}, auth bool) (int, map[string]interface{}) {
	h.t.Helper()
	var rd io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			rd = strings.NewReader(b)
		default:
			j, _ := json.Marshal(b)
			rd = bytes.NewReader(j)
		}
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, rd)
	if auth {
		req.Header.Set("X-Internal-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]interface{}
	_ = json.Unmarshal(raw, &m)
	return resp.StatusCode, m
}

func (h *harness) password() string { return vmtest.Password }

// job 은 요청 본문을 만든다.
func (h *harness) job(id, action string, opts map[string]interface{}) map[string]interface{} {
	p := map[string]interface{}{"options": opts}
	switch action {
	case vm.ActionExport:
		p["targetType"] = "VM"
		p["targetUUID"] = vmtest.VMUUID
	default:
		p["targetType"] = "FILE"
	}
	if action != vm.ActionVerify {
		p["masterIp"] = h.fake.Addr()
		p["credentials"] = map[string]interface{}{"username": "root", "password": h.password()}
	}
	return map[string]interface{}{"taskId": id, "jobType": "VM_BACKUP", "action": action, "payload": p}
}

func (h *harness) submit(id, action string, opts map[string]interface{}) {
	h.t.Helper()
	code, body := h.do("POST", "/api/v1/jobs", h.job(id, action, opts), true)
	if code != http.StatusAccepted || body["status"] != "ACCEPTED" || body["taskId"] != id {
		h.t.Fatalf("submit %s: %d %v", id, code, body)
	}
}

// wait 는 끝날 때까지(또는 want 상태가 될 때까지) 조회한다.
func (h *harness) wait(id string, want ...string) map[string]interface{} {
	h.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		_, body := h.do("GET", "/api/v1/jobs/"+id, nil, true)
		st, _ := body["status"].(string)
		for _, w := range want {
			if st == w {
				return body
			}
		}
		if len(want) == 0 && jobstore.Finished(st) {
			return body
		}
		time.Sleep(50 * time.Millisecond)
	}
	h.t.Fatalf("task %s did not reach %v", id, want)
	return nil
}

func opts(body map[string]interface{}) map[string]interface{} {
	p, _ := body["payload"].(map[string]interface{})
	o, _ := p["options"].(map[string]interface{})
	return o
}

func errCode(body map[string]interface{}) string {
	e, _ := body["error"].(map[string]interface{})
	c, _ := e["code"].(string)
	return c
}

func errDetail(body map[string]interface{}) string {
	e, _ := body["error"].(map[string]interface{})
	c, _ := e["detail"].(string)
	return c
}

// T-VM-U70, U80: /health 는 인증 없이, /api/v1 은 토큰이 있어야 한다.
func TestHealthAndAuth(t *testing.T) {
	h := newHarness(t, nil)
	if code, body := h.do("GET", "/health", nil, false); code != 200 || body["version"] != Version || body["status"] != "ok" {
		t.Fatalf("health: %d %v", code, body)
	}
	code, body := h.do("GET", "/api/v1/jobs", nil, false)
	if code != 401 || body["error"] != "unauthorized: missing or invalid X-Internal-Token" {
		t.Fatalf("no token: %d %v", code, body)
	}
	if code, _ := h.do("GET", "/api/v1/jobs", nil, true); code != 200 {
		t.Fatalf("with token: %d", code)
	}
	h.cfg.update(func(l *vmconfig.Loaded) { l.APIAuth.Enabled = false })
	if code, _ := h.do("GET", "/api/v1/jobs", nil, false); code != 200 {
		t.Fatalf("auth disabled: %d", code)
	}
}

// T-VM-U02, U81: 필수 값·형식 오류는 접수하지 않고 400 + 코드·필드.
func TestSubmitValidation(t *testing.T) {
	h := newHarness(t, nil)
	exp := func(mut func(m map[string]interface{})) map[string]interface{} {
		m := h.job("v1", vm.ActionExport, map[string]interface{}{"file_name": "a"})
		mut(m)
		return m
	}
	pl := func(m map[string]interface{}) map[string]interface{} { return m["payload"].(map[string]interface{}) }
	cases := []struct {
		name  string
		body  interface{}
		code  string
		field string
	}{
		{"not json", "{", vm.CodeInvalidRequest, ""},
		{"no taskId", exp(func(m map[string]interface{}) { delete(m, "taskId") }), vm.CodeMissingField, "taskId"},
		{"bad taskId", exp(func(m map[string]interface{}) { m["taskId"] = "a b" }), vm.CodeInvalidField, "taskId"},
		{"bad jobType", exp(func(m map[string]interface{}) { m["jobType"] = "VM_CREATE" }), vm.CodeUnsupportedJob, "jobType"},
		{"bad action", exp(func(m map[string]interface{}) { m["action"] = "copy" }), vm.CodeUnsupportedJob, "action"},
		{"no payload", exp(func(m map[string]interface{}) { delete(m, "payload") }), vm.CodeMissingField, "payload"},
		{"bad targetType", exp(func(m map[string]interface{}) { pl(m)["targetType"] = "FILE" }), vm.CodeUnsupportedJob, "payload.targetType"},
		{"no targetUUID", exp(func(m map[string]interface{}) { delete(pl(m), "targetUUID") }), vm.CodeMissingField, "payload.targetUUID"},
		{"bad uuid", exp(func(m map[string]interface{}) { pl(m)["targetUUID"] = "x" }), vm.CodeInvalidField, "payload.targetUUID"},
		{"no masterIp", exp(func(m map[string]interface{}) { delete(pl(m), "masterIp") }), vm.CodeMissingField, "payload.masterIp"},
		{"no password", exp(func(m map[string]interface{}) { delete(pl(m), "credentials") }), vm.CodeMissingField, "payload.credentials.password"},
		{"no options", exp(func(m map[string]interface{}) { delete(pl(m), "options") }), vm.CodeMissingField, "payload.options"},
		{"no file_name", exp(func(m map[string]interface{}) { pl(m)["options"] = map[string]interface{}{} }), vm.CodeMissingField, "payload.options.file_name"},
		{"bad file_name", exp(func(m map[string]interface{}) { pl(m)["options"] = map[string]interface{}{"file_name": "a/b"} }), vm.CodeInvalidField, "payload.options.file_name"},
		{"bad compression", exp(func(m map[string]interface{}) {
			pl(m)["options"] = map[string]interface{}{"file_name": "a", "compression": "lz4"}
		}), vm.CodeInvalidField, "payload.options.compression"},
		{"empty disks", exp(func(m map[string]interface{}) {
			pl(m)["options"] = map[string]interface{}{"file_name": "a", "disks": []int{}}
		}), vm.CodeInvalidField, "payload.options.disks"},
		{"bad snapshot_mode", exp(func(m map[string]interface{}) {
			pl(m)["options"] = map[string]interface{}{"file_name": "a", "snapshot_mode": "never"}
		}), vm.CodeInvalidField, "payload.options.snapshot_mode"},
		{"restore no sr", h.job("v2", vm.ActionRestore, map[string]interface{}{"file_path": "a.xva"}), vm.CodeMissingField, "payload.options.sr_uuid"},
		{"verify no file", h.job("v3", vm.ActionVerify, map[string]interface{}{}), vm.CodeMissingField, "payload.options.file_path"},
	}
	for _, c := range cases {
		code, body := h.do("POST", "/api/v1/jobs", c.body, true)
		if code != 400 || body["code"] != c.code || (c.field != "" && body["field"] != c.field) {
			t.Errorf("%s: %d %v, want 400 %s %s", c.name, code, body, c.code, c.field)
		}
	}
	if n := len(h.svc.List(jobstore.Filter{})); n != 0 {
		t.Fatalf("rejected requests must not be recorded: %d", n)
	}
	// crypto.enabled=true 면 평문 암호를 거부한다
	h.cfg.update(func(l *vmconfig.Loaded) { l.Crypto.Enabled = true })
	code, body := h.do("POST", "/api/v1/jobs", h.job("v4", vm.ActionExport, map[string]interface{}{"file_name": "a"}), true)
	if code != 400 || body["code"] != vm.CodePlaintextPassword {
		t.Fatalf("plaintext: %d %v", code, body)
	}
}

// T-VM-U20, U26: Halted VM + auto 는 스냅샷 없이 내보낸다. .partial 뒤 이름 바꿈, SHA256·크기.
func TestExportHaltedAuto(t *testing.T) {
	h := newHarness(t, nil)
	h.submit("e1", vm.ActionExport, map[string]interface{}{"file_name": "vm1"})
	body := h.wait("e1")
	if body["status"] != "succeeded" || body["result"] != "success" || body["percent"].(float64) != 100 {
		t.Fatalf("status: %v", body)
	}
	o := opts(body)
	path := filepath.Join(h.cfg.Current().Backup.BasePath, "vm1.xva")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if o["file_path"] != path || o["checksum_sha256"] != hex.EncodeToString(sum[:]) ||
		o["file_size_bytes"].(float64) != float64(len(data)) || o["snapshot_used"] != false ||
		o["exported_uuid"] != vmtest.VMUUID || o["compression"] != "none" || o["power_state"] != "halted" {
		t.Fatalf("result: %v", o)
	}
	calls := h.fake.Calls()
	if strings.Contains(calls, "VM.snapshot") || !strings.Contains(calls, "HTTP export "+vmtest.VMUUID+" gzip=") {
		t.Fatalf("calls:\n%s", calls)
	}
	if _, err := os.Stat(path + ".partial"); !os.IsNotExist(err) {
		t.Fatal(".partial must not remain")
	}
	if _, ok := body["request"]; !ok {
		t.Fatal("single get must include request")
	}
}

// T-VM-U21, U22, U26: Running + disks:[0] → 스냅샷, 디스크 1 제거, 스냅샷 내보내기, 스냅샷과 VDI 정리.
func TestExportRunningWithDisks(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.PowerState = "running"
	h.submit("e2", vm.ActionExport, map[string]interface{}{"file_name": "part", "disks": []int{0}, "compression": "gzip"})
	body := h.wait("e2")
	if body["status"] != "succeeded" {
		t.Fatalf("status: %v", body)
	}
	calls := h.fake.Calls()
	for _, want := range []string{"VM.snapshot", "VBD.add_to_other_config OpaqueRef:svbd1 owner false",
		"VDI.destroy OpaqueRef:svdi1", "HTTP export " + vmtest.SnapUUID + " gzip=true", "VM.destroy OpaqueRef:snap"} {
		if !strings.Contains(calls, want) {
			t.Fatalf("missing %q in calls:\n%s", want, calls)
		}
	}
	if strings.Contains(calls, "VDI.destroy OpaqueRef:svdi0\n") && strings.Index(calls, "VDI.destroy OpaqueRef:svdi0") < strings.Index(calls, "HTTP export") {
		t.Fatal("selected disk must not be removed before export")
	}
	o := opts(body)
	if o["snapshot_used"] != true || o["exported_uuid"] != vmtest.SnapUUID || fmt.Sprint(o["disks"]) != "[0]" {
		t.Fatalf("result: %v", o)
	}
	if _, ok := o["snapshot_cleanup_failed"]; ok {
		t.Fatalf("cleanup must succeed: %v", o)
	}
}

// T-VM-U21: VM 에 없는 디스크 번호 → DISK_NOT_FOUND, 임시 스냅샷은 지운다.
func TestExportDiskNotFound(t *testing.T) {
	h := newHarness(t, nil)
	h.submit("e3", vm.ActionExport, map[string]interface{}{"file_name": "x", "disks": []int{5}})
	body := h.wait("e3")
	if body["status"] != "failed" || errCode(body) != vm.CodeDiskNotFound {
		t.Fatalf("status: %v", body)
	}
	if !strings.Contains(h.fake.Calls(), "VM.destroy OpaqueRef:snap") {
		t.Fatalf("snapshot must be removed:\n%s", h.fake.Calls())
	}
}

// T-VM-U24: 같은 이름 파일 + overwrite=false → FILE_EXISTS(XAPI 호출 전), true → 덮어씀.
func TestExportFileExists(t *testing.T) {
	h := newHarness(t, nil)
	h.submit("f1", vm.ActionExport, map[string]interface{}{"file_name": "dup.xva"})
	h.wait("f1")
	h.submit("f2", vm.ActionExport, map[string]interface{}{"file_name": "dup"})
	if b := h.wait("f2"); b["status"] != "failed" || errCode(b) != vm.CodeFileExists {
		t.Fatalf("f2: %v", b)
	}
	h.submit("f3", vm.ActionExport, map[string]interface{}{"file_name": "dup", "overwrite": true})
	if b := h.wait("f3"); b["status"] != "succeeded" {
		t.Fatalf("f3: %v", b)
	}
}

// T-VM-U23, U41, U40: zstd 는 .xva.zst, 백업 뒤 검증 결과가 들어간다.
func TestExportZstdVerifyAfter(t *testing.T) {
	h := newHarness(t, nil)
	h.submit("z1", vm.ActionExport, map[string]interface{}{"file_name": "z", "compression": "zstd", "verify_after_export": true})
	body := h.wait("z1")
	o := opts(body)
	v, _ := o["verify"].(map[string]interface{})
	if body["status"] != "succeeded" || !strings.HasSuffix(fmt.Sprint(o["file_path"]), "z.xva.zst") ||
		v["verified"] != true || v["compression_detected"] != "zstd" || v["checksum_sha256"] != o["checksum_sha256"] {
		t.Fatalf("result: %v", body)
	}
}

// T-VM-U10, I08: XAPI 오류 원문이 detail 에 그대로, 틀린 암호는 XAPI_AUTH_FAILED.
func TestXAPIErrors(t *testing.T) {
	h := newHarness(t, nil)
	b := h.job("x1", vm.ActionExport, map[string]interface{}{"file_name": "x1"})
	b["payload"].(map[string]interface{})["targetUUID"] = "00000000-0000-0000-0000-000000000000"
	if code, _ := h.do("POST", "/api/v1/jobs", b, true); code != 202 {
		t.Fatal(code)
	}
	body := h.wait("x1")
	if body["status"] != "failed" || errCode(body) != vm.CodeXAPI ||
		errDetail(body) != "XAPI Error: UUID_INVALID | VM | 00000000-0000-0000-0000-000000000000" {
		t.Fatalf("x1: %v", body)
	}
	b = h.job("x2", vm.ActionExport, map[string]interface{}{"file_name": "x2"})
	b["payload"].(map[string]interface{})["credentials"] = map[string]interface{}{"password": "wrong"}
	h.do("POST", "/api/v1/jobs", b, true)
	if body := h.wait("x2"); errCode(body) != vm.CodeXAPIAuth {
		t.Fatalf("x2: %v", body)
	}
	h.fake.ExportHTTP = 500
	h.submit("x3", vm.ActionExport, map[string]interface{}{"file_name": "x3"})
	if body := h.wait("x3"); errCode(body) != vm.CodeXAPI || !strings.Contains(errDetail(body), "export HTTP 500") {
		t.Fatalf("x3: %v", body)
	}
	h.fake.ExportHTTP = 0
	h.fake.TaskStatus = "failure"
	h.submit("x4", vm.ActionExport, map[string]interface{}{"file_name": "x4"})
	if body := h.wait("x4"); errCode(body) != vm.CodeExportIncomplete || !strings.Contains(errDetail(body), "VDI_IO_ERROR") {
		t.Fatalf("x4: %v", body)
	}
	if _, err := os.Stat(filepath.Join(h.cfg.Current().Backup.BasePath, "x4.xva")); !os.IsNotExist(err) {
		t.Fatal("incomplete export must not leave a file")
	}
}

// T-VM-U40: 손상 파일은 succeeded + verified=false, 없는 파일은 failed FILE_IO_ERROR.
func TestVerify(t *testing.T) {
	h := newHarness(t, nil)
	base := h.cfg.Current().Backup.BasePath
	_ = os.MkdirAll(base, 0o755)
	_ = os.WriteFile(filepath.Join(base, "good.xva"), vmtest.XVA(), 0o644)
	_ = os.WriteFile(filepath.Join(base, "bad.xva"), vmtest.BadXVA(), 0o644)
	h.submit("w1", vm.ActionVerify, map[string]interface{}{"file_path": "good.xva"})
	if b := h.wait("w1"); b["status"] != "succeeded" || opts(b)["verified"] != true || opts(b)["chunk_count"].(float64) != 1 {
		t.Fatalf("good: %v", b)
	}
	h.submit("w2", vm.ActionVerify, map[string]interface{}{"file_path": "bad.xva"})
	b := h.wait("w2")
	if b["status"] != "succeeded" || opts(b)["verified"] != false || opts(b)["failure_reasons"] == nil {
		t.Fatalf("bad: %v", b)
	}
	h.submit("w3", vm.ActionVerify, map[string]interface{}{"file_path": "none.xva"})
	if b := h.wait("w3"); b["status"] != "failed" || errCode(b) != vm.CodeFileIO {
		t.Fatalf("missing: %v", b)
	}
}

// T-VM-U30, U32: restore 파라미터와 후처리(템플릿, allowed_operations, 이름, Home Server).
func TestRestore(t *testing.T) {
	h := newHarness(t, nil)
	base := h.cfg.Current().Backup.BasePath
	_ = os.MkdirAll(base, 0o755)
	xvaData := vmtest.XVA()
	_ = os.WriteFile(filepath.Join(base, "r.xva"), xvaData, 0o644)
	h.submit("r1", vm.ActionRestore, map[string]interface{}{"file_path": "r.xva", "sr_uuid": vmtest.SRUUID,
		"restore_original_uuid": true, "name_label": "restored", "home_server_uuid": vmtest.HostUUID})
	b := h.wait("r1")
	o := opts(b)
	if b["status"] != "succeeded" || o["vm_uuid"] != vmtest.NewVMUUID || o["is_a_template_fixed"] != true ||
		o["home_server_applied"] != true || o["name_label"] != "restored" {
		t.Fatalf("r1: %v", b)
	}
	calls := h.fake.Calls()
	for _, want := range []string{"VM.update_allowed_operations", "VM.set_name_label", "VM.set_affinity", "HTTP import true"} {
		if !strings.Contains(calls, want) {
			t.Fatalf("missing %q:\n%s", want, calls)
		}
	}
	if !strings.Contains(h.fake.ImportQuery, "sr_id=OpaqueRef%3Asr") || !bytes.Equal(h.fake.Imported, xvaData) {
		t.Fatalf("import query %s, %d bytes", h.fake.ImportQuery, len(h.fake.Imported))
	}
	// 없는 SR 은 파일을 보내기 전에 실패한다(실기 E-05)
	h.submit("r2", vm.ActionRestore, map[string]interface{}{"file_path": "r.xva", "sr_uuid": "00000000-0000-0000-0000-000000000000"})
	if b := h.wait("r2"); errCode(b) != vm.CodeXAPI || !strings.Contains(errDetail(b), "UUID_INVALID | SR") {
		t.Fatalf("r2: %v", b)
	}
}

// T-VM-U31: zstd 파일은 풀어서 올리고, 임시 파일을 남기지 않는다.
func TestRestoreZstd(t *testing.T) {
	h := newHarness(t, nil)
	base := h.cfg.Current().Backup.BasePath
	_ = os.MkdirAll(base, 0o755)
	raw := vmtest.XVA()
	var buf bytes.Buffer
	enc, _ := zstd.NewWriter(&buf)
	_, _ = enc.Write(raw)
	_ = enc.Close()
	_ = os.WriteFile(filepath.Join(base, "r.xva.zst"), buf.Bytes(), 0o644)
	h.submit("rz", vm.ActionRestore, map[string]interface{}{"file_path": "r.xva.zst", "sr_uuid": vmtest.SRUUID})
	b := h.wait("rz")
	if b["status"] != "succeeded" || opts(b)["compression_detected"] != "zstd" || !bytes.Equal(h.fake.Imported, raw) {
		t.Fatalf("rz: %v (%d bytes imported)", b, len(h.fake.Imported))
	}
	left, _ := filepath.Glob(filepath.Join(base, ".vbr-vm-zstd-*"))
	if len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

// T-VM-U03: 같은 taskId 를 동시에 보내도 한 번만 실행한다.
func TestDuplicateConcurrent(t *testing.T) {
	h := newHarness(t, nil)
	base := h.cfg.Current().Backup.BasePath
	_ = os.MkdirAll(base, 0o755)
	_ = os.WriteFile(filepath.Join(base, "good.xva"), vmtest.XVA(), 0o644)
	var wg sync.WaitGroup
	var dups, firsts int32
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, body := h.do("POST", "/api/v1/jobs", h.job("same", vm.ActionVerify, map[string]interface{}{"file_path": "good.xva"}), true)
			if code != 202 {
				t.Errorf("code %d", code)
				return
			}
			if body["message"] == DuplicateMessage {
				atomic.AddInt32(&dups, 1)
			} else {
				atomic.AddInt32(&firsts, 1)
			}
		}()
	}
	wg.Wait()
	if firsts != 1 || dups != 49 {
		t.Fatalf("firsts=%d dups=%d", firsts, dups)
	}
	h.wait("same")
}

// T-VM-U51: 대기 중 취소 200, 처리 중 취소 202 → canceled + XAPI task 취소 + .partial 삭제, 끝난 것 409, 없는 것 404.
func TestCancel(t *testing.T) {
	h := newHarness(t, func(l *vmconfig.Loaded) { l.Concurrency.MaxJobs = 1 })
	h.fake.SlowExport = make(chan struct{})
	h.fake.ExportStart = make(chan struct{})
	h.submit("c1", vm.ActionExport, map[string]interface{}{"file_name": "c1"})
	<-h.fake.ExportStart
	h.submit("c2", vm.ActionExport, map[string]interface{}{"file_name": "c2"})
	if code, b := h.do("POST", "/api/v1/jobs/c2/cancel", nil, true); code != 200 || b["status"] != "canceled" {
		t.Fatalf("queued cancel: %d %v", code, b)
	}
	if code, b := h.do("POST", "/api/v1/jobs/c1/cancel", nil, true); code != 202 || b["step"] != "canceling" {
		t.Fatalf("running cancel: %d %v", code, b)
	}
	b := h.wait("c1")
	if b["status"] != "canceled" || errCode(b) != vm.CodeCanceled {
		t.Fatalf("c1: %v", b)
	}
	if !strings.Contains(h.fake.Calls(), "task.cancel") {
		t.Fatalf("XAPI task must be canceled:\n%s", h.fake.Calls())
	}
	if _, err := os.Stat(filepath.Join(h.cfg.Current().Backup.BasePath, "c1.xva.partial")); !os.IsNotExist(err) {
		t.Fatal(".partial must be removed after cancel")
	}
	if code, b := h.do("POST", "/api/v1/jobs/c1/cancel", nil, true); code != 409 || b["code"] != vm.CodeAlreadyFinished {
		t.Fatalf("finished cancel: %d %v", code, b)
	}
	if code, b := h.do("POST", "/api/v1/jobs/none/cancel", nil, true); code != 404 || b["code"] != vm.CodeTaskNotFound {
		t.Fatalf("unknown cancel: %d %v", code, b)
	}
	close(h.fake.SlowExport)
}

// T-VM-U22·U51: 처리 중 취소 직후 XenServer 가 디스크를 잠시 잡고 있어도(task cancelling, VDI_IN_USE)
// 임시 스냅샷을 끝내 지운다. 2026-11-19 실기 I07 에서 VDI_IN_USE 로 스냅샷이 남았던 결함의 회귀 시험.
func TestCancelSnapshotInUse(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.SlowExport = make(chan struct{})
	h.fake.ExportStart = make(chan struct{})
	h.fake.Cancelling = 2
	h.fake.VDIInUse = 1
	h.submit("ciu", vm.ActionExport, map[string]interface{}{"file_name": "ciu", "snapshot_mode": "always"})
	<-h.fake.ExportStart
	if _, b := h.do("GET", "/api/v1/jobs/ciu", nil, true); opts(b)["snapshot_left"] == nil {
		t.Fatalf("running export must record the temporary snapshot: %v", b)
	}
	if code, _ := h.do("POST", "/api/v1/jobs/ciu/cancel", nil, true); code != 202 {
		t.Fatalf("running cancel: %d", code)
	}
	b := h.wait("ciu")
	if b["status"] != "canceled" {
		t.Fatalf("status: %v", b)
	}
	if o := opts(b); o["snapshot_cleanup_failed"] != nil || o["snapshot_left"] != nil {
		t.Fatalf("snapshot must be removed after retry: %v", o)
	}
	calls := h.fake.Calls()
	if strings.Count(calls, "VDI.destroy") < 2 || !strings.Contains(calls, "VM.destroy") {
		t.Fatalf("expected VDI.destroy retry then VM.destroy:\n%s", calls)
	}
	if strings.Index(calls, "task.cancel") > strings.Index(calls, "VDI.destroy") {
		t.Fatalf("cleanup must start after task.cancel:\n%s", calls)
	}
	close(h.fake.SlowExport)
}

// T-VM-U52: 작업자 1 에 둘째는 queued, 설정을 2 로 바꾸면 바로 시작한다. cancel-queued 는 대기만 취소.
func TestWorkersAndCancelQueued(t *testing.T) {
	h := newHarness(t, func(l *vmconfig.Loaded) { l.Concurrency.MaxJobs = 1 })
	h.fake.SlowExport = make(chan struct{})
	h.fake.ExportStart = make(chan struct{})
	h.submit("q1", vm.ActionExport, map[string]interface{}{"file_name": "q1"})
	<-h.fake.ExportStart
	h.submit("q2", vm.ActionExport, map[string]interface{}{"file_name": "q2"})
	h.submit("q3", vm.ActionExport, map[string]interface{}{"file_name": "q3"})
	if _, b := h.do("GET", "/api/v1/jobs/q2", nil, true); b["status"] != "queued" {
		t.Fatalf("q2 must be queued: %v", b)
	}
	if code, b := h.do("GET", "/api/v1/jobs?status=queued", nil, true); code != 200 || b["count"].(float64) != 2 {
		t.Fatalf("list queued: %v", b)
	}
	h.cfg.update(func(l *vmconfig.Loaded) { l.Concurrency.MaxJobs = 2 })
	h.svc.AdjustWorkers()
	h.wait("q2", "running")
	code, b := h.do("POST", "/api/v1/jobs/cancel-queued", nil, true)
	if code != 200 || b["count"].(float64) != 1 {
		t.Fatalf("cancel-queued: %v", b)
	}
	close(h.fake.SlowExport)
	if b := h.wait("q1"); b["status"] != "succeeded" {
		t.Fatalf("q1: %v", b)
	}
	if b := h.wait("q3"); b["status"] != "canceled" {
		t.Fatalf("q3: %v", b)
	}
}

// T-VM-U72: 종료하면 처리 중은 interrupted, 새 접수는 503.
func TestShutdown(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.SlowExport = make(chan struct{})
	h.fake.ExportStart = make(chan struct{})
	h.submit("s1", vm.ActionExport, map[string]interface{}{"file_name": "s1"})
	<-h.fake.ExportStart
	h.svc.Shutdown(5 * time.Second)
	rec, _ := h.svc.Get("s1")
	if rec.Status != jobstore.StatusInterrupted || rec.Error == nil || rec.Error.Code != vm.CodeInterrupted {
		t.Fatalf("s1: %+v", rec)
	}
	code, b := h.do("POST", "/api/v1/jobs", h.job("s2", vm.ActionExport, map[string]interface{}{"file_name": "s2"}), true)
	if code != 503 || b["code"] != vm.CodeStopping {
		t.Fatalf("after shutdown: %d %v", code, b)
	}
	close(h.fake.SlowExport)
}

// T-VM-U82: 암호(평문과 ENC 값 모두)가 작업 기록·조회 응답에 남지 않는다.
func TestSecretsNotLeaked(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	p, err := secret.Build(secret.KeyringConfig{Version: secret.KeyringVersion1, ActiveKeyID: "k1",
		Keys: map[string]secret.KeyConfig{"k1": {Algorithm: secret.AlgorithmAES256GCM, Material: base64.StdEncoding.EncodeToString(key)}}})
	if err != nil {
		t.Fatal(err)
	}
	enc, err := p.Encrypt(vm.PasswordPurpose, []byte(vmtest.Password))
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(l *vmconfig.Loaded) { l.Crypto.Enabled = true; l.Protector = p })
	b := h.job("k1", vm.ActionExport, map[string]interface{}{"file_name": "k1"})
	b["payload"].(map[string]interface{})["credentials"] = map[string]interface{}{"username": "root", "password": enc}
	if code, _ := h.do("POST", "/api/v1/jobs", b, true); code != 202 {
		t.Fatal(code)
	}
	body := h.wait("k1")
	if body["status"] != "succeeded" {
		t.Fatalf("encrypted password must work: %v", body)
	}
	_, list := h.do("GET", "/api/v1/jobs", nil, true)
	all, _ := json.Marshal([]interface{}{body, list})
	file, _ := os.ReadFile(filepath.Join(h.cfg.Current().TaskStore.Dir, jobstore.FileName))
	for _, s := range []string{vmtest.Password, enc} {
		if bytes.Contains(all, []byte(s)) || bytes.Contains(file, []byte(s)) {
			t.Fatalf("secret leaked: %q", s)
		}
	}
	if !bytes.Contains(file, []byte(`"username":"root"`)) {
		t.Fatal("username should be kept in the record")
	}
}
