// copied from ncc-xen stdlogger/logger/config.go @043c6d5 (CP-VM-11), 2026-11-19. 변경: 이 머리 주석과 gofmt 정렬(공백·줄바꿈)뿐, 코드는 그대로.
package logger

import "time"

// ─────────────────────────────────────────────────────────────────────────────
// 예약 키워드 (time_format에 쓸 수 있는 RFC 단축어)
//
//   config.yaml 예:
//     time_format: "rfc3339"            → 2006-01-02T15:04:05Z07:00
//     time_format: "rfc3339_millis"     → 2006-01-02T15:04:05.000Z07:00
//     time_format: "rfc3339_micros"     → 2006-01-02T15:04:05.000000Z07:00
//     time_format: "rfc3339_nanos"      → 2006-01-02T15:04:05.000000000Z07:00
//     time_format: "datetime"           → 2006-01-02 15:04:05
//     time_format: "datetime_millis"    → 2006-01-02 15:04:05.000
//     time_format: "date"               → 2006-01-02
//     time_format: "time"               → 15:04:05
//
// 위 키워드 외의 값은 Go 레이아웃 문자열로 직접 사용됩니다.
//   time_format: "2006/01/02 15:04:05"
//   time_format: "01-02 15:04:05.000"
//
// time_zone은 "utc", "local" 또는 IANA 타임존 이름을 지원합니다.
//   time_zone: "Asia/Seoul"
//   time_zone: "America/New_York"
// ─────────────────────────────────────────────────────────────────────────────

// reservedFormats는 예약 키워드 → Go 레이아웃 매핑입니다.
var reservedFormats = map[string]string{
	"rfc3339":         time.RFC3339,
	"rfc3339_millis":  "2006-01-02T15:04:05.000Z07:00",
	"rfc3339_micros":  "2006-01-02T15:04:05.000000Z07:00",
	"rfc3339_nanos":   "2006-01-02T15:04:05.000000000Z07:00",
	"datetime":        "2006-01-02 15:04:05",
	"datetime_millis": "2006-01-02 15:04:05.000",
	"date":            "2006-01-02",
	"time":            "15:04:05",
	// 하위 호환
	"legacy_local": "2006-01-02 15:04:05.000",
}

// Config is the root application config shape.
type Config struct {
	Logging LoggingConfig `mapstructure:"logging"`
}

// LoggingConfig defines the standard logger settings.
type LoggingConfig struct {
	Level      string `mapstructure:"level"`  // trace|debug|info|warn|error
	Format     string `mapstructure:"format"` // text|json|custom
	Output     string `mapstructure:"output"` // stdout|file|both
	FileName   string `mapstructure:"file_name"`
	MaxSizeMB  int    `mapstructure:"max_size_mb"`
	MaxBackups int    `mapstructure:"max_backups"`
	MaxAgeDays int    `mapstructure:"max_age_days"`
	Compress   bool   `mapstructure:"compress"`
	LocalTime  bool   `mapstructure:"local_time"`

	// TimeFormat: RFC 단축어 또는 Go 레이아웃 문자열 직접 입력 가능.
	// 예약어: rfc3339 | rfc3339_millis | rfc3339_micros | rfc3339_nanos
	//         datetime | datetime_millis | date | time
	// 직접 입력: "2006-01-02 15:04:05.000" 등
	TimeFormat string `mapstructure:"time_format"`

	// TimeZone: "utc" | "local" | IANA 타임존 (예: "Asia/Seoul")
	TimeZone string `mapstructure:"time_zone"`

	// LogPattern: format=custom일 때 출력 패턴.
	// 지원 토큰: {time} {level} {msg} {attrs} {service} {source}
	// {source}: add_source=true일 때 "파일명:줄번호" 출력
	LogPattern string `mapstructure:"log_pattern"`

	// AddSource: 소스 파일명:줄번호를 {source} 토큰으로 출력할지 여부
	AddSource   bool   `mapstructure:"add_source"`
	ServiceName string `mapstructure:"service_name"`
}

// DefaultLoggingConfig returns a conservative default configuration.
func DefaultLoggingConfig() LoggingConfig {
	return LoggingConfig{
		Level:       "info",
		Format:      "text",
		Output:      "both",
		FileName:    "./logs/app.log",
		MaxSizeMB:   50,
		MaxBackups:  5,
		MaxAgeDays:  28,
		Compress:    true,
		LocalTime:   true,
		TimeFormat:  "rfc3339_millis",
		TimeZone:    "local",
		LogPattern:  DefaultLogPattern,
		AddSource:   false,
		ServiceName: "app",
	}
}

// resolveLayout은 time_format 값을 Go 레이아웃 문자열로 변환합니다.
// 예약 키워드면 매핑 테이블에서 조회하고, 아니면 그대로 사용합니다.
func resolveLayout(timeFormat string) string {
	if layout, ok := reservedFormats[timeFormat]; ok {
		return layout
	}
	return timeFormat
}

// DefaultLogPattern은 custom 포맷의 기본 패턴입니다.
const DefaultLogPattern = "[{time}] [{level}] {msg} {attrs}"
