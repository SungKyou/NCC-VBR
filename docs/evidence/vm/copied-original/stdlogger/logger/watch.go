package logger

import (
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// Watcher reloads logging settings when the config file changes.
type Watcher struct {
	vp      *viper.Viper
	manager *Manager

	mu    sync.Mutex
	timer *time.Timer
}

// NewWatcher creates a watcher for logging config.
func NewWatcher(vp *viper.Viper, manager *Manager) *Watcher {
	return &Watcher{vp: vp, manager: manager}
}

// LoadAndApply reads config and installs the initial logger.
func (w *Watcher) LoadAndApply() error {
	cfg, err := w.readConfig()
	if err != nil {
		return err
	}
	return w.apply(cfg, "logger initialized")
}

// Watch starts config file watching with debounce.
func (w *Watcher) Watch() {
	w.vp.OnConfigChange(func(e fsnotify.Event) {
		w.scheduleReload(e)
	})
	w.vp.WatchConfig()
}

func (w *Watcher) scheduleReload(_ fsnotify.Event) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.timer != nil {
		w.timer.Stop()
	}
	// 500ms debounce — 파일 저장 중 여러 번 이벤트가 오는 것을 방지
	w.timer = time.AfterFunc(500*time.Millisecond, func() {
		if err := w.reload(); err != nil {
			// 현재 manager의 logger로 출력 (log.Printf 대신)
			w.manager.Logger().Error("logger reload failed", "error", err)
		}
	})
}

func (w *Watcher) reload() error {
	// OnConfigChange 콜백은 viper가 이미 파일을 다시 읽은 후 호출됩니다.
	// ReadInConfig() 재호출 없이 현재 viper 상태를 Unmarshal합니다.
	cfg, err := w.unmarshalConfig()
	if err != nil {
		return err
	}
	return w.apply(cfg, "logger reloaded")
}

// apply는 config로 새 logger를 빌드하고 원자적으로 교체합니다.
func (w *Watcher) apply(cfg LoggingConfig, msg string) error {
	l, err := Build(cfg)
	if err != nil {
		return err
	}
	w.manager.Replace(l)
	slogSetDefaultIfStdManager(w.manager, l)
	w.manager.Logger().Info(msg,
		"level", cfg.Level,
		"format", cfg.Format,
		"output", cfg.Output,
		"file", cfg.FileName,
		"time_format", cfg.TimeFormat,
		"time_zone", cfg.TimeZone,
	)
	return nil
}

// readConfig는 viper에서 파일을 읽고 LoggingConfig를 반환합니다.
// 최초 로드(LoadAndApply)에서만 사용합니다.
func (w *Watcher) readConfig() (LoggingConfig, error) {
	if err := w.vp.ReadInConfig(); err != nil {
		return LoggingConfig{}, err
	}
	return w.unmarshalConfig()
}

// unmarshalConfig는 현재 viper 상태를 LoggingConfig로 변환합니다.
// reload에서는 ReadInConfig 없이 이 함수만 호출합니다.
func (w *Watcher) unmarshalConfig() (LoggingConfig, error) {
	var root Config
	if err := w.vp.Unmarshal(&root); err != nil {
		return LoggingConfig{}, err
	}
	// level이 비어있는 경우에만 기본값 적용 (zero value 전체 교체 방지)
	if root.Logging.Level == "" {
		root.Logging = DefaultLoggingConfig()
	}
	return root.Logging, nil
}
