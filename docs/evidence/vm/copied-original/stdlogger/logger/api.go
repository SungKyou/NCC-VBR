package logger

import (
	"context"
	"fmt"
	"log/slog"
)

// std는 패키지 전역 Manager입니다.
// 서비스 내 모든 패키지에서 logger.Info() 형식으로 바로 사용합니다.
// InitFromFile() 또는 NewWatcher().LoadAndApply() 호출 후 교체됩니다.
var std = NewManager()

// ─────────────────────────────────────────────
// 초기화 API
// ─────────────────────────────────────────────

// Init builds and installs the initial logger from LoggingConfig.
func Init(cfg LoggingConfig) error {
	l, err := Build(cfg)
	if err != nil {
		return err
	}
	std.Replace(l)
	return nil
}

// SetDefault replaces the standard logger and updates slog.Default.
func SetDefault(l *slog.Logger) {
	std.Replace(l)
	slog.SetDefault(l)
}

// Std returns the global manager.
// NewWatcher(vp, logger.Std()) 처럼 직접 viper를 사용할 때 필요합니다.
func Std() *Manager {
	return std
}

// ─────────────────────────────────────────────
// Entry — 속성 고정 / context 연동
// ─────────────────────────────────────────────

// With returns an Entry with fixed attributes.
// 컴포넌트명 등 반복되는 속성을 미리 고정할 때 사용합니다.
//
//	log := logger.With("component", "sync-worker", "pool_id", poolUUID)
//	log.Info("시작")
func With(args ...any) *Entry {
	return &Entry{manager: std, attrs: args}
}

// Ctx returns an Entry bound to the supplied context.
//
//	logger.Ctx(ctx).Info("request received", "request_id", reqID)
func Ctx(ctx context.Context) *Entry {
	return &Entry{ctx: ctx, manager: std}
}

// ─────────────────────────────────────────────
// Entry 구현
// ─────────────────────────────────────────────

// Entry is a convenient wrapper around slog.Logger and context.
//
// 2026-07-15 수정: 과거에는 생성 시점의 *slog.Logger를 필드에 고정 보관했으나,
// 이 경우 hot reload(watch.go)로 Manager.Replace()가 호출돼도 이미 만들어진
// Entry(대부분 main.go에서 서비스 기동 시 1회 생성되는 executor별 로거)는
// 새 writer/format/level을 반영하지 못하는 문제가 있었다("logger reloaded" 로그는
// 정상 출력되지만 실제 업무 로그는 예전 설정 그대로 나가는 증상).
// manager 참조 + 누적 attrs만 보관하고, 매 호출마다 manager.Logger()로
// "현재" 로거를 다시 가져와 attrs를 적용하는 방식으로 변경해 이 문제를 해결한다.
type Entry struct {
	ctx     context.Context
	manager *Manager
	attrs   []any
}

// normalizedCtx는 원본과 동일한 메서드명을 유지합니다.
func (e *Entry) normalizedCtx() context.Context {
	if e.ctx != nil {
		return e.ctx
	}
	return context.Background()
}

// currentManager는 manager가 비어있는(zero-value Entry 등) 경우를 대비해
// 패키지 전역 std로 안전하게 폴백합니다.
func (e *Entry) currentManager() *Manager {
	if e.manager != nil {
		return e.manager
	}
	return std
}

// live는 현재 시점의 slog.Logger에 누적 attrs를 적용해 반환합니다.
// 매 로그 호출마다 다시 계산되므로 hot reload 이후 즉시 새 설정을 반영합니다.
func (e *Entry) live() *slog.Logger {
	l := e.currentManager().Logger()
	if len(e.attrs) > 0 {
		l = l.With(e.attrs...)
	}
	return l
}

// With adds fixed attributes to this Entry.
func (e *Entry) With(args ...any) *Entry {
	merged := make([]any, 0, len(e.attrs)+len(args))
	merged = append(merged, e.attrs...)
	merged = append(merged, args...)
	return &Entry{ctx: e.ctx, manager: e.currentManager(), attrs: merged}
}

func (e *Entry) Trace(msg string, args ...any) {
	e.live().Log(e.normalizedCtx(), LevelTrace, msg, args...)
}
func (e *Entry) Debug(msg string, args ...any) {
	e.live().DebugContext(e.normalizedCtx(), msg, args...)
}
func (e *Entry) Info(msg string, args ...any) {
	e.live().InfoContext(e.normalizedCtx(), msg, args...)
}
func (e *Entry) Warn(msg string, args ...any) {
	e.live().WarnContext(e.normalizedCtx(), msg, args...)
}
func (e *Entry) Error(msg string, args ...any) {
	e.live().ErrorContext(e.normalizedCtx(), msg, args...)
}

func (e *Entry) Tracef(format string, args ...any) {
	e.Trace(fmt.Sprintf(format, args...))
}
func (e *Entry) Debugf(format string, args ...any) {
	e.Debug(fmt.Sprintf(format, args...))
}
func (e *Entry) Infof(format string, args ...any) {
	e.Info(fmt.Sprintf(format, args...))
}
func (e *Entry) Warnf(format string, args ...any) {
	e.Warn(fmt.Sprintf(format, args...))
}
func (e *Entry) Errorf(format string, args ...any) {
	e.Error(fmt.Sprintf(format, args...))
}

// ─────────────────────────────────────────────
// 패키지 수준 함수 (원본과 동일)
// ─────────────────────────────────────────────

func Trace(msg string, args ...any) { std.Log(context.Background(), LevelTrace, msg, args...) }
func Debug(msg string, args ...any) { std.Logger().Debug(msg, args...) }
func Info(msg string, args ...any)  { std.Logger().Info(msg, args...) }
func Warn(msg string, args ...any)  { std.Logger().Warn(msg, args...) }
func Error(msg string, args ...any) { std.Logger().Error(msg, args...) }

func Tracef(format string, args ...any) { Trace(fmt.Sprintf(format, args...)) }
func Debugf(format string, args ...any) { Debug(fmt.Sprintf(format, args...)) }
func Infof(format string, args ...any)  { Info(fmt.Sprintf(format, args...)) }
func Warnf(format string, args ...any)  { Warn(fmt.Sprintf(format, args...)) }
func Errorf(format string, args ...any) { Error(fmt.Sprintf(format, args...)) }
