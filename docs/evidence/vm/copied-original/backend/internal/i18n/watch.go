package i18n

import (
	"fmt"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"

	"github.com/ncc/stdlogger/logger"
)

// localeFileConfig는 sysconfig.yaml 최상위의 locale 키만 뽑아옵니다.
// 다른 섹션(redis, logging 등)은 이 구조체와 무관하며 무시됩니다.
type localeFileConfig struct {
	Locale string `mapstructure:"locale"`
}

// Watcher는 sysconfig.yaml의 locale 값 변경을 감시해 SetLocale을 호출합니다.
// stdlogger/logger/watch.go의 Watcher와 동일한 패턴(viper + fsnotify +
// 500ms 디바운스)을 그대로 따릅니다 — 이미 이 프로젝트에서 검증된 방식입니다.
type Watcher struct {
	vp *viper.Viper

	mu    sync.Mutex
	timer *time.Timer
}

// InitOption은 InitFromFile의 동작을 조정합니다. stdlogger.InitOption과 의도적으로
// 동일한 형태를 유지합니다.
type InitOption struct {
	ConfigName  string   // 기본값 "sysconfig"
	ConfigType  string   // 기본값 "yaml"
	ConfigPaths []string // 기본값 ["."]
	HotReload   *bool    // nil(생략)이면 기본값 true
}

func (o InitOption) hotReloadEnabled() bool {
	if o.HotReload == nil {
		return true
	}
	return *o.HotReload
}

func defaultOption() InitOption {
	t := true
	return InitOption{
		ConfigName:  "sysconfig",
		ConfigType:  "yaml",
		ConfigPaths: []string{"."},
		HotReload:   &t,
	}
}

var stdWatcher *Watcher

// InitFromFile은 sysconfig.yaml에서 초기 locale을 읽어 적용하고,
// HotReload가 켜져 있으면(기본값) 이후 파일 변경도 계속 감시합니다.
//
// stdlogger.InitFromFile과 마찬가지로 서비스 기동 시 한 번만 호출합니다.
// stdlogger와 별도의 viper 인스턴스를 사용하므로(동일 파일을 각자 읽음),
// 기존 로깅 설정 감시 로직과는 완전히 독립적으로 동작합니다.
func InitFromFile(opts ...InitOption) error {
	opt := defaultOption()
	if len(opts) > 0 {
		o := opts[0]
		if o.ConfigName != "" {
			opt.ConfigName = o.ConfigName
		}
		if o.ConfigType != "" {
			opt.ConfigType = o.ConfigType
		}
		if len(o.ConfigPaths) > 0 {
			opt.ConfigPaths = o.ConfigPaths
		}
		if o.HotReload != nil {
			opt.HotReload = o.HotReload
		}
	}

	vp := viper.New()
	vp.SetConfigName(opt.ConfigName)
	vp.SetConfigType(opt.ConfigType)
	for _, p := range opt.ConfigPaths {
		vp.AddConfigPath(p)
	}
	vp.SetDefault("locale", DefaultLocale)

	if err := vp.ReadInConfig(); err != nil {
		return fmt.Errorf("i18n: config read failed (%s.%s): %w", opt.ConfigName, opt.ConfigType, err)
	}

	w := &Watcher{vp: vp}
	stdWatcher = w

	if err := w.applyCurrent("locale initialized"); err != nil {
		return err
	}

	if opt.hotReloadEnabled() {
		vp.OnConfigChange(func(e fsnotify.Event) {
			w.scheduleReload(e)
		})
		vp.WatchConfig()
	}

	return nil
}

func (w *Watcher) scheduleReload(_ fsnotify.Event) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.timer != nil {
		w.timer.Stop()
	}
	// stdlogger와 동일하게 500ms 디바운스 — 파일 저장 중 여러 이벤트 방지
	w.timer = time.AfterFunc(500*time.Millisecond, func() {
		if err := w.applyCurrent("locale reloaded"); err != nil {
			logger.With("component", "i18n").Error("locale reload failed", "error", err)
		}
	})
}

// applyCurrent는 현재 viper 상태에서 locale 값을 읽어 SetLocale로 적용합니다.
func (w *Watcher) applyCurrent(msg string) error {
	var cfg localeFileConfig
	if err := w.vp.Unmarshal(&cfg); err != nil {
		return fmt.Errorf("i18n: config parse failed: %w", err)
	}
	loc := cfg.Locale
	if loc == "" {
		loc = DefaultLocale
	}
	if !IsSupported(loc) {
		logger.With("component", "i18n").Warn(
			"지원하지 않는 locale — 이전 값 유지",
			"requested", loc, "current", CurrentLocale(),
		)
		return nil
	}
	SetLocale(loc)
	logger.With("component", "i18n").Info(msg, "locale", loc)
	return nil
}
