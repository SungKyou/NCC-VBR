package jobstore

// outbox.go 는 결과 보고(ncc-xen 규약 2, 05 문서 API-VM-07)를 보내고 다시 보낸다.
// 설계는 ncc-xen backend/internal/job/task_durability.go 의 outbox(@043c6d5, CP-VM-04)를 따른다.
//   - outbox_pending.jsonl(대기) → 보내면 outbox_delivered.jsonl, 포기하면 outbox_deadletter.jsonl.
//   - 5xx·429·408·연결 오류는 다시 보낸다. 대기 시간이 max_age 를 넘으면 Dead Letter.
//     그 밖의 4xx(400·401 등)는 다시 보내도 안 되므로 바로 Dead Letter.
//   - 대기가 비면 delivered 를 비운다(ncc-xen 2026-09-29 무한 증가 수정).
//   - Dead Letter 는 보관 기간이 지난 줄만 지운다(ncc-xen 2026-09-30).
// 바뀐 점: ncc-xen 은 Middleware 가 항상 있다. vbr_vm 은 주소가 비어 있으면 아무것도 쌓지 않는다.
// 재기동 복구는 jobs.jsonl 이 맡으므로 accepted.jsonl 은 두지 않는다.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Outbox 파일 이름.
const (
	PendingFile    = "outbox_pending.jsonl"
	DeliveredFile  = "outbox_delivered.jsonl"
	DeadLetterFile = "outbox_deadletter.jsonl"
)

// OutboxSettings 는 결과 보고 설정이다(sysconfig.yaml result_report.*). 즉시 반영된다.
type OutboxSettings struct {
	URL              string
	Token            string
	MaxAge           time.Duration
	DeadLetterRetain time.Duration
	RequestTimeout   time.Duration
}

// Envelope 는 outbox 한 줄이다.
type Envelope struct {
	TaskID         string          `json:"taskId"`
	CreatedAt      time.Time       `json:"createdAt"`
	Attempts       int             `json:"attempts"`
	LastError      string          `json:"lastError,omitempty"`
	DeadLetteredAt *time.Time      `json:"deadLetteredAt,omitempty"`
	Body           json.RawMessage `json:"body"`
}

// Outbox 는 결과 보고 대기열이다.
type Outbox struct {
	mu       sync.Mutex
	dir      string
	settings func() OutboxSettings
	client   *http.Client
	now      func() time.Time
	wake     chan struct{}
	logf     func(level, msg string, args ...any)
}

// NewOutbox 는 dir 에 대기열을 둔다. settings 는 보낼 때마다 다시 읽는다.
func NewOutbox(dir string, settings func() OutboxSettings, logf func(level, msg string, args ...any)) *Outbox {
	if logf == nil {
		logf = func(string, string, ...any) {}
	}
	return &Outbox{dir: dir, settings: settings, client: &http.Client{}, now: time.Now,
		wake: make(chan struct{}, 1), logf: logf}
}

// Enqueue 는 결과를 대기열에 넣는다. 주소가 비어 있으면 넣지 않는다.
func (o *Outbox) Enqueue(taskID string, body interface{}) error {
	if o.settings().URL == "" {
		return nil
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	env := Envelope{TaskID: taskID, CreatedAt: o.now().UTC(), Body: b}
	o.mu.Lock()
	err = appendJSONL(filepath.Join(o.dir, PendingFile), env)
	o.mu.Unlock()
	if err != nil {
		return err
	}
	select {
	case o.wake <- struct{}{}:
	default:
	}
	return nil
}

// Run 은 5초마다(또는 Enqueue 직후) 대기열을 보낸다. 1시간마다 Dead Letter 를 정리한다. ctx 가 끝나면 멈춘다.
func (o *Outbox) Run(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	prune := time.NewTicker(time.Hour)
	defer prune.Stop()
	o.pruneDeadLetter()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-o.wake:
		case <-prune.C:
			o.pruneDeadLetter()
			continue
		}
		o.DeliverOnce(ctx)
	}
}

// DeliverOnce 는 대기열을 한 번 돌며 보낸다.
func (o *Outbox) DeliverOnce(ctx context.Context) {
	o.mu.Lock()
	defer o.mu.Unlock()
	pendingPath := filepath.Join(o.dir, PendingFile)
	pending, err := readJSONL(pendingPath)
	if err != nil || len(pending) == 0 {
		if err == nil {
			o.resetDeliveredIfDrained()
		}
		return
	}
	set := o.settings()
	var keep []Envelope
	for _, env := range pending {
		if ctx.Err() != nil {
			keep = append(keep, env)
			continue
		}
		if set.URL == "" {
			keep = append(keep, env) // 주소를 지웠으면 다시 생길 때까지 둔다
			continue
		}
		retry, sendErr := o.send(ctx, set, env.Body)
		env.Attempts++
		switch {
		case sendErr == nil:
			_ = appendJSONL(filepath.Join(o.dir, DeliveredFile), env)
		case !retry || (set.MaxAge > 0 && o.now().Sub(env.CreatedAt) > set.MaxAge):
			env.LastError = sendErr.Error()
			t := o.now().UTC()
			env.DeadLetteredAt = &t
			if werr := appendJSONL(filepath.Join(o.dir, DeadLetterFile), env); werr != nil {
				keep = append(keep, env)
			}
			o.logf("warn", "result report moved to dead letter", "taskId", env.TaskID, "error", sendErr)
		default:
			env.LastError = sendErr.Error()
			keep = append(keep, env)
		}
	}
	if err := writeJSONL(pendingPath, keep); err != nil {
		o.logf("error", "outbox rewrite failed", "error", err)
		return
	}
	if len(keep) == 0 {
		o.resetDeliveredIfDrained()
	}
}

// send 는 한 건을 보낸다. 다시 보내도 되는 실패면 retry 가 참이다.
func (o *Outbox) send(ctx context.Context, set OutboxSettings, body []byte) (retry bool, err error) {
	timeout := set.RequestTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, set.URL, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if set.Token != "" {
		req.Header.Set("X-Internal-Token", set.Token)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return true, err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return false, nil
	}
	err = errors.New("HTTP " + resp.Status)
	switch {
	case resp.StatusCode >= 500, resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode == http.StatusRequestTimeout:
		return true, err
	}
	return false, err
}

// resetDeliveredIfDrained 는 대기가 비었을 때만 delivered 를 비운다.
func (o *Outbox) resetDeliveredIfDrained() {
	p := filepath.Join(o.dir, DeliveredFile)
	if st, err := os.Stat(p); err == nil && st.Size() > 0 {
		_ = os.Truncate(p, 0)
	}
}

// pruneDeadLetter 는 보관 기간이 지난 Dead Letter 줄만 지운다. 지울 것이 없으면 파일을 다시 쓰지 않는다.
func (o *Outbox) pruneDeadLetter() {
	retain := o.settings().DeadLetterRetain
	if retain <= 0 {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	p := filepath.Join(o.dir, DeadLetterFile)
	list, err := readJSONL(p)
	if err != nil || len(list) == 0 {
		return
	}
	cut := o.now().Add(-retain)
	var keep []Envelope
	for _, env := range list {
		at := env.CreatedAt
		if env.DeadLetteredAt != nil {
			at = *env.DeadLetteredAt
		}
		if at.Before(cut) {
			continue
		}
		keep = append(keep, env)
	}
	if len(keep) != len(list) {
		_ = writeJSONL(p, keep)
	}
}

func appendJSONL(path string, v interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(b, '\n'))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

func readJSONL(path string) ([]Envelope, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Envelope
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var env Envelope
		if json.Unmarshal([]byte(line), &env) == nil {
			out = append(out, env)
		}
	}
	return out, sc.Err()
}

// writeJSONL 은 임시 파일에 쓰고 바꿔 끼운다.
func writeJSONL(path string, list []Envelope) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, env := range list {
		b, err := json.Marshal(env)
		if err != nil {
			continue
		}
		_, _ = w.Write(append(b, '\n'))
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
