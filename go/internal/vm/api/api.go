// Package api 는 vbr_vm HTTP 처리기다(docs/vm/05-interface-spec.md 8장). JSON 을 업무 호출로 바꾸고 결과를 JSON 으로 돌려준다.
// 업무 규칙은 두지 않는다(vm 패키지).
//
// 접수 처리기의 모양은 ncc-xen backend/internal/job/handler.go Handle(@043c6d5, CP-VM-01)을 따른다:
// 바인딩 → 즉시 202 {taskId, status:"ACCEPTED", sentAt, message:""}, 중복 taskId 는 202 + 고정 문장.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ncc-vbr/internal/apiauth"
	"ncc-vbr/internal/i18n"
	"ncc-vbr/internal/jobstore"
	logger "ncc-vbr/internal/stdlogger"
	"ncc-vbr/internal/vm"
)

// DuplicateMessage 는 중복 taskId 응답 문장이다(ncc-xen v3.36 과 같은 고정 문장, locale 무관).
const DuplicateMessage = "duplicate taskId — already accepted, not re-executed"

// maxBody 는 요청 본문 최대 크기다(05 문서 8.1).
const maxBody = 1 << 20

// Version 은 서비스 판이다(DEC-VM-04).
const Version = "4.000"

// Handler 는 처리기 묶음이다.
type Handler struct {
	svc *vm.Service
	cfg vm.Config
	now func() time.Time
}

// NewRouter 는 gin 엔진을 만든다.
func NewRouter(svc *vm.Service, cfg vm.Config) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	h := &Handler{svc: svc, cfg: cfg, now: time.Now}
	r.GET("/health", h.health)
	g := r.Group("/api/v1", apiauth.Middleware(func() (bool, string) {
		c := cfg.Current()
		return c.APIAuth.Enabled, c.APIToken
	}))
	g.POST("/jobs", h.submit)
	g.GET("/jobs", h.list)
	g.POST("/jobs/cancel-queued", h.cancelQueued)
	g.GET("/jobs/:taskId", h.get)
	g.POST("/jobs/:taskId/cancel", h.cancel)
	return r
}

func errBody(code, msg, field string) gin.H {
	b := gin.H{"error": msg, "code": code}
	if field != "" {
		b["field"] = field
	}
	return b
}

func statusOf(code string) int {
	switch code {
	case vm.CodeTaskNotFound:
		return http.StatusNotFound
	case vm.CodeAlreadyFinished:
		return http.StatusConflict
	case vm.CodeQueueFull, vm.CodeStopping:
		return http.StatusServiceUnavailable
	case vm.CodeStoreError:
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}

func (h *Handler) submit(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		c.JSON(http.StatusBadRequest, errBody(vm.CodeInvalidRequest, "request body is too large or unreadable", ""))
		return
	}
	var req vm.Request
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, errBody(vm.CodeInvalidRequest, "invalid request body: "+err.Error(), ""))
		return
	}
	cfg := h.cfg.Current()
	job, rerr := vm.Validate(req, cfg.Crypto.Enabled)
	if rerr != nil {
		logger.With("taskId", req.TaskID).Warn(i18n.T("vm.log.rejected"), "code", rerr.Code, "field", rerr.Field)
		c.JSON(statusOf(rerr.Code), errBody(rerr.Code, rerr.Msg, rerr.Field))
		return
	}
	_, dup, rerr := h.svc.Submit(job)
	if rerr != nil {
		c.JSON(statusOf(rerr.Code), errBody(rerr.Code, rerr.Msg, rerr.Field))
		return
	}
	resp := gin.H{"taskId": req.TaskID, "status": "ACCEPTED", "sentAt": h.now().UTC().Format(time.RFC3339), "message": ""}
	if dup {
		logger.With("taskId", req.TaskID).Info(i18n.T("vm.log.duplicate"))
		resp["message"] = DuplicateMessage
	}
	c.JSON(http.StatusAccepted, resp)
}

func (h *Handler) get(c *gin.Context) {
	rec, err := h.svc.Get(c.Param("taskId"))
	if errors.Is(err, jobstore.ErrNotFound) {
		c.JSON(http.StatusNotFound, errBody(vm.CodeTaskNotFound, "task not found: "+c.Param("taskId"), ""))
		return
	}
	c.JSON(http.StatusOK, vm.View(rec, true))
}

func (h *Handler) list(c *gin.Context) {
	f := jobstore.Filter{Limit: 100, Action: c.Query("action")}
	if s := c.Query("status"); s != "" {
		f.Statuses = strings.Split(s, ",")
	}
	if s := c.Query("since"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			c.JSON(http.StatusBadRequest, errBody(vm.CodeInvalidField, "since must be RFC 3339", "since"))
			return
		}
		f.Since = t
	}
	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 1000 {
			c.JSON(http.StatusBadRequest, errBody(vm.CodeInvalidField, "limit must be 1..1000", "limit"))
			return
		}
		f.Limit = n
	}
	recs := h.svc.List(f)
	items := make([]interface{}, 0, len(recs))
	for _, r := range recs {
		items = append(items, vm.View(r, false))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "count": len(items)})
}

func (h *Handler) cancel(c *gin.Context) {
	rec, status, rerr := h.svc.Cancel(c.Param("taskId"))
	if rerr != nil {
		c.JSON(status, errBody(rerr.Code, rerr.Msg, ""))
		return
	}
	c.JSON(status, vm.View(rec, false))
}

func (h *Handler) cancelQueued(c *gin.Context) {
	ids := h.svc.CancelQueued()
	if ids == nil {
		ids = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"canceled": ids, "count": len(ids)})
}

func (h *Handler) health(c *gin.Context) {
	running, queued, stopping := h.svc.Counts()
	cfg := h.cfg.Current()
	b := gin.H{"status": "ok", "service": "vbr_vm", "version": Version,
		"startedAt": h.svc.StartedAt().Format(time.RFC3339), "running": running, "queued": queued,
		"authEnabled": cfg.APIAuth.Enabled, "cryptoEnabled": cfg.Crypto.Enabled}
	if stopping {
		b["status"] = "stopping"
		c.JSON(http.StatusServiceUnavailable, b)
		return
	}
	c.JSON(http.StatusOK, b)
}
