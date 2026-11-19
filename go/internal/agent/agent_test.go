package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ncc-vbr/internal/logx"
)

// fakeHV 는 시험용 Hypervisor 다. gate 가 있으면 백업이 그 채널을 기다린다.
type fakeHV struct {
	mu      sync.Mutex
	calls   []string
	gate    chan struct{}
	started chan string
	fail    error
	delay   time.Duration // 실제 백업처럼 시간이 걸리게 한다(접수 응답 뒤 관리 서버가 끊는 순서를 재현)
}

func (f *fakeHV) Backup(ctx context.Context, req BackupRequest, log *StepLog) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "backup "+req.VMUUID+" "+req.SnapshotName+" "+req.FileName+" "+req.DiskSeq)
	f.mu.Unlock()
	if f.started != nil {
		f.started <- req.SnapshotName
	}
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.fail != nil {
		return "", f.fail
	}
	return "676ffe7e-c3f4-8913-f0ed-e39eb31d0d7a", nil
}

func (f *fakeHV) Restore(ctx context.Context, req RestoreRequest, log *StepLog) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf("restore %s %v %s", req.FileName, req.Preserve, req.SRUUID))
	f.mu.Unlock()
	return "cd5498f2-0342-db8e-749e-53a5e86744dd", nil
}

type harness struct {
	t      *testing.T
	dir    string
	agent  *Agent
	cancel context.CancelFunc
	done   chan struct{}
}

func start(t *testing.T, hv Hypervisor, mutate func(*Settings)) *harness {
	t.Helper()
	dir := t.TempDir()
	cfg := Settings{Port: 0, BackupRoots: []string{filepath.Join(dir, "BACKUP")}, AsyncWorkers: 2}
	if mutate != nil {
		mutate(&cfg)
	}
	return startIn(t, dir, hv, cfg)
}

func startIn(t *testing.T, dir string, hv Hypervisor, cfg Settings) *harness {
	t.Helper()
	log := logx.New(dir)
	a := New(cfg, log, hv, dir)
	if err := a.Listen(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{t: t, dir: dir, agent: a, cancel: cancel, done: make(chan struct{})}
	go func() {
		a.Run(ctx)
		close(h.done)
	}()
	t.Cleanup(h.stop)
	return h
}

func (h *harness) stop() {
	h.cancel()
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		h.t.Error("agent did not stop")
	}
}

func (h *harness) dial() net.Conn {
	c, err := net.Dial("tcp", h.agent.Addr().String())
	if err != nil {
		h.t.Fatal(err)
	}
	return c
}

// call 은 전문 하나를 보내고 응답 하나를 받는다.
func (h *harness) call(c net.Conn, msg string) string {
	h.t.Helper()
	if _, err := c.Write(Frame([]byte(msg))); err != nil {
		h.t.Fatal(err)
	}
	return readFrame(h.t, c)
}

func readFrame(t *testing.T, c net.Conn) string {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	head := make([]byte, 7)
	if _, err := io.ReadFull(c, head); err != nil {
		t.Fatalf("read length: %v", err)
	}
	n, err := strconv.Atoi(string(head))
	if err != nil {
		t.Fatalf("length %q", head)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(c, body); err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// once 는 접속을 새로 열어 한 번 묻고 닫는다(관리 서버의 방식).
func (h *harness) once(msg string) string {
	c := h.dial()
	defer c.Close()
	return h.call(c, msg)
}

// waitQuery 는 결과 조회 응답의 코드가 want 가 될 때까지 기다린다.
func (h *harness) waitQuery(qseq, target string, want byte) string {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r := h.once("B" + qseq + "Q0001" + target)
		if r[18] == want {
			return r
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("query %s: want code %c, last %q", target, want, r)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (h *harness) tranLog(seq string) string {
	data, err := os.ReadFile(filepath.Join(h.dir, tranFileName(seq)))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(data)
}

// steps 는 transaction 로그에서 seq 의 단계 기호와 문구를 차례로 뽑는다.
func steps(log, seq string) []string {
	var out []string
	for _, line := range strings.Split(log, "\n") {
		if len(line) > 45 && line[0] == 0 && line[1] == '[' && line[26:43] == seq {
			out = append(out, line[44:])
		}
	}
	return out
}

const vmUUID = "22acc256-b431-f9df-6c44-b1f71e7912fc"

// T-AG-U01 전문 틀
func TestMessageFrame(t *testing.T) {
	m := Message{Raw: []byte("B20261117001001001A0035BACKUP x")}
	if m.Seq() != "20261117001001001" || m.Task() != 'A' || m.Priority() != 5 || m.Body() != "BACKUP x" {
		t.Fatalf("parse: %q %c %d %q", m.Seq(), m.Task(), m.Priority(), m.Body())
	}
	if got := string(m.Reply('0', "OK")); got != "B2026111700100100100035OK" {
		t.Fatalf("reply %q", got)
	}
	if got := string(Frame(m.Reply('0', "OK"))); got != "0000025B2026111700100100100035OK" {
		t.Fatalf("frame %q (원본 접수 응답은 길이 25)", got)
	}
	if got := string(m.Echo('8')); got != "B2026111700100100180035BACKUP x" {
		t.Fatalf("echo %q", got)
	}
}

// T-AG-U02 운영 로그 형식과 결과 조회
func TestTranLogFormatAndQuery(t *testing.T) {
	dir := t.TempDir()
	tl := NewTranLog(logx.New(dir), dir)
	req := []byte("B20261117001001001A0035BACKUP " + vmUUID + " 17 /BACKUP/17.xva")
	res := []byte("B2026111700100100100035VM BACKUP Success : VM uuid=" + vmUUID +
		", Snapshot VM uuid=676ffe7e-c3f4-8913-f0ed-e39eb31d0d7a, backup filename=/BACKUP/17.xva\n")
	tl.Write('1', "Message Receive", req)
	tl.Write('3', "ASYNC Queue receive", req)
	tl.Write('5', "ASYNC Queue result send finished", res)
	tl.Write('0', "ASYNC RESULT send error", res)

	data, _ := os.ReadFile(filepath.Join(dir, "transaction-202611.log"))
	first := strings.SplitN(string(data), "\n", 2)[0]
	if !strings.HasPrefix(first, "\x00[") || !strings.HasSuffix(first, "]20261117001001001 1:Message Receive") || len(first) != 61 {
		t.Fatalf("header line %q", first)
	}
	if !bytes.Contains(data, append(append([]byte("\n\x00"), req...), 0, '\n')) {
		t.Fatal("body line format")
	}
	code, text, ok := tl.Query("20261117001001001")
	if !ok || code != '0' || text != string(res[23:]) {
		t.Fatalf("query: %v %c %q", ok, code, text)
	}
	code, text, ok = tl.Query("20261117009999999")
	if !ok || code != '9' || text != "20261117009999999" {
		t.Fatalf("not found: %v %c %q", ok, code, text)
	}
	if _, _, ok = tl.Query("20251117009999999"); ok {
		t.Fatal("missing month file must fail")
	}
}

// T-AG-U03 비동기 백업: 접수 응답 → 접속 끊김 → 결과 조회(운영 로그와 같은 흐름)
func TestAsyncBackupFlow(t *testing.T) {
	hv := &fakeHV{delay: 200 * time.Millisecond}
	h := start(t, hv, nil)
	file := filepath.Join(h.dir, "BACKUP", "17.xva")
	seq := "20261117001001001"
	reply := h.once("B" + seq + "A0035BACKUP " + vmUUID + " 17 " + file)
	if reply != "B"+seq+"00035OK" {
		t.Fatalf("accept reply %q", reply)
	}
	r := h.waitQuery("20261117002000001", seq, '0')
	want := "B20261117002000001" + "0" + "0001" + "VM BACKUP Success : VM uuid=" + vmUUID +
		", Snapshot VM uuid=676ffe7e-c3f4-8913-f0ed-e39eb31d0d7a, backup filename=" + file + "\n"
	if r != want {
		t.Fatalf("query result\n got %q\nwant %q", r, want)
	}
	got := steps(h.tranLog(seq), seq)
	wantSteps := []string{"1:Message Receive", "2:Queue saving", "3:ASYNC Queue receive",
		"5:ASYNC Queue result send finished", "0:ASYNC RESULT send error"}
	if strings.Join(got, "|") != strings.Join(wantSteps, "|") {
		t.Fatalf("steps %v", got)
	}
}

// T-AG-U04 접속을 유지하면 결과를 같은 접속으로 받는다
func TestAsyncResultOnOpenConnection(t *testing.T) {
	h := start(t, &fakeHV{}, nil)
	c := h.dial()
	defer c.Close()
	seq := "20261117001001002"
	file := filepath.Join(h.dir, "BACKUP", "a.xva")
	if r := h.call(c, "B"+seq+"A0005BACKUP "+vmUUID+" s "+file); !strings.HasSuffix(r, "OK") {
		t.Fatal(r)
	}
	r := readFrame(t, c)
	if r[18] != '0' || !strings.HasPrefix(r[23:], "VM BACKUP Success") {
		t.Fatalf("result %q", r)
	}
	time.Sleep(50 * time.Millisecond)
	got := steps(h.tranLog(seq), seq)
	if got[len(got)-1] != "0:ASYNC RESULT send success" {
		t.Fatalf("steps %v", got)
	}
}

// T-AG-U05 처리 중이면 3 과 요청 본문, 없으면 9 와 전문번호(2015 표본과 같다)
func TestQueryProcessingAndNotFound(t *testing.T) {
	hv := &fakeHV{gate: make(chan struct{}), started: make(chan string, 1)}
	h := start(t, hv, nil)
	seq := "20261117001001003"
	body := "BACKUP " + vmUUID + " s " + filepath.Join(h.dir, "BACKUP", "b.xva")
	h.once("B" + seq + "A0005" + body)
	<-hv.started
	r := h.waitQuery("20261117002000002", seq, '3')
	if r[23:] != body {
		t.Fatalf("processing body %q", r[23:])
	}
	close(hv.gate)
	r = h.once("B20261117002000003Q0001" + "20261117001009999")
	if r != "B20261117002000003900012026111700100"+"9999" {
		t.Fatalf("not found %q", r)
	}
	r = h.once("B20261117002000004Q0001short")
	if r != "B20261117002000004"+"90001"+"TASK SEQ is not valid.[short]" {
		t.Fatalf("invalid seq %q", r)
	}
}

// T-AG-U06 취소: 대기 중 → 취소, 처리 중 → 거절, 일괄 취소
func TestCancel(t *testing.T) {
	hv := &fakeHV{gate: make(chan struct{}), started: make(chan string, 10)}
	h := start(t, hv, func(s *Settings) { s.AsyncWorkers = 1 })
	file := filepath.Join(h.dir, "BACKUP", "c.xva")
	running, queued, queued2 := "20261117001001010", "20261117001001011", "20261117001001012"
	h.once("B" + running + "A0005BACKUP " + vmUUID + " run " + file)
	<-hv.started
	h.once("B" + queued + "A0005BACKUP " + vmUUID + " q1 " + file)
	h.once("B" + queued2 + "A0005BACKUP " + vmUUID + " q2 " + file)

	if r := h.once("B20261117003000001C0001" + running); r[18:] != "90001Request message is processing." {
		t.Fatalf("cancel running %q", r)
	}
	if r := h.once("B20261117003000002C0001" + queued); r[18:] != "00001Request message is cancel.[20261117003000002]" {
		t.Fatalf("cancel queued %q", r)
	}
	// 같은 작업을 다시 취소: 취소된 칸(상태 9)은 빈 칸으로 다시 쓰이므로 이 요청 자신이 그 칸을 차지해
	// 원본과 같이 "not found" 가 된다(원본 816행 status > 3 재사용).
	if r := h.once("B20261117003000003C0001" + queued); r[18:] != "90001Request message not found.(Invalid or Already finished)." {
		t.Fatalf("cancel twice %q", r)
	}
	if r := h.once("B20261117003000004C0001" + "20261117001009999"); r[18:] != "90001Request message not found.(Invalid or Already finished)." {
		t.Fatalf("cancel unknown %q", r)
	}
	r := h.once("B20261117003000005C0001" + "00000000000000000")
	if r[18:] != "C0001Multi cancel summary : total [100], cancel [1], failed [99]\n" {
		t.Fatalf("multi cancel %q", r)
	}
	// 취소된 작업의 결과 조회는 마지막 기록 'C'
	q := h.once("B20261117003000006Q0001" + queued)
	if q[18] != 'C' || !strings.HasPrefix(q[23:], "BACKUP "+vmUUID+" q1") {
		t.Fatalf("query canceled %q", q)
	}
	close(hv.gate)
}

// T-AG-U07 형식 오류, 지원하지 않는 요청, 대기열 가득
func TestRejects(t *testing.T) {
	hv := &fakeHV{gate: make(chan struct{})}
	h := start(t, hv, nil)
	if r := h.once("X20261117001001020A0005BACKUP"); r != "X2026111700100102090005BACKUP" {
		t.Fatalf("format error %q", r)
	}
	if r := h.once("B20261117001001021S0101df -k /"); r != "B20261117001001021"+"90101"+"[ERROR]Not supported." {
		t.Fatalf("sync %q", r)
	}
	if r := h.once("B20261117001001022I0001STATUS"); r[18:] != "90001[ERROR]Not supported." {
		t.Fatalf("inquire %q", r)
	}
	if r := h.once("B20261117001001023Z0001x"); r != "B2026111700100102390001x" {
		t.Fatalf("task format %q", r)
	}
	// 100칸을 비동기 작업으로 채운다(작업자 2개는 gate 에서 멈춘다)
	file := filepath.Join(h.dir, "BACKUP", "f.xva")
	for i := 0; i < MaxSlots; i++ {
		seq := fmt.Sprintf("2026111705%07d", i)
		if r := h.once("B" + seq + "A0005BACKUP " + vmUUID + " s " + file); !strings.HasSuffix(r, "OK") {
			t.Fatalf("fill %d: %q", i, r)
		}
	}
	if r := h.once("B20261117009999999A0005BACKUP x"); r != "B2026111700999999980005BACKUP x" {
		t.Fatalf("queue full %q", r)
	}
	close(hv.gate)
}

// T-AG-U08 인자 오류 문장(원본 문장 그대로)과 백업 루트 밖 거부
func TestArgumentErrors(t *testing.T) {
	hv := &fakeHV{}
	h := start(t, hv, nil)
	root := filepath.Join(h.dir, "BACKUP")
	cases := []struct{ body, want string }{
		{"BACKUP 1234 s " + root + "/a.xva", "[ERROR]VM UUID is not valid."},
		{"BACKUP " + vmUUID, "[ERROR]Snapshot Name is not valid."},
		{"BACKUP " + vmUUID + " s", "[ERROR]Export File Name is not valid."},
		{"PARTIAL " + vmUUID + " 0101 s " + root + "/a.xva", "[ERROR]Partial Backup Disk sequence information is not valid."},
		{"BACKUP " + vmUUID + " s " + filepath.Join(h.dir, "etc", "passwd"), "[ERROR]Export File Name is not allowed."},
		{"BACKUP " + vmUUID + " s " + root + "/../x.xva", "[ERROR]Export File Name is not allowed."},
		{"RESTORE", "[ERROR]File Name is not valid."},
		// 2015 표본: preserve 를 빼먹어 sr-uuid 가 그 자리로 온 경우
		{"RESTORE " + root + "/t.xva  9ad088e0-d03d-0269-0b7f-e40006aa5014", "[ERROR]SR UUID is not valid."},
		{"RESTORE " + root + "/t.xva maybe 9ad088e0-d03d-0269-0b7f-e40006aa5014", "[ERROR]Preserve option(true/false) is not valid."},
		{"SLEEP 5", "[ERROR]Not supported."},
	}
	for i, c := range cases {
		seq := fmt.Sprintf("2026111706%07d", i)
		h.once("B" + seq + "A0005" + c.body)
		r := h.waitQuery(fmt.Sprintf("2026111707%07d", i), seq, '0')
		if r[23:] != c.want {
			t.Errorf("%q: got %q want %q", c.body, r[23:], c.want)
		}
	}
	if len(hv.calls) != 0 {
		t.Fatalf("hypervisor must not be called: %v", hv.calls)
	}
	// 정상 복구 인자
	seq := "20261117008000001"
	h.once("B" + seq + "A0005RESTORE " + root + "/t.xva true 9ad088e0-d03d-0269-0b7f-e40006aa5014")
	r := h.waitQuery("20261117008000002", seq, '0')
	if r[23:] != "cd5498f2-0342-db8e-749e-53a5e86744dd" || hv.calls[0] != "restore "+root+"/t.xva true 9ad088e0-d03d-0269-0b7f-e40006aa5014" {
		t.Fatalf("restore %q %v", r, hv.calls)
	}
}

// T-AG-U09 백업 실패는 결과 코드 9 의 결과가 기록되고, 조회 코드는 마지막 단계 0 이다(원본 동작)
func TestBackupFailureResult(t *testing.T) {
	hv := &fakeHV{fail: errors.New("[ERROR]XAPI Error: UUID_INVALID | VM | x")}
	h := start(t, hv, nil)
	seq := "20261117001001030"
	h.once("B" + seq + "A0005BACKUP " + vmUUID + " s " + filepath.Join(h.dir, "BACKUP", "e.xva"))
	r := h.waitQuery("20261117002000030", seq, '0')
	if r[23:] != "[ERROR]XAPI Error: UUID_INVALID | VM | x" {
		t.Fatalf("query %q", r)
	}
	if !strings.Contains(h.tranLog(seq), "\x00B"+seq+"90005[ERROR]XAPI Error") {
		t.Fatal("result record must carry code 9")
	}
}

// T-AG-U10 우선순위: 숫자가 작은 것부터, 같으면 먼저 온 것부터(원본 queue_seq)
func TestPriorityOrder(t *testing.T) {
	hv := &fakeHV{gate: make(chan struct{}), started: make(chan string, 10)}
	h := start(t, hv, func(s *Settings) { s.AsyncWorkers = 1 })
	file := filepath.Join(h.dir, "BACKUP", "p.xva")
	h.once("B20261117001001040A0005BACKUP " + vmUUID + " first " + file)
	<-hv.started
	h.once("B20261117001001041A0009BACKUP " + vmUUID + " nine " + file)
	h.once("B20261117001001042A0005BACKUP " + vmUUID + " fiveA " + file)
	h.once("B20261117001001043A0000BACKUP " + vmUUID + " zero " + file)
	h.once("B20261117001001044A0005BACKUP " + vmUUID + " fiveB " + file)
	var order []string
	for i := 0; i < 4; i++ {
		hv.gate <- struct{}{}
		order = append(order, <-hv.started)
	}
	hv.gate <- struct{}{}
	if strings.Join(order, ",") != "zero,fiveA,fiveB,nine" {
		t.Fatalf("order %v", order)
	}
}

// T-AG-U11 재기동: 처리 중이던 작업은 6, 대기 중이던 작업은 다시 처리
func TestRestartRecovery(t *testing.T) {
	hv := &fakeHV{gate: make(chan struct{}), started: make(chan string, 10)}
	dir := t.TempDir()
	cfg := Settings{Port: 0, BackupRoots: []string{filepath.Join(dir, "BACKUP")}, AsyncWorkers: 2}
	h := startIn(t, dir, hv, func() Settings { c := cfg; c.AsyncWorkers = 1; return c }())
	file := filepath.Join(dir, "BACKUP", "r.xva")
	h.once("B20261117001001050A0005BACKUP " + vmUUID + " run " + file)
	<-hv.started
	h.once("B20261117001001051A0005BACKUP " + vmUUID + " wait " + file)
	h.stop() // 처리 중에 종료

	hv2 := &fakeHV{}
	h2 := startIn(t, dir, hv2, cfg)
	r := h2.waitQuery("20261117002000050", "20261117001001050", '6')
	if !strings.HasPrefix(r[23:], "BACKUP "+vmUUID+" run") {
		t.Fatalf("restarted %q", r)
	}
	r = h2.waitQuery("20261117002000051", "20261117001001051", '0')
	if !strings.HasPrefix(r[23:], "VM BACKUP Success") {
		t.Fatalf("requeued %q", r)
	}
}

// T-AG-U12 허용 주소 밖의 접속은 끊는다(Q-A06)
func TestAllowClient(t *testing.T) {
	h := start(t, &fakeHV{}, func(s *Settings) { s.AllowClients = []string{"192.0.2.1"} })
	c := h.dial()
	defer c.Close()
	_, _ = c.Write(Frame([]byte("B20261117001001060Q000120261117001001001")))
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("connection must be closed")
	}
}

// T-AG-U13 길이가 모자란 전문은 접속을 끊는다
func TestShortMessage(t *testing.T) {
	h := start(t, &fakeHV{}, nil)
	c := h.dial()
	defer c.Close()
	_, _ = c.Write([]byte("0000050B2026"))
	_ = c.SetReadDeadline(time.Now().Add(readRetryWindow + 2*time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("connection must be closed")
	}
}
