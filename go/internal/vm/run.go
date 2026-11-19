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

// outcome 은 작업 하나의 결과다.
type outcome struct {
	result  map[string]interface{}
	message string
	err     error
}

// progress 는 진행률을 메모리에 바로 반영하고, 파일에는 간격·변화량을 보고 쓴다(03 설계 10장).
type progress struct {
	s        *Service
	id       string
	interval time.Duration
	sendProg bool

	mu        sync.Mutex
	lastStep  string
	lastPct   int
	lastWrite time.Time
}

// set 은 단계와 진행률을 바꾼다. msg 는 사람이 읽는 문구다.
// note 는 처리 중 작업 기록의 Result 에 값을 바로 적는다(재기동 때 읽는다).
func (p *progress) note(key string, val interface{}) {
	_, _, _ = p.s.store.Update(p.id, true, func(r *jobstore.Record) {
		if r.Result == nil {
			r.Result = map[string]interface{}{}
		}
		r.Result[key] = val
	})
}

func (p *progress) set(step string, pct int, msg string) {
	p.mu.Lock()
	now := p.s.now()
	persist := step != p.lastStep || (now.Sub(p.lastWrite) >= p.interval && pct-p.lastPct >= 1)
	if persist {
		p.lastStep, p.lastPct, p.lastWrite = step, pct, now
	}
	p.mu.Unlock()
	rec, changed, _ := p.s.store.Update(p.id, persist, func(r *jobstore.Record) {
		if r.Step == "canceling" {
			return // 취소 중 표시는 덮지 않는다
		}
		r.Step = step
		if pct > r.Percent {
			r.Percent = pct
		}
		r.Message = msg
	})
	if changed && persist && p.sendProg {
		p.s.report(rec)
	}
}

// run 은 작업 하나를 처리하고 끝난 상태를 기록한다.
func (s *Service) run(ctx context.Context, job *Job, cfg *vmconfig.Loaded, a *active) {
	id := job.Req.TaskID
	log := logger.With("taskId", id, "action", job.Req.Action, "targetUUID", job.Req.Payload.TargetUUID)
	t := s.now().UTC()
	_, changed, err := s.store.Update(id, true, func(r *jobstore.Record) {
		r.Status = jobstore.StatusRunning
		r.Step = "login"
		r.StartedAt = &t
		r.Message = i18n.T("vm.job.running")
	})
	if err != nil || !changed {
		return
	}
	log.Info(i18n.T("vm.log.started"))
	p := &progress{s: s, id: id, interval: cfg.ProgressInterval(), sendProg: cfg.ResultReport.SendProgress,
		lastStep: "login", lastWrite: s.now()}
	var out outcome
	switch job.Req.Action {
	case ActionExport:
		out = s.runExport(ctx, job, cfg, p, log)
	case ActionRestore:
		out = s.runRestore(ctx, job, cfg, p, log)
	case ActionVerify:
		out = s.runVerify(ctx, job, cfg, p, log)
	}
	s.finish(ctx, id, out, a, log)
}

// finish 는 결과를 기록하고 보고한다. 취소·중단·시간 초과를 오류 코드로 나눈다.
func (s *Service) finish(ctx context.Context, id string, out outcome, a *active, log *logger.Entry) {
	s.mu.Lock()
	reason := a.reason
	s.mu.Unlock()
	t := s.now().UTC()
	status := jobstore.StatusSucceeded
	var ei *jobstore.ErrorInfo
	msg := out.message
	if out.err != nil {
		je := classify(out.err)
		status = jobstore.StatusFailed
		switch {
		case reason == reasonCanceled:
			status, je.Code = jobstore.StatusCanceled, CodeCanceled
			msg = i18n.T("vm.job.canceled")
		case reason == reasonInterrupted:
			status, je.Code = jobstore.StatusInterrupted, CodeInterrupted
			msg = i18n.T("vm.job.interrupted")
		case errors.Is(ctx.Err(), context.DeadlineExceeded) || je.Code == CodeTimeout:
			je.Code = CodeTimeout
			msg = i18n.T("vm.job.timeout")
		default:
			msg = i18n.T("vm.job.failed", je.Code)
		}
		ei = &jobstore.ErrorInfo{Code: je.Code, Detail: je.Detail}
	}
	rec, _, err := s.store.Update(id, true, func(r *jobstore.Record) {
		r.Status = status
		r.Step = "done"
		if status == jobstore.StatusSucceeded {
			r.Percent = 100
		}
		r.FinishedAt = &t
		r.Message = msg
		r.Result = out.result
		r.Error = ei
	})
	if err != nil {
		log.Error(i18n.T("vm.log.store_failed"), "error", err)
	}
	if ei != nil {
		log.Warn(i18n.T("vm.log.finished_error"), "status", status, "code", ei.Code, "detail", ei.Detail)
	} else {
		log.Info(i18n.T("vm.log.finished"), "status", status)
	}
	s.report(rec)
}

// ResultStatus 는 상태를 ncc-xen 규약 2 result 로 바꾼다.
func ResultStatus(status string) string {
	switch status {
	case jobstore.StatusQueued, jobstore.StatusRunning:
		return "processing"
	case jobstore.StatusSucceeded:
		return "success"
	}
	return "failure"
}

// ResultBody 는 ncc-xen 규약 2 본문이다(05 문서 API-VM-07).
func ResultBody(r jobstore.Record, now time.Time) map[string]interface{} {
	opts := map[string]interface{}{}
	for k, v := range r.Result {
		opts[k] = v
	}
	if ResultStatus(r.Status) == "processing" {
		opts["percent"] = r.Percent
		opts["step"] = r.Step
	}
	if r.Error != nil {
		opts["error_code"] = r.Error.Code
		opts["error_detail"] = r.Error.Detail
	}
	return map[string]interface{}{
		"taskId":  r.TaskID,
		"jobType": r.JobType,
		"action":  r.Action,
		"result":  ResultStatus(r.Status),
		"sentAt":  now.UTC().Format(time.RFC3339),
		"payload": map[string]interface{}{
			"targetType": r.TargetType,
			"targetUUID": r.TargetUUID,
			"message":    r.Message,
			"options":    opts,
		},
	}
}

// View 는 조회 API 응답이다(05 문서 API-VM-02). withRequest 가 거짓이면 request 를 뺀다(목록).
func View(r jobstore.Record, withRequest bool) map[string]interface{} {
	opts := r.Result
	if opts == nil {
		opts = map[string]interface{}{}
	}
	v := map[string]interface{}{
		"taskId":     r.TaskID,
		"jobType":    r.JobType,
		"action":     r.Action,
		"status":     r.Status,
		"result":     ResultStatus(r.Status),
		"percent":    r.Percent,
		"step":       r.Step,
		"acceptedAt": r.AcceptedAt.UTC().Format(time.RFC3339),
		"payload": map[string]interface{}{
			"targetType": r.TargetType,
			"targetUUID": r.TargetUUID,
			"message":    r.Message,
			"options":    opts,
		},
	}
	if r.StartedAt != nil {
		v["startedAt"] = r.StartedAt.UTC().Format(time.RFC3339)
	}
	if r.FinishedAt != nil {
		v["finishedAt"] = r.FinishedAt.UTC().Format(time.RFC3339)
	}
	if r.Error != nil {
		v["error"] = r.Error
	}
	if withRequest && len(r.Request) > 0 {
		v["request"] = r.Request
	}
	return v
}

func completedAt(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func pctMsg(key string, pct int) string { return i18n.T(key, pct) }

func seconds(d time.Duration) int { return int(d.Seconds()) }
