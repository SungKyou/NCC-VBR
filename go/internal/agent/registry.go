package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// 작업 상태. 원본 BACKUP_INFO.status 값이다.
const (
	StatusFree      = 0 // 미사용 / 송신 완료
	StatusReceived  = 1 // 등록
	StatusQueued    = 2 // 대기열
	StatusRunning   = 3 // 처리 중
	StatusDone      = 5 // 처리 완료(조회·취소 응답 뒤)
	StatusRestarted = 6 // 재기동 때 처리 중이던 작업(원본 "Task cancel(mode 1)")
	StatusError     = 9 // 결과 송신 실패, 취소 등
)

// MaxSlots 는 원본 MAX_MSG_CNT 다.
const MaxSlots = 100

// Conn 은 응답을 돌려줄 접속이다. 접속이 끊기면 Alive 가 거짓이 된다.
type Conn interface {
	Send(frame []byte) error
	Alive() bool
}

// slot 은 작업 등록부 한 칸이다(원본 backup_info[i]).
type slot struct {
	status int
	msg    Message
	conn   Conn
	order  uint64 // 같은 우선순위 안의 도착 순서(원본 queue_seq 의 일련번호 부분)
	queued bool   // 비동기 대기열에 들어 있는가
}

// Registry 는 작업 등록부와 비동기 대기열이다.
// 원본은 공유 메모리 100칸과 System V 메시지 큐(우선순위 = msg_type) 였다.
type Registry struct {
	mu        sync.Mutex
	slots     [MaxSlots]slot
	next      uint64
	wake      chan struct{}
	statePath string
}

func NewRegistry(statePath string) *Registry {
	return &Registry{wake: make(chan struct{}, 1), statePath: statePath}
}

// Register 는 빈 칸에 전문을 등록한다. 빈 칸이 없으면 -1 이다(원본 결과 코드 8).
// 원본처럼 상태가 0 이거나 3 보다 큰 칸을 다시 쓴다.
func (r *Registry) Register(msg Message, conn Conn) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.slots {
		s := &r.slots[i]
		if s.status == StatusFree || s.status > StatusRunning {
			r.next++
			*s = slot{status: StatusReceived, msg: msg, conn: conn, order: r.next}
			return i
		}
	}
	return -1
}

// SetStatus 는 칸의 상태를 바꾼다.
func (r *Registry) SetStatus(i, status int) {
	r.mu.Lock()
	r.slots[i].status = status
	r.mu.Unlock()
	r.save()
}

// Enqueue 는 칸을 비동기 대기열에 넣는다(상태 2).
func (r *Registry) Enqueue(i int) {
	r.mu.Lock()
	r.slots[i].status = StatusQueued
	r.slots[i].queued = true
	r.mu.Unlock()
	r.save()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Dequeue 는 우선순위가 가장 높은(숫자가 작은) 작업을 꺼내 처리 중(3)으로 바꾼다.
// 원본 msgrcv(..., -2100000000L, 0): msg_type = 우선순위*1억 + 일련번호 가 가장 작은 것.
func (r *Registry) Dequeue(stop <-chan struct{}) (int, Message, bool) {
	for {
		r.mu.Lock()
		best := -1
		for i := range r.slots {
			s := &r.slots[i]
			if !s.queued {
				continue
			}
			if best < 0 || less(s, &r.slots[best]) {
				best = i
			}
		}
		if best >= 0 {
			s := &r.slots[best]
			s.queued = false
			s.status = StatusRunning
			msg := s.msg
			more := r.hasQueuedLocked()
			r.mu.Unlock()
			r.save()
			if more {
				select {
				case r.wake <- struct{}{}:
				default:
				}
			}
			return best, msg, true
		}
		r.mu.Unlock()
		select {
		case <-r.wake:
		case <-stop:
			return -1, Message{}, false
		}
	}
}

func (r *Registry) hasQueuedLocked() bool {
	for i := range r.slots {
		if r.slots[i].queued {
			return true
		}
	}
	return false
}

func less(a, b *slot) bool {
	if a.msg.Priority() != b.msg.Priority() {
		return a.msg.Priority() < b.msg.Priority()
	}
	return a.order < b.order
}

// Find 는 전문번호(17바이트 비교)로 칸을 찾는다. 원본처럼 앞에서부터 첫 칸이다.
func (r *Registry) Find(seq [seqLen]byte) (int, int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.findLocked(seq)
}

func (r *Registry) findLocked(seq [seqLen]byte) (int, int, bool) {
	for i := range r.slots {
		if r.slots[i].seqBytes() == seq {
			return i, r.slots[i].status, true
		}
	}
	return -1, 0, false
}

func (s *slot) seqBytes() [seqLen]byte {
	if s.msg.Raw == nil {
		return [seqLen]byte{}
	}
	return s.msg.SeqBytes()
}

// SeqAt 은 i 번째 칸의 전문번호 17바이트다(일괄 취소에서 쓴다).
func (r *Registry) SeqAt(i int) [seqLen]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.slots[i].seqBytes()
}

// CancelQueued 는 대기 중(1, 2)인 작업을 오류(9)로 바꾸고 대기열에서 뺀다. 원본 cancel_task_detail 의 뒷부분이다.
// 원본처럼 상태를 다시 읽어, 그 사이 1·2 가 아니게 바뀌었으면 changed 가 참이다(원본 3053~3061행 [NOTICE]).
// 돌려주는 값: 상태가 바뀌었는가, 대기열에서 뺐는가, 취소한 전문.
func (r *Registry) CancelQueued(i int) (changed, removed bool, msg Message) {
	r.mu.Lock()
	s := &r.slots[i]
	if s.status != StatusReceived && s.status != StatusQueued {
		r.mu.Unlock()
		return true, false, Message{}
	}
	s.status = StatusError
	removed = s.queued
	s.queued = false
	msg = s.msg
	r.mu.Unlock()
	r.save()
	return false, removed, msg
}

// Conn 은 칸의 접속이다.
func (r *Registry) Conn(i int) Conn {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.slots[i].conn
}

// Free 는 송신을 마친 칸을 비운다(원본 memset 0).
func (r *Registry) Free(i int) {
	r.mu.Lock()
	r.slots[i] = slot{}
	r.mu.Unlock()
	r.save()
}

// ── 재기동 대비 상태 파일 ──────────────────────────────────────
// 원본은 공유 메모리(키 1)와 메시지 큐가 agent 재기동 뒤에도 남아,
// 처리 중(3)이던 작업은 "Task cancel(mode 1)"(6)로 표시하고 대기 중(1, 2)이던 작업은 이어서 처리했다.
// 전환본은 비동기 작업만 상태 파일에 남겨 같은 일을 한다.

type savedTask struct {
	Status int    `json:"status"`
	Raw    []byte `json:"raw"`
	Order  uint64 `json:"order"`
}

func (r *Registry) save() {
	if r.statePath == "" {
		return
	}
	r.mu.Lock()
	var tasks []savedTask
	for i := range r.slots {
		s := &r.slots[i]
		if s.msg.Task() != 'A' {
			continue
		}
		if s.status == StatusReceived || s.status == StatusQueued || s.status == StatusRunning {
			tasks = append(tasks, savedTask{Status: s.status, Raw: s.msg.Raw, Order: s.order})
		}
	}
	r.mu.Unlock()
	data, err := json.Marshal(tasks)
	if err != nil {
		return
	}
	tmp := r.statePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, r.statePath)
}

// Restore 는 상태 파일을 읽는다. 처리 중이던 작업은 onRestarted 로 알리고(원본 '6' 기록),
// 대기 중이던 작업은 다시 대기열에 넣는다(접속은 없으므로 결과는 조회로 받는다).
func (r *Registry) Restore(onRestarted func(Message)) (requeued int) {
	if r.statePath == "" {
		return 0
	}
	data, err := os.ReadFile(r.statePath)
	if err != nil {
		return 0
	}
	var tasks []savedTask
	if json.Unmarshal(data, &tasks) != nil {
		return 0
	}
	sort.Slice(tasks, func(a, b int) bool { return tasks[a].Order < tasks[b].Order })
	for _, t := range tasks {
		msg := Message{Raw: t.Raw}
		if t.Status == StatusRunning {
			onRestarted(msg)
			continue
		}
		i := r.Register(msg, nil)
		if i < 0 {
			break
		}
		r.Enqueue(i)
		requeued++
	}
	r.save()
	return requeued
}

// StatePath 는 기본 상태 파일 경로다.
func StatePath(dir string) string {
	return filepath.Join(dir, "vbr_agent.state")
}
