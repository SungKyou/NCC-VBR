// copied from ncc-xen stdlogger/logger/manager.go @043c6d5 (CP-VM-11), 2026-11-19. 변경: 이 머리 주석과 gofmt 정렬(공백·줄바꿈)뿐, 코드는 그대로.
package logger

import (
	"context"
	"log/slog"
	"sync/atomic"
)

// Custom trace level. slog has debug as the lowest built-in level.
const LevelTrace = slog.Level(-8)

// Manager stores the current logger and allows atomic replacement during reload.
type Manager struct {
	cur atomic.Value // stores *slog.Logger
}

// NewManager creates a manager with slog.Default as a fallback.
func NewManager() *Manager {
	m := &Manager{}
	m.cur.Store(slog.Default())
	return m
}

// Logger returns the current logger instance.
func (m *Manager) Logger() *slog.Logger {
	v := m.cur.Load()
	if v == nil {
		return slog.Default()
	}
	return v.(*slog.Logger)
}

// Replace atomically swaps the current logger.
func (m *Manager) Replace(l *slog.Logger) {
	if l == nil {
		return
	}
	m.cur.Store(l)
}

// Log sends a structured log with the supplied level.
func (m *Manager) Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	m.Logger().Log(ctx, level, msg, args...)
}
