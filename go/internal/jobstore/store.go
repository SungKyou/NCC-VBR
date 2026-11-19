// Package jobstore 는 vbr_vm 작업 상태를 로컬 JSON Lines 파일에 기록한다(docs/vm/03-tobe-design.md 10장).
//
// 설계는 ncc-xen backend/internal/job/task_durability.go(@043c6d5, CP-VM-04)를 따른다.
//   - append-only JSON Lines. 압축은 임시 파일에 쓰고 os.Rename 으로 바꿔 끼운다(도중에 죽어도 파일이 깨지지 않는다).
//   - 기동 때 파일을 줄 단위로 읽고 taskId 마다 마지막 상태만 메모리에 둔다(전체 파싱으로 메모리가 튀던 사례를 피한다).
//   - 재기동 때 끝나지 않은 작업은 interrupted 로 바꾸고 자동으로 다시 하지 않는다.
//
// ncc-xen 의 accepted.jsonl 은 접수 여부만 담는다. 조회 API(05 문서 API-VM-02)가 상태·진행률을 돌려줘야 해서
// 상태가 바뀔 때마다 한 줄을 쓰는 jobs.jsonl 을 새로 설계했다. 업무(백업·복구)는 모른다.
package jobstore

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// 작업 상태(05 문서 API-VM-02 status).
const (
	StatusQueued      = "queued"
	StatusRunning     = "running"
	StatusSucceeded   = "succeeded"
	StatusFailed      = "failed"
	StatusCanceled    = "canceled"
	StatusInterrupted = "interrupted"
)

// Finished 는 끝난 상태인지 본다.
func Finished(status string) bool {
	switch status {
	case StatusSucceeded, StatusFailed, StatusCanceled, StatusInterrupted:
		return true
	}
	return false
}

// ErrorInfo 는 실패·취소·중단의 오류다. Detail 은 원문이다.
type ErrorInfo struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// Record 는 작업 하나의 상태다. 파일의 한 줄과 메모리 표가 같은 모양이다.
// SnapshotLeftKey 는 처리 중 작업 기록의 Result 에 임시 스냅샷 uuid 를 적는 키다. 작업이 정상으로 끝나면
// 최종 결과가 Result 를 바꿔 쓰므로 사라지고, 재기동 때 남아 있으면 그 스냅샷이 풀에 남았을 수 있다.
const SnapshotLeftKey = "snapshot_left"

type Record struct {
	TaskID     string                 `json:"taskId"`
	JobType    string                 `json:"jobType"`
	Action     string                 `json:"action"`
	Status     string                 `json:"status"`
	Percent    int                    `json:"percent"`
	Step       string                 `json:"step"`
	AcceptedAt time.Time              `json:"acceptedAt"`
	StartedAt  *time.Time             `json:"startedAt,omitempty"`
	FinishedAt *time.Time             `json:"finishedAt,omitempty"`
	TargetType string                 `json:"targetType"`
	TargetUUID string                 `json:"targetUUID,omitempty"`
	Message    string                 `json:"message,omitempty"`
	Result     map[string]interface{} `json:"result,omitempty"`
	Error      *ErrorInfo             `json:"error,omitempty"`
	Request    json.RawMessage        `json:"request,omitempty"` // 비밀 값을 가린 요청
}

// clone 은 바깥에 돌려줄 사본이다(map 은 얕게 복사한다. 결과 map 은 끝난 뒤 바꾸지 않는다).
func (r *Record) clone() Record {
	c := *r
	if r.Result != nil {
		c.Result = make(map[string]interface{}, len(r.Result))
		for k, v := range r.Result {
			c.Result[k] = v
		}
	}
	return c
}

// ErrDuplicate 는 같은 taskId 가 이미 있을 때다.
var ErrDuplicate = errors.New("duplicate taskId")

// ErrNotFound 는 taskId 가 없을 때다.
var ErrNotFound = errors.New("task not found")

// FileName 은 작업 기록 파일 이름이다.
const FileName = "jobs.jsonl"

// Store 는 작업 기록이다. 메서드는 여러 고루틴에서 불러도 된다.
type Store struct {
	mu        sync.Mutex
	path      string
	f         *os.File
	recs      map[string]*Record
	retention time.Duration
	now       func() time.Time
}

// Open 은 dir 의 기록을 읽는다. 끝나지 않은 작업은 interrupted 로 바꾸고 그 목록을 돌려준다.
func Open(dir string, retention time.Duration, now func() time.Time) (*Store, []Record, error) {
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	s := &Store{path: filepath.Join(dir, FileName), recs: map[string]*Record{}, retention: retention, now: now}
	if err := s.load(); err != nil {
		return nil, nil, err
	}
	var interrupted []Record
	t := now().UTC()
	for _, r := range s.recs {
		if Finished(r.Status) {
			continue
		}
		detail := "service restarted while the task was " + stepOrQueued(r) // 단계를 바꾸기 전에 만든다
		if u, ok := r.Result[SnapshotLeftKey].(string); ok && u != "" {
			detail += "; temporary snapshot may remain: " + u // REQ-VM-17, 05 9장 INTERRUPTED
		}
		r.Status = StatusInterrupted
		r.Step = "done"
		r.FinishedAt = &t
		r.Error = &ErrorInfo{Code: "INTERRUPTED", Detail: detail}
		interrupted = append(interrupted, r.clone())
	}
	if err := s.compactLocked(); err != nil {
		return nil, nil, err
	}
	sort.Slice(interrupted, func(i, j int) bool { return interrupted[i].AcceptedAt.Before(interrupted[j].AcceptedAt) })
	return s, interrupted, nil
}

func stepOrQueued(r *Record) string {
	if r.StartedAt == nil {
		return StatusQueued
	}
	return StatusRunning + " (step " + r.Step + ")"
}

// load 는 파일을 줄 단위로 읽어 taskId 마다 마지막 줄만 남긴다. 해석할 수 없는 줄은 건너뛴다.
func (s *Store) load() error {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r Record
		if json.Unmarshal([]byte(line), &r) != nil || r.TaskID == "" {
			continue
		}
		s.recs[r.TaskID] = &r
	}
	return sc.Err()
}

// compactLocked 는 taskId 마다 마지막 상태만 남기고, 끝난 지 retention 이 지난 것은 빼서 다시 쓴다.
func (s *Store) compactLocked() error {
	if s.f != nil {
		_ = s.f.Close()
		s.f = nil
	}
	cut := s.now().Add(-s.retention)
	ids := make([]string, 0, len(s.recs))
	for id, r := range s.recs {
		if s.retention > 0 && r.FinishedAt != nil && r.FinishedAt.Before(cut) {
			delete(s.recs, id)
			continue
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return s.recs[ids[i]].AcceptedAt.Before(s.recs[ids[j]].AcceptedAt) })
	tmp := s.path + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(out)
	for _, id := range ids {
		b, err := json.Marshal(s.recs[id])
		if err != nil {
			continue
		}
		_, _ = w.Write(append(b, '\n'))
	}
	if err := w.Flush(); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	return s.openAppend()
}

func (s *Store) openAppend() error {
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	s.f = f
	return nil
}

// appendLocked 는 한 줄을 쓴다.
func (s *Store) appendLocked(r *Record) error {
	if s.f == nil {
		if err := s.openAppend(); err != nil {
			return err
		}
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.f.Write(append(b, '\n'))
	return err
}

// Accept 는 새 작업을 queued 로 기록한다. 중복 확인과 기록을 한 잠금 안에서 한다(ncc-xen v3.36 동시 중복 수정).
// 같은 taskId 가 있으면 ErrDuplicate 와 기존 상태를 돌려준다. 기록에 실패하면 접수하지 않는다.
func (s *Store) Accept(r Record) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.recs[r.TaskID]; ok {
		return old.clone(), ErrDuplicate
	}
	r.Status = StatusQueued
	r.Step = StatusQueued
	if r.AcceptedAt.IsZero() {
		r.AcceptedAt = s.now().UTC()
	}
	if err := s.appendLocked(&r); err != nil {
		return Record{}, fmt.Errorf("task store write failed: %w", err)
	}
	s.recs[r.TaskID] = &r
	return r.clone(), nil
}

// Update 는 fn 으로 상태를 바꾼다. persist 가 거짓이면 메모리만 바꾼다(잦은 진행률 갱신용).
// 끝난 작업은 바꾸지 않는다(거짓을 돌려준다).
func (s *Store) Update(taskID string, persist bool, fn func(*Record)) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.recs[taskID]
	if !ok {
		return Record{}, false, ErrNotFound
	}
	if Finished(r.Status) {
		return r.clone(), false, nil
	}
	fn(r)
	if !persist && !Finished(r.Status) {
		return r.clone(), true, nil
	}
	return r.clone(), true, s.appendLocked(r)
}

// Get 은 작업 하나다.
func (s *Store) Get(taskID string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.recs[taskID]
	if !ok {
		return Record{}, ErrNotFound
	}
	return r.clone(), nil
}

// Filter 는 목록 조회 조건이다(05 문서 API-VM-03).
type Filter struct {
	Statuses []string
	Action   string
	Since    time.Time
	Limit    int
}

// List 는 접수 시각 최신순 목록이다.
func (s *Store) List(f Filter) []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, 0, len(s.recs))
	for _, r := range s.recs {
		if len(f.Statuses) > 0 && !contains(f.Statuses, r.Status) {
			continue
		}
		if f.Action != "" && r.Action != f.Action {
			continue
		}
		if !f.Since.IsZero() && r.AcceptedAt.Before(f.Since) {
			continue
		}
		out = append(out, r.clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AcceptedAt.After(out[j].AcceptedAt) })
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// SetRetention 은 보관 기간을 바꾼다(설정 즉시 반영).
func (s *Store) SetRetention(d time.Duration) {
	s.mu.Lock()
	s.retention = d
	s.mu.Unlock()
}

// Compact 는 파일을 정리한다(기동 때와 1시간마다).
func (s *Store) Compact() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.compactLocked()
}

// Close 는 파일을 닫는다.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}
