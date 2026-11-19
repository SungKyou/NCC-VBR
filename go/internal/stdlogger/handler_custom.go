// copied from ncc-xen stdlogger/logger/handler_custom.go @043c6d5 (CP-VM-11), 2026-11-19. 변경: 이 머리 주석과 gofmt 정렬(공백·줄바꿈)뿐, 코드는 그대로.
package logger

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"time"
)

// CustomHandler는 log_pattern 기반 포맷터입니다.
//
// 지원 토큰:
//
//	{time}    → time_format 적용된 시간
//	{level}   → INFO / DEBUG / WARN / ERROR / TRACE
//	{msg}     → 로그 메시지
//	{attrs}   → 나머지 key=value 쌍 (service 포함)
//	{service} → service 필드만 별도 추출 ({attrs}에서 제외됨)
//	{source}  → 소스 파일명:줄번호 (add_source: true 시 출력, false 시 빈 문자열)
//
// 예시 패턴:
//
//	"[{time}][{level}] {msg} {attrs}"
//	  → [2026-04-09 11:40:31.741][INFO] service started service=ncc-xen version=1.0.0
//
//	"[{time}][{level}][{service}] {msg} {attrs}"
//	  → [2026-04-09 11:40:31.741][INFO][ncc-xen] service started version=1.0.0
//
//	"[{time}][{level}] {source} {msg} {attrs}"
//	  → [2026-04-09 11:40:31.741][INFO] main.go:42 service started service=ncc-xen
type CustomHandler struct {
	w        io.Writer
	opts     *slog.HandlerOptions
	pattern  string // 예: "[{time}][{level}] {msg} {attrs}"
	layout   string // resolveLayout(timeFormat) 결과
	loc      *time.Location
	preAttrs []slog.Attr // WithAttrs()로 추가된 공통 속성
	groupPfx string      // WithGroup()으로 누적된 그룹 접두사 (예: "db.")
	mu       sync.Mutex  // io.Writer 쓰기 직렬화용
}

// NewCustomHandler는 패턴 기반 핸들러를 생성합니다.
func NewCustomHandler(w io.Writer, opts *slog.HandlerOptions, pattern, layout string, loc *time.Location) *CustomHandler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	if pattern == "" {
		pattern = DefaultLogPattern
	}
	return &CustomHandler{
		w:       w,
		opts:    opts,
		pattern: pattern,
		layout:  layout,
		loc:     loc,
	}
}

func (h *CustomHandler) Enabled(_ context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.opts.Level != nil {
		minLevel = h.opts.Level.Level()
	}
	return level >= minLevel
}

// WithAttrs는 공통 속성을 고정한 새 핸들러를 반환합니다.
// preAttrs는 불변 슬라이스로 복사되므로 race condition이 없습니다.
func (h *CustomHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.preAttrs)+len(attrs))
	copy(newAttrs, h.preAttrs)
	copy(newAttrs[len(h.preAttrs):], attrs)
	return &CustomHandler{
		w:        h.w,
		opts:     h.opts,
		pattern:  h.pattern,
		layout:   h.layout,
		loc:      h.loc,
		preAttrs: newAttrs,
		groupPfx: h.groupPfx,
	}
}

// WithGroup은 그룹명을 접두사로 적용한 새 핸들러를 반환합니다.
// 이후 추가되는 속성은 "그룹명.키=값" 형식으로 출력됩니다.
func (h *CustomHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &CustomHandler{
		w:        h.w,
		opts:     h.opts,
		pattern:  h.pattern,
		layout:   h.layout,
		loc:      h.loc,
		preAttrs: h.preAttrs,
		groupPfx: h.groupPfx + name + ".",
	}
}

func (h *CustomHandler) Handle(_ context.Context, r slog.Record) error {
	// ── 시간 ─────────────────────────────────────────────────
	// r.Time이 zero value일 수 있으므로 방어 처리
	t := r.Time
	if t.IsZero() {
		t = time.Now()
	}
	timeStr := t.In(h.loc).Format(h.layout)

	// ── 레벨 ─────────────────────────────────────────────────
	levelStr := levelName(r.Level)

	// ── 소스 ({source} 토큰용, add_source: true일 때만 채움) ──
	sourceStr := ""
	if h.opts.AddSource && r.PC != 0 {
		frames := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := frames.Next()
		// 파일 전체 경로 대신 파일명:줄번호만 표시
		file := f.File
		if idx := strings.LastIndex(file, "/"); idx >= 0 {
			file = file[idx+1:]
		}
		sourceStr = fmt.Sprintf("%s:%d", file, f.Line)
	}

	// ── 속성 수집 (preAttrs + 레코드 attrs) ──────────────────
	var serviceName string
	var attrBuf bytes.Buffer

	writeAttr := func(a slog.Attr) {
		// 그룹 접두사 적용
		key := h.groupPfx + a.Key

		if key == "service" {
			serviceName = a.Value.String()
			return
		}
		if attrBuf.Len() > 0 {
			attrBuf.WriteByte(' ')
		}
		attrBuf.WriteString(key)
		attrBuf.WriteByte('=')
		v := a.Value.String()
		// 공백 포함 시 따옴표
		if strings.ContainsAny(v, " \t\n") {
			fmt.Fprintf(&attrBuf, "%q", v)
		} else {
			attrBuf.WriteString(v)
		}
	}

	for _, a := range h.preAttrs {
		writeAttr(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(a)
		return true
	})

	// ── 패턴 치환 ─────────────────────────────────────────────
	out := h.pattern
	out = strings.ReplaceAll(out, "{time}", timeStr)
	out = strings.ReplaceAll(out, "{level}", levelStr)
	out = strings.ReplaceAll(out, "{msg}", r.Message)
	out = strings.ReplaceAll(out, "{service}", serviceName)
	out = strings.ReplaceAll(out, "{attrs}", attrBuf.String())
	out = strings.ReplaceAll(out, "{source}", sourceStr)

	// 후행 공백 정리 (attrs가 비었을 때)
	out = strings.TrimRight(out, " ")

	// mu는 io.Writer 쓰기만 직렬화합니다.
	// attrBuf는 로컬 변수이므로 goroutine 안전합니다.
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := fmt.Fprintln(h.w, out)
	return err
}
