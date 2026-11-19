package logger

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// ValidateLoggingConfig checks whether the logging config is usable.
// time_format은 예약 키워드 또는 Go 레이아웃 문자열 모두 허용합니다.
// time_zone은 "utc", "local", IANA 타임존 모두 허용합니다.
func ValidateLoggingConfig(cfg LoggingConfig) error {
	switch strings.ToLower(strings.TrimSpace(cfg.Level)) {
	case "trace", "debug", "info", "warn", "warning", "error":
	default:
		return fmt.Errorf("invalid logging.level: %q", cfg.Level)
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Format)) {
	case "text", "json", "custom":
	default:
		return fmt.Errorf("invalid logging.format: %q", cfg.Format)
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Output)) {
	case "stdout", "file", "both":
	default:
		return fmt.Errorf("invalid logging.output: %q", cfg.Output)
	}

	// time_format: 예약어 또는 Go 레이아웃 문자열 — 비어있을 때만 오류
	if strings.TrimSpace(cfg.TimeFormat) == "" {
		return errors.New("logging.time_format is required")
	}

	// time_zone: "utc" | "local" | IANA 타임존 검증
	if err := validateTimeZone(cfg.TimeZone); err != nil {
		return err
	}

	if strings.ToLower(cfg.Output) != "stdout" && strings.TrimSpace(cfg.FileName) == "" {
		return errors.New("logging.file_name is required when output is file or both")
	}
	if cfg.MaxSizeMB <= 0 {
		return errors.New("logging.max_size_mb must be greater than zero")
	}
	if cfg.MaxBackups < 0 {
		return errors.New("logging.max_backups must be zero or greater")
	}
	if cfg.MaxAgeDays < 0 {
		return errors.New("logging.max_age_days must be zero or greater")
	}
	return nil
}

// validateTimeZone은 time_zone 값이 유효한지 확인합니다.
// "utc", "local" 또는 IANA 타임존 이름을 허용합니다.
func validateTimeZone(tz string) error {
	lower := strings.ToLower(strings.TrimSpace(tz))
	if lower == "utc" || lower == "local" || lower == "" {
		return nil
	}
	// IANA 타임존 유효성 검사
	if _, err := time.LoadLocation(tz); err != nil {
		return fmt.Errorf("invalid logging.time_zone: %q (use utc, local, or IANA name like Asia/Seoul)", tz)
	}
	return nil
}

// loadLocation은 time_zone 문자열을 *time.Location으로 변환합니다.
func loadLocation(tz string) *time.Location {
	lower := strings.ToLower(strings.TrimSpace(tz))
	switch lower {
	case "", "local":
		return time.Local
	case "utc":
		return time.UTC
	default:
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return time.Local // 파싱 실패 시 로컬 사용 (ValidateLoggingConfig에서 사전 검증됨)
		}
		return loc
	}
}

// Build creates a new logger from config.
func Build(cfg LoggingConfig) (*slog.Logger, error) {
	if err := ValidateLoggingConfig(cfg); err != nil {
		return nil, err
	}

	writer, err := buildWriter(cfg)
	if err != nil {
		return nil, err
	}

	level := parseLevel(cfg.Level)

	// time_format을 Go 레이아웃으로 변환 (예약어 or 직접 레이아웃)
	layout := resolveLayout(cfg.TimeFormat)
	loc := loadLocation(cfg.TimeZone)

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.AddSource,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			switch a.Key {
			case slog.TimeKey:
				if t, ok := a.Value.Any().(time.Time); ok {
					return slog.String(a.Key, t.In(loc).Format(layout))
				}
			case slog.LevelKey:
				if lv, ok := a.Value.Any().(slog.Level); ok {
					return slog.String(a.Key, levelName(lv))
				}
			}
			return a
		},
	}

	var handler slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "json":
		handler = slog.NewJSONHandler(writer, opts)
	case "custom":
		// 패턴 기반 커스텀 포맷
		// log_pattern이 비어있으면 기본값 사용
		pattern := cfg.LogPattern
		if pattern == "" {
			pattern = DefaultLogPattern
		}
		handler = NewCustomHandler(writer, opts, pattern, layout, loc)
	default:
		// text 포맷 (slog 기본)
		handler = slog.NewTextHandler(writer, opts)
	}

	l := slog.New(handler)
	if cfg.ServiceName != "" {
		l = l.With("service", cfg.ServiceName)
	}
	return l, nil
}

func buildWriter(cfg LoggingConfig) (io.Writer, error) {
	switch strings.ToLower(cfg.Output) {
	case "stdout":
		return os.Stdout, nil
	case "file":
		if err := ensureLogDir(cfg.FileName); err != nil {
			return nil, err
		}
		return newRotator(cfg), nil
	case "both":
		if err := ensureLogDir(cfg.FileName); err != nil {
			return nil, err
		}
		return io.MultiWriter(os.Stdout, newRotator(cfg)), nil
	default:
		return nil, fmt.Errorf("unsupported logging.output: %s", cfg.Output)
	}
}

func newRotator(cfg LoggingConfig) *lumberjack.Logger {
	return &lumberjack.Logger{
		Filename:   cfg.FileName,
		MaxSize:    cfg.MaxSizeMB,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAgeDays,
		LocalTime:  cfg.LocalTime,
		Compress:   cfg.Compress,
	}
}

func ensureLogDir(fileName string) error {
	dir := filepath.Dir(fileName)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func parseLevel(s string) slog.Leveler {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return LevelTrace
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func levelName(lv slog.Level) string {
	switch {
	case lv <= LevelTrace:
		return "TRACE"
	case lv <= slog.LevelDebug:
		return "DEBUG"
	case lv <= slog.LevelInfo:
		return "INFO"
	case lv <= slog.LevelWarn:
		return "WARN"
	default:
		return "ERROR"
	}
}
