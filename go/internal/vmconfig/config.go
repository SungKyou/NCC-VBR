// Package vmconfig 는 vbr_vm 의 sysconfig.yaml 을 읽고, 파일이 바뀌면 재기동 없이 반영한다(DEC-VM-17).
//
// 방식은 ncc-xen i18n/watch.go, stdlogger/logger/watch.go 와 같다: viper + fsnotify, 500ms 디바운스.
// 바뀐 파일을 읽지 못하면 이전 설정을 그대로 쓴다. 키마다 반영 시점은 docs/vm/05-interface-spec.md 10장이다.
// logging.* 은 stdlogger 가 같은 파일을 따로 감시해 반영한다(ncc-xen 과 같은 구조).
package vmconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"

	"ncc-vbr/internal/secret"
)

// 토큰 복호화 용도 문자열(ncc-xen 과 같은 방식: sysconfig.<경로>).
const (
	PurposeAPIToken     = "sysconfig.api_auth.VBR_VM_API_TOKEN"
	PurposeGatewayToken = "sysconfig.api_auth.GATEWAY_INTERNAL_TOKEN"
)

// 환경 변수 이름(ncc-xen 운영 규칙 ①: 같은 이름의 환경 변수가 설정보다 우선).
const (
	EnvAPIToken     = "VBR_VM_API_TOKEN"
	EnvGatewayToken = "GATEWAY_INTERNAL_TOKEN"
)

// Config 는 sysconfig.yaml 의 vbr_vm 부분이다.
type Config struct {
	Server struct {
		Port   int    `mapstructure:"port"`
		Listen string `mapstructure:"listen"`
	} `mapstructure:"server"`
	APIAuth struct {
		Enabled      bool   `mapstructure:"enabled"`
		APIToken     string `mapstructure:"vbr_vm_api_token"`
		GatewayToken string `mapstructure:"gateway_internal_token"`
	} `mapstructure:"api_auth"`
	Crypto struct {
		Enabled     bool   `mapstructure:"enabled"`
		KeyringFile string `mapstructure:"keyring_file"`
	} `mapstructure:"crypto"`
	Locale string `mapstructure:"locale"`
	Backup struct {
		BasePath             string   `mapstructure:"base_path"`
		SandboxMode          bool     `mapstructure:"sandbox_mode"`
		AllowedAbsolutePaths []string `mapstructure:"allowed_absolute_paths"`
		TimeoutHours         int      `mapstructure:"timeout_hours"`
		VerifyAfterExport    bool     `mapstructure:"verify_after_export"`
	} `mapstructure:"backup"`
	Concurrency struct {
		MaxJobs  int `mapstructure:"max_jobs"`
		MaxQueue int `mapstructure:"max_queue"`
	} `mapstructure:"concurrency"`
	XAPI struct {
		VerifySSL           bool `mapstructure:"verify_ssl"`
		ProgressIntervalSec int  `mapstructure:"progress_interval_sec"`
	} `mapstructure:"xapi"`
	TaskStore struct {
		Dir           string `mapstructure:"dir"`
		RetentionDays int    `mapstructure:"retention_days"`
	} `mapstructure:"task_store"`
	ResultReport struct {
		URL                     string `mapstructure:"url"`
		SendProgress            bool   `mapstructure:"send_progress"`
		MaxAgeHours             int    `mapstructure:"max_age_hours"`
		DeadLetterRetentionDays int    `mapstructure:"deadletter_retention_days"`
	} `mapstructure:"result_report"`
}

// Loaded 는 읽고 검사하고 비밀 값을 푼 설정이다. 바꾸지 않고 통째로 갈아 끼운다.
type Loaded struct {
	Config
	APIToken     string            // 풀린 받는 토큰(환경 변수 우선)
	GatewayToken string            // 풀린 보내는 토큰
	Protector    *secret.Protector // crypto.enabled 일 때 키링. 아니면 nil 일 수 있다
	LoadedAt     time.Time
}

// ProgressInterval 은 진행률 간격이다.
func (l *Loaded) ProgressInterval() time.Duration {
	return time.Duration(l.XAPI.ProgressIntervalSec) * time.Second
}

// Timeout 은 작업 시간 제한이다.
func (l *Loaded) Timeout() time.Duration { return time.Duration(l.Backup.TimeoutHours) * time.Hour }

func setDefaults(vp *viper.Viper) {
	vp.SetDefault("server.port", 8090)
	vp.SetDefault("server.listen", "0.0.0.0")
	vp.SetDefault("api_auth.enabled", true)
	vp.SetDefault("crypto.enabled", true)
	vp.SetDefault("crypto.keyring_file", "./key.yaml")
	vp.SetDefault("locale", "ko")
	vp.SetDefault("backup.base_path", "./backups")
	vp.SetDefault("backup.timeout_hours", 4)
	vp.SetDefault("concurrency.max_jobs", 3)
	vp.SetDefault("concurrency.max_queue", 100)
	vp.SetDefault("xapi.progress_interval_sec", 5)
	vp.SetDefault("task_store.dir", "./data/tasks")
	vp.SetDefault("task_store.retention_days", 30)
	vp.SetDefault("result_report.max_age_hours", 24)
	vp.SetDefault("result_report.deadletter_retention_days", 30)
}

// knownTopKeys 는 이 서비스가 아는 최상위 키다. 그 밖은 경고하고 무시한다.
var knownTopKeys = map[string]bool{
	"server": true, "api_auth": true, "crypto": true, "locale": true, "logging": true, "backup": true,
	"concurrency": true, "xapi": true, "task_store": true, "result_report": true,
}

// restartKeys 는 바꿔도 재기동해야 반영되는 값이다.
func restartKeys(old, cur *Loaded) []string {
	var keys []string
	if old.Server.Port != cur.Server.Port {
		keys = append(keys, "server.port")
	}
	if old.Server.Listen != cur.Server.Listen {
		keys = append(keys, "server.listen")
	}
	if old.TaskStore.Dir != cur.TaskStore.Dir {
		keys = append(keys, "task_store.dir")
	}
	return keys
}

// parse 는 viper 상태를 Loaded 로 바꾸고 검사한다.
func parse(vp *viper.Viper, now time.Time) (*Loaded, []string, error) {
	var c Config
	if err := vp.Unmarshal(&c); err != nil {
		return nil, nil, fmt.Errorf("config parse failed: %w", err)
	}
	var warns []string
	for _, k := range vp.AllKeys() {
		top := strings.SplitN(k, ".", 2)[0]
		if !knownTopKeys[top] {
			warns = append(warns, "unknown config key ignored: "+k)
		}
	}
	if err := validate(&c); err != nil {
		return nil, warns, err
	}
	l := &Loaded{Config: c, LoadedAt: now}
	if err := resolveSecrets(l); err != nil {
		return nil, warns, err
	}
	return l, warns, nil
}

func validate(c *Config) error {
	switch {
	case c.Server.Port < 1 || c.Server.Port > 65535:
		return fmt.Errorf("server.port out of range: %d", c.Server.Port)
	case c.Concurrency.MaxJobs < 1 || c.Concurrency.MaxJobs > 10:
		return fmt.Errorf("concurrency.max_jobs must be 1..10: %d", c.Concurrency.MaxJobs)
	case c.Concurrency.MaxQueue < 1:
		return fmt.Errorf("concurrency.max_queue must be >= 1: %d", c.Concurrency.MaxQueue)
	case c.Backup.TimeoutHours < 1:
		return fmt.Errorf("backup.timeout_hours must be >= 1: %d", c.Backup.TimeoutHours)
	case c.XAPI.ProgressIntervalSec < 1:
		return fmt.Errorf("xapi.progress_interval_sec must be >= 1: %d", c.XAPI.ProgressIntervalSec)
	case c.TaskStore.RetentionDays < 0:
		return fmt.Errorf("task_store.retention_days must be >= 0: %d", c.TaskStore.RetentionDays)
	case c.Backup.BasePath == "":
		return errors.New("backup.base_path is empty")
	case c.TaskStore.Dir == "":
		return errors.New("task_store.dir is empty")
	}
	switch c.Locale {
	case "ko", "en", "ja":
	default:
		c.Locale = "ko" // ncc-xen 과 같다: 모르는 값은 ko
	}
	return nil
}

// resolveSecrets 는 키링을 읽고 토큰을 푼다. 환경 변수가 있으면 그 값을 쓴다.
func resolveSecrets(l *Loaded) error {
	if l.Crypto.Enabled || keyringExists(l.Crypto.KeyringFile) {
		p, err := secret.LoadFromFile(l.Crypto.KeyringFile)
		if err != nil {
			if l.Crypto.Enabled {
				return fmt.Errorf("keyring load failed (%s): %w", l.Crypto.KeyringFile, err)
			}
		} else {
			l.Protector = p
		}
	}
	var err error
	if l.APIToken, err = l.token(os.Getenv(EnvAPIToken), l.APIAuth.APIToken, PurposeAPIToken); err != nil {
		return fmt.Errorf("api_auth.VBR_VM_API_TOKEN: %w", err)
	}
	if l.GatewayToken, err = l.token(os.Getenv(EnvGatewayToken), l.APIAuth.GatewayToken, PurposeGatewayToken); err != nil {
		return fmt.Errorf("api_auth.GATEWAY_INTERNAL_TOKEN: %w", err)
	}
	if l.APIAuth.Enabled && l.APIToken == "" {
		return errors.New("api_auth.enabled is true but VBR_VM_API_TOKEN is empty")
	}
	return nil
}

func keyringExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// token 은 환경 변수가 있으면 그것을, 없으면 설정 값을 푼다. crypto.enabled 면 평문을 받지 않는다.
func (l *Loaded) token(env, value, purpose string) (string, error) {
	v := value
	if env != "" {
		v = env
	}
	return l.Reveal(v, purpose)
}

// Reveal 은 ENC 값이면 풀고, 평문이면 crypto.enabled=false 일 때만 그대로 돌려준다.
func (l *Loaded) Reveal(value, purpose string) (string, error) {
	if value == "" {
		return "", nil
	}
	if secret.HasEnvelopePrefix(value) {
		if l.Protector == nil {
			return "", errors.New("encrypted value but no keyring is loaded")
		}
		b, err := l.Protector.Decrypt(purpose, value)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	if l.Crypto.Enabled {
		return "", ErrPlaintext
	}
	return value, nil
}

// ErrPlaintext 는 crypto.enabled=true 인데 평문이 왔을 때다.
var ErrPlaintext = errors.New("plaintext secret is not allowed while crypto.enabled is true")

// Store 는 현재 설정과 변경 알림이다.
type Store struct {
	vp    *viper.Viper
	cur   atomic.Pointer[Loaded]
	mu    sync.Mutex
	timer *time.Timer
	subs  []func(old, cur *Loaded)
	logf  func(level, msg string, args ...any)
	now   func() time.Time
}

// Load 는 path 를 읽는다(path 가 비면 실행 파일 옆, 없으면 현재 디렉터리의 sysconfig.yaml).
func Load(path string, logf func(level, msg string, args ...any)) (*Store, []string, error) {
	if logf == nil {
		logf = func(string, string, ...any) {}
	}
	file, err := Locate(path)
	if err != nil {
		return nil, nil, err
	}
	vp := viper.New()
	vp.SetConfigFile(file)
	vp.SetConfigType("yaml")
	setDefaults(vp)
	if err := vp.ReadInConfig(); err != nil {
		return nil, nil, fmt.Errorf("config read failed (%s): %w", file, err)
	}
	s := &Store{vp: vp, logf: logf, now: time.Now}
	l, warns, err := parse(vp, s.now())
	if err != nil {
		return nil, warns, err
	}
	s.cur.Store(l)
	return s, warns, nil
}

// Locate 는 설정 파일 위치를 정한다(05 문서 10장).
func Locate(path string) (string, error) {
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("config file not found: %s", path)
		}
		return filepath.Abs(path)
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "sysconfig.yaml")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if _, err := os.Stat("sysconfig.yaml"); err == nil {
		return filepath.Abs("sysconfig.yaml")
	}
	return "", errors.New("sysconfig.yaml not found (next to the executable or in the current directory)")
}

// File 은 읽은 설정 파일 경로다.
func (s *Store) File() string { return s.vp.ConfigFileUsed() }

// Current 는 현재 설정이다. 작업은 시작할 때 한 번 받아 끝까지 쓴다.
func (s *Store) Current() *Loaded { return s.cur.Load() }

// Subscribe 는 설정이 바뀔 때 부를 함수를 더한다.
func (s *Store) Subscribe(fn func(old, cur *Loaded)) {
	s.mu.Lock()
	s.subs = append(s.subs, fn)
	s.mu.Unlock()
}

// Watch 는 파일 감시를 시작한다.
func (s *Store) Watch() {
	s.vp.OnConfigChange(func(fsnotify.Event) { s.schedule() })
	s.vp.WatchConfig()
}

func (s *Store) schedule() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(500*time.Millisecond, func() { _ = s.Reload() })
}

// Reload 는 파일을 다시 읽는다. 실패하면 이전 설정을 그대로 둔다.
func (s *Store) Reload() error {
	if err := s.vp.ReadInConfig(); err != nil {
		s.logf("error", "config reload failed, previous settings kept", "file", s.File(), "error", err)
		return err
	}
	l, warns, err := parse(s.vp, s.now())
	for _, w := range warns {
		s.logf("warn", w)
	}
	if err != nil {
		s.logf("error", "config reload failed, previous settings kept", "file", s.File(), "error", err)
		return err
	}
	old := s.cur.Swap(l)
	for _, k := range restartKeys(old, l) {
		s.logf("warn", "config key changed but needs restart to apply", "key", k)
	}
	s.logf("info", "config reloaded", "file", s.File())
	s.mu.Lock()
	subs := append([]func(old, cur *Loaded){}, s.subs...)
	s.mu.Unlock()
	for _, fn := range subs {
		fn(old, l)
	}
	return nil
}

// TestSwitchWarnings 는 시험용 스위치가 꺼진 것을 알리는 문장이다(기동 때와 바뀔 때 로그).
func TestSwitchWarnings(l *Loaded) []string {
	var w []string
	if !l.APIAuth.Enabled {
		w = append(w, "api_auth.enabled is false: API token check is OFF (test only)")
	}
	if !l.Crypto.Enabled {
		w = append(w, "crypto.enabled is false: plaintext passwords and tokens are accepted (test only)")
	}
	return w
}
