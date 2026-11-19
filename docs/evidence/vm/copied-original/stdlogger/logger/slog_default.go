package logger

import "log/slog"

func slogSetDefaultIfStdManager(m *Manager, l *slog.Logger) {
	if m == std {
		slog.SetDefault(l)
	}
}
