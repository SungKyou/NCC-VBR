// Package job은 규약 1(Middleware → Backend 작업 알림)을 처리합니다.
//
// 모든 jobType은 이 핸들러를 단일 진입점으로 사용하며,
// jobType에 따라 적절한 Executor로 라우팅됩니다.
package job

import (
	"github.com/ncc/xen-backend/internal/i18n"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ncc/stdlogger/logger"
)

// Request는 규약 1 요청 바디입니다.
type Request struct {
	TaskID        string  `json:"taskId"        binding:"required"`
	JobType       string  `json:"jobType"       binding:"required"`
	Action        string  `json:"action"        binding:"required"`
	ExecutionType string  `json:"executionType"`
	SentAt        string  `json:"sentAt"`
	Payload       Payload `json:"payload"       binding:"required"`
}

// Payload는 규약 1의 payload 객체입니다.
type Payload struct {
	TargetType string                 `json:"targetType" binding:"required"`
	TargetUUID string                 `json:"targetUUID"`
	MasterIP   string                 `json:"masterIp"`
	// PoolUUID: 2026-09-18 코드 리뷰 지적 대응 — 명세서 1.3절(규약 1 요청
	// 구조)에 "masterIp 또는 poolUUID 중 하나 필수, 둘 다 있으면 poolUUID
	// 우선"이라고 정의돼 있었으나 이 구조체에 필드 자체가 없어 한 번도
	// 구현된 적이 없었음. 실제로는 Pool Master IP가 바뀌면(HA failover
	// 등) registry의 예전 masterIP와 신규 요청의 masterIp가 안 맞아 "Pool
	// 세션 없음"이 될 수 있는 문제가 있었음. 지금은 Middleware가 이 필드를
	// 아직 보내지 않아(masterIp만 신뢰성 있게 옴, 2026-09-17 실기 로그로
	// 확인) 당장 효과는 없지만, findSession()이 이미 poolUUID 우선 매칭을
	// 지원하므로 필드만 추가해두면 Middleware가 채워 보내기 시작하는
	// 즉시 동작한다.
	PoolUUID string                 `json:"poolUUID"`
	Options  map[string]interface{} `json:"options"`
}

// Response는 규약 1 즉시 응답(202 ACCEPTED) 바디입니다.
type Response struct {
	TaskID  string `json:"taskId"`
	Status  string `json:"status"`
	SentAt  string `json:"sentAt"`
	Message string `json:"message"`
}

// Executor는 jobType별로 실제 작업을 실행하는 인터페이스입니다.
// 새로운 jobType 추가 시 이 인터페이스를 구현합니다.
type Executor interface {
	// JobType은 처리 가능한 jobType 값을 반환합니다.
	JobType() string
	// Execute는 비동기로 실행될 실제 작업입니다. goroutine에서 호출됩니다.
	Execute(req Request)
}

// Handler는 규약 1 HTTP 핸들러입니다.
type Handler struct {
	executors map[string]Executor // jobType → Executor
	log       *logger.Entry
	gate      *JobGate // 전역/Pool별/jobType별 동시성 제한 + 리소스 락 (2026-09-15, 옵션 C)
}

// NewHandler는 Executor들을 등록한 핸들러를 생성합니다.
// gate가 nil이면 게이팅 없이(기존과 동일하게) 동작합니다.
func NewHandler(log *logger.Entry, gate *JobGate, executors ...Executor) *Handler {
	m := make(map[string]Executor, len(executors))
	for _, e := range executors {
		m[e.JobType()] = e
		log.Info("Job Executor 등록", "jobType", e.JobType())
	}
	return &Handler{executors: m, log: log, gate: gate}
}

// RegisterRoutes는 Gin 라우터에 엔드포인트를 등록합니다.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/jobs", h.Handle)
}

// Handle은 규약 1 요청을 수신하고 즉시 202를 반환합니다.
// 실제 작업은 goroutine에서 비동기로 실행됩니다.
func (h *Handler) Handle(c *gin.Context) {
	var req Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	// taskId 필수 검증
	if req.TaskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "taskId is required"})
		return
	}

	// taskId 멱등성 검사 (2026-09-15) — 이미 접수된 taskId면 재실행하지 않고
	// 202만 다시 응답합니다. destroy/move 같은 작업이 재전송으로 중복
	// 실행되는 것을 방지합니다.
	if isDuplicateTask(req.TaskID) {
		h.log.Warn(i18n.T("handler.l004"), "taskId", req.TaskID)
		c.JSON(http.StatusAccepted, Response{
			TaskID:  req.TaskID,
			Status:  "ACCEPTED",
			SentAt:  time.Now().UTC().Format(time.RFC3339),
			Message: "duplicate taskId — already accepted, not re-executed",
		})
		return
	}

	// ── 규약 1 수신 전문 로그 — pretty print ───────────────────────
	// 2026-09-18: 원본 req를 그대로 로그에 남기면 payload.options에 담긴
	// password/master_password/dst_password/chap_password 등 인프라
	// 자격증명이 평문으로 로그 파일에 기록됨(redact.go 참조). 로그 전용
	// 사본(redactedRequestForLogging)에 대해서만 마스킹을 적용하고, 실제
	// 실행에는 원본 req를 그대로 사용한다(아래 executor.Execute(req) 등).
	if reqJSON, err := json.MarshalIndent(redactedRequestForLogging(req), "", "  "); err == nil {
		h.log.Info(i18n.T("handler.l001")+ string(reqJSON) + "\n")
	}

	// jobType에 맞는 Executor 조회
	executor, ok := h.executors[req.JobType]
	if !ok {
		h.log.Warn(i18n.T("handler.l003"), "jobType", req.JobType)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "unsupported jobType: " + req.JobType,
		})
		return
	}

	// 접수 기록 durable 저장 (2026-09-15) — 202 반환 전에 파일에 먼저
	// 기록해서, 이후 프로세스가 crash해도 재기동 시 "완료 확인 안 됨"으로
	// 남아있는 걸 알 수 있게 합니다(자동 재실행은 하지 않음 — Xen Orchestra
	// 참고: 재기동 시 interrupted로 표시만 하고 재시도는 Middleware/운영자
	// 판단에 맡깁니다).
	//
	// 🔧 2026-10-29: acceptTask → tryAcceptTask. 위 isDuplicateTask는 빠른 경로로
	// 그대로 두고, 여기서 확인+기록을 한 락 안에서 다시 수행해 거의 동시에 들어온
	// 같은 taskId 중 하나만 실행되게 합니다(나머지는 위와 같은 중복 응답).
	accepted, accErr := tryAcceptTask(req.TaskID, req.JobType, req.Action, req.Payload.TargetType, req.Payload.TargetUUID)
	if !accepted {
		h.log.Warn(i18n.T("handler.l004"), "taskId", req.TaskID)
		c.JSON(http.StatusAccepted, Response{
			TaskID:  req.TaskID,
			Status:  "ACCEPTED",
			SentAt:  time.Now().UTC().Format(time.RFC3339),
			Message: "duplicate taskId — already accepted, not re-executed",
		})
		return
	}
	if accErr != nil {
		// 접수 기록 파일 쓰기 실패(디스크 풀 등) — 실행은 계속하지만, 프로세스가
		// 중간에 죽으면 재기동 시 interrupted 복구 대상에서 빠진다는 경고.
		h.log.Error("접수 기록(accepted.jsonl) 저장 실패 — 재기동 시 interrupted 복구 대상에서 빠질 수 있음",
			"taskId", req.TaskID, "error", accErr)
	}

	// 즉시 202 응답 (규약 1 응답 명세)
	resp := Response{
		TaskID:  req.TaskID,
		Status:  "ACCEPTED",
		SentAt:  time.Now().UTC().Format(time.RFC3339),
		Message: "",
	}

	// ── 규약 1 응답 전문 로그 ──────────────────────────────────────
	h.log.Info(i18n.T("handler.l002"),
		"taskId", resp.TaskID,
		"status", resp.Status,
		"sentAt", resp.SentAt,
	)

	c.JSON(http.StatusAccepted, resp)

	// 비동기 실행 (규약 2는 Executor 내부에서 완료 후 전송)
	// 전역/Pool별/jobType별 동시성 제한 + destructive action 리소스 락 적용
	// (2026-09-15, 옵션 C). gate가 nil이면 기존과 동일하게 무조건 실행.
	if h.gate == nil {
		go executor.Execute(req)
		return
	}
	go h.gate.Guard(context.Background(), req, func() {
		executor.Execute(req)
	})
}
