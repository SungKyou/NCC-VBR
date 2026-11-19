package vm

import (
	"context"
	"errors"
	"sync"
	"time"

	"ncc-vbr/internal/i18n"
	"ncc-vbr/internal/jobstore"
	logger "ncc-vbr/internal/stdlogger"
	"ncc-vbr/internal/vmconfig"
)

// Config 는 현재 설정을 준다(vmconfig.Store). 시험에서 바꿔 끼운다.
type Config interface {
	Current() *vmconfig.Loaded
}

// 취소 이유.
const (
	reasonCanceled    = "canceled"
	reasonInterrupted = "interrupted"
)

type active struct {
	cancel context.CancelFunc
	reason string
}

// Service 는 접수·대기열·작업자·취소·종료를 맡는다(03 설계 6장).
type Service struct {
	cfg    Config
	store  *jobstore.Store
	outbox *jobstore.Outbox
	now    func() time.Time

	mu       sync.Mutex
	cond     *sync.Cond
	queue    []string
	pending  map[string]*Job
	running  map[string]*active
	workers  int
	stopping bool
	wg       sync.WaitGroup
	base     context.Context
	stopBase context.CancelFunc

	startedAt time.Time
}

// New 는 서비스를 만든다. outbox 는 nil 이어도 된다.
func New(cfg Config, store *jobstore.Store, outbox *jobstore.Outbox) *Service {
	base, stop := context.WithCancel(context.Background())
	s := &Service{cfg: cfg, store: store, outbox: outbox, now: time.Now,
		pending: map[string]*Job{}, running: map[string]*active{}, base: base, stopBase: stop}
	s.cond = sync.NewCond(&s.mu)
	s.startedAt = s.now().UTC()
	return s
}

// Start 는 작업자를 띄운다.
func (s *Service) Start() { s.AdjustWorkers() }

// AdjustWorkers 는 작업자 수를 설정에 맞춘다. 늘리면 바로 띄우고, 줄이면 남는 작업자가 지금 작업을 끝낸 뒤 멈춘다.
func (s *Service) AdjustWorkers() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return
	}
	target := s.cfg.Current().Concurrency.MaxJobs
	for s.workers < target {
		s.workers++
		s.wg.Add(1)
		go s.worker()
	}
	s.cond.Broadcast()
}

// Submit 은 작업을 접수한다. 같은 taskId 면 dup 이 참이고 기존 기록을 돌려준다.
func (s *Service) Submit(job *Job) (rec jobstore.Record, dup bool, rerr *ReqError) {
	cfg := s.cfg.Current()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return jobstore.Record{}, false, &ReqError{Code: CodeStopping, Msg: "service is stopping"}
	}
	if old, err := s.store.Get(job.Req.TaskID); err == nil {
		return old, true, nil
	}
	if len(s.queue) >= cfg.Concurrency.MaxQueue {
		return jobstore.Record{}, false, &ReqError{Code: CodeQueueFull, Msg: "queue is full"}
	}
	r := jobstore.Record{
		TaskID: job.Req.TaskID, JobType: job.Req.JobType, Action: job.Req.Action,
		TargetType: job.Req.Payload.TargetType, TargetUUID: job.Req.Payload.TargetUUID,
		Message: i18n.T("vm.job.queued"), Request: Redacted(job.Req), AcceptedAt: s.now().UTC(),
	}
	rec, err := s.store.Accept(r)
	if errors.Is(err, jobstore.ErrDuplicate) {
		return rec, true, nil
	}
	if err != nil {
		return jobstore.Record{}, false, &ReqError{Code: CodeStoreError, Msg: err.Error()}
	}
	s.queue = append(s.queue, job.Req.TaskID)
	s.pending[job.Req.TaskID] = job
	s.cond.Signal()
	logger.With("taskId", job.Req.TaskID, "action", job.Req.Action, "targetUUID", job.Req.Payload.TargetUUID).
		Info(i18n.T("vm.log.accepted"), "request", string(rec.Request))
	return rec, false, nil
}

func (s *Service) worker() {
	defer s.wg.Done()
	for {
		s.mu.Lock()
		for len(s.queue) == 0 && !s.stopping && s.workers <= s.cfg.Current().Concurrency.MaxJobs {
			s.cond.Wait()
		}
		if s.stopping || s.workers > s.cfg.Current().Concurrency.MaxJobs {
			s.workers--
			s.mu.Unlock()
			return
		}
		id := s.queue[0]
		s.queue = s.queue[1:]
		job := s.pending[id]
		delete(s.pending, id)
		cfg := s.cfg.Current()
		ctx, cancel := context.WithTimeout(s.base, cfg.Timeout())
		a := &active{cancel: cancel}
		s.running[id] = a
		s.mu.Unlock()

		s.run(ctx, job, cfg, a)
		cancel()

		s.mu.Lock()
		delete(s.running, id)
		s.mu.Unlock()
	}
}

// Cancel 은 작업 하나를 취소한다. 대기 중이면 바로 canceled(200), 처리 중이면 취소를 시작한다(202).
func (s *Service) Cancel(taskID string) (jobstore.Record, int, *ReqError) {
	s.mu.Lock()
	for i, id := range s.queue {
		if id != taskID {
			continue
		}
		s.queue = append(s.queue[:i], s.queue[i+1:]...)
		delete(s.pending, id)
		s.mu.Unlock()
		rec := s.finishQueued(taskID, jobstore.StatusCanceled, CodeCanceled, "canceled by API while queued", "vm.job.canceled")
		return rec, 200, nil
	}
	if a, ok := s.running[taskID]; ok {
		a.reason = reasonCanceled
		s.mu.Unlock()
		rec, _, _ := s.store.Update(taskID, true, func(r *jobstore.Record) {
			r.Step = "canceling"
			r.Message = i18n.T("vm.job.canceling")
		})
		a.cancel()
		return rec, 202, nil
	}
	s.mu.Unlock()
	rec, err := s.store.Get(taskID)
	if err != nil {
		return jobstore.Record{}, 404, &ReqError{Code: CodeTaskNotFound, Msg: "task not found: " + taskID}
	}
	return rec, 409, &ReqError{Code: CodeAlreadyFinished, Msg: "task already finished: " + rec.Status}
}

// CancelQueued 는 대기 중 작업을 모두 취소한다(처리 중은 건드리지 않는다).
func (s *Service) CancelQueued() []string {
	s.mu.Lock()
	ids := append([]string(nil), s.queue...)
	s.queue = nil
	for _, id := range ids {
		delete(s.pending, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.finishQueued(id, jobstore.StatusCanceled, CodeCanceled, "canceled by cancel-queued", "vm.job.canceled")
	}
	return ids
}

func (s *Service) finishQueued(id, status, code, detail, msgKey string) jobstore.Record {
	t := s.now().UTC()
	rec, changed, _ := s.store.Update(id, true, func(r *jobstore.Record) {
		r.Status = status
		r.Step = "done"
		r.FinishedAt = &t
		r.Message = i18n.T(msgKey)
		r.Error = &jobstore.ErrorInfo{Code: code, Detail: detail}
	})
	if changed {
		s.report(rec)
	}
	return rec
}

// Get, List 는 조회다.
func (s *Service) Get(taskID string) (jobstore.Record, error) { return s.store.Get(taskID) }

// List 는 목록이다.
func (s *Service) List(f jobstore.Filter) []jobstore.Record { return s.store.List(f) }

// Counts 는 처리 중·대기 수다(/health).
func (s *Service) Counts() (running, queued int, stopping bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.running), len(s.queue), s.stopping
}

// StartedAt 은 기동 시각이다.
func (s *Service) StartedAt() time.Time { return s.startedAt }

// Shutdown 은 새 접수를 막고, 대기 중은 interrupted 로, 처리 중은 취소 정리 뒤 interrupted 로 남긴다.
// 처리 중 작업의 정리를 wait 만큼 기다린다.
func (s *Service) Shutdown(wait time.Duration) {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return
	}
	s.stopping = true
	ids := append([]string(nil), s.queue...)
	s.queue = nil
	for _, a := range s.running {
		a.reason = reasonInterrupted
		a.cancel()
	}
	s.cond.Broadcast()
	s.mu.Unlock()
	for _, id := range ids {
		s.finishQueued(id, jobstore.StatusInterrupted, CodeInterrupted, "service stopped while queued", "vm.job.interrupted")
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(wait):
		logger.Warn(i18n.T("vm.log.shutdown_timeout"), "wait", wait.String())
	}
	s.stopBase()
}

// ReportRecords 는 재기동 때 interrupted 로 바뀐 작업을 결과 보고에 넣는다(ncc-xen 과 같다).
func (s *Service) ReportRecords(recs []jobstore.Record) {
	for _, r := range recs {
		s.report(r)
	}
}

// report 는 결과 보고를 대기열에 넣는다(설정 시).
func (s *Service) report(rec jobstore.Record) {
	if s.outbox == nil {
		return
	}
	if err := s.outbox.Enqueue(rec.TaskID, ResultBody(rec, s.now())); err != nil {
		logger.With("taskId", rec.TaskID).Error(i18n.T("vm.log.outbox_failed"), "error", err)
	}
}
