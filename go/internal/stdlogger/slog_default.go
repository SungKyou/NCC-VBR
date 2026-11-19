// copied from ncc-xen stdlogger/logger/slog_default.go @043c6d5 (CP-VM-11), 2026-11-19. 변경: 이 머리 주석과 gofmt 정렬(공백·줄바꿈)뿐, 코드는 그대로.
package logger

import "log/slog"

func slogSetDefaultIfStdManager(m *Manager, l *slog.Logger) {
	if m == std {
		slog.SetDefault(l)
	}
}
