package jobstore

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// T-VM-U53: 끝나지 않은 기록(queued, running)은 다시 열 때 interrupted 가 된다.
func TestReopenMarksInterrupted(t *testing.T) {
	dir := t.TempDir()
	s, _, err := Open(dir, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"q", "r", "d"} {
		if _, err := s.Accept(Record{TaskID: id, Action: "export"}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	_, _, _ = s.Update("r", true, func(r *Record) { r.Status = StatusRunning; r.StartedAt = &now; r.Step = "exporting" })
	_, _, _ = s.Update("d", true, func(r *Record) { r.Status = StatusSucceeded; r.FinishedAt = &now })
	_ = s.Close()

	s2, interrupted, err := Open(dir, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if len(interrupted) != 2 {
		t.Fatalf("interrupted %d", len(interrupted))
	}
	for _, id := range []string{"q", "r"} {
		r, _ := s2.Get(id)
		if r.Status != StatusInterrupted || r.Error == nil || r.Error.Code != "INTERRUPTED" {
			t.Fatalf("%s: %+v", id, r)
		}
	}
	if r, _ := s2.Get("d"); r.Status != StatusSucceeded {
		t.Fatalf("d: %+v", r)
	}
	if !strings.Contains(mustGetErr(s2, "r"), "exporting") {
		t.Fatal("running step should be in the detail")
	}
}

func mustGetErr(s *Store, id string) string {
	r, _ := s.Get(id)
	if r.Error == nil {
		return ""
	}
	return r.Error.Detail
}

// T-VM-U54: 압축하면 taskId 마다 한 줄, 보관 기간이 지난 끝난 작업은 빠진다. 같은 taskId 는 ErrDuplicate.
func TestCompactAndRetention(t *testing.T) {
	dir := t.TempDir()
	clock := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	s, _, err := Open(dir, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []string{"old", "new"} {
		_, _ = s.Accept(Record{TaskID: id})
	}
	if _, err := s.Accept(Record{TaskID: "old"}); err != ErrDuplicate {
		t.Fatalf("duplicate: %v", err)
	}
	for i := 0; i < 5; i++ {
		_, _, _ = s.Update("new", true, func(r *Record) { r.Status = StatusRunning; r.Percent = i })
	}
	fin := clock.Add(-48 * time.Hour)
	_, _, _ = s.Update("old", true, func(r *Record) { r.Status = StatusSucceeded; r.FinishedAt = &fin })
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, FileName))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], `"taskId":"new"`) {
		t.Fatalf("after compact:\n%s", data)
	}
	if _, err := s.Get("old"); err != ErrNotFound {
		t.Fatal("expired record must be removed")
	}
	// 끝난 작업은 더 바꾸지 않는다
	now2 := clock
	_, _, _ = s.Update("new", true, func(r *Record) { r.Status = StatusFailed; r.FinishedAt = &now2 })
	if _, changed, _ := s.Update("new", true, func(r *Record) { r.Status = StatusSucceeded }); changed {
		t.Fatal("finished record must not change")
	}
}

// T-VM-U60: 주소가 없으면 쌓지 않는다. 5xx 는 다시 보내고, 400 은 바로 Dead Letter. 토큰 헤더를 싣는다.
func TestOutbox(t *testing.T) {
	dir := t.TempDir()
	var status atomic.Int32
	status.Store(503)
	var gotToken atomic.Value
	var bodies atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken.Store(r.Header.Get("X-Internal-Token"))
		b, _ := io.ReadAll(r.Body)
		var m map[string]interface{}
		if json.Unmarshal(b, &m) == nil && m["taskId"] != nil {
			bodies.Add(1)
		}
		w.WriteHeader(int(status.Load()))
	}))
	defer srv.Close()
	set := OutboxSettings{}
	o := NewOutbox(dir, func() OutboxSettings { return set }, nil)
	if err := o.Enqueue("a", map[string]string{"taskId": "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, PendingFile)); !os.IsNotExist(err) {
		t.Fatal("no URL: nothing must be queued")
	}
	set = OutboxSettings{URL: srv.URL, Token: "gw", MaxAge: time.Hour}
	_ = o.Enqueue("a", map[string]string{"taskId": "a"})
	o.DeliverOnce(context.Background())
	if p, _ := readJSONL(filepath.Join(dir, PendingFile)); len(p) != 1 || p[0].Attempts != 1 {
		t.Fatalf("5xx must stay pending: %+v", p)
	}
	status.Store(200)
	o.DeliverOnce(context.Background())
	if p, _ := readJSONL(filepath.Join(dir, PendingFile)); len(p) != 0 {
		t.Fatalf("delivered must leave pending: %+v", p)
	}
	if gotToken.Load() != "gw" || bodies.Load() != 2 {
		t.Fatalf("token %v bodies %d", gotToken.Load(), bodies.Load())
	}
	status.Store(400)
	_ = o.Enqueue("b", map[string]string{"taskId": "b"})
	o.DeliverOnce(context.Background())
	dl, _ := readJSONL(filepath.Join(dir, DeadLetterFile))
	if len(dl) != 1 || dl[0].TaskID != "b" || dl[0].DeadLetteredAt == nil {
		t.Fatalf("400 must go to dead letter: %+v", dl)
	}
}
