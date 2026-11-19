// copied from ncc-xen stdlogger/logger/init.go @043c6d5 (CP-VM-11), 2026-11-19. 변경: 이 머리 주석과 gofmt 정렬(공백·줄바꿈)뿐, 코드는 그대로.
package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/viper"
)

// InitOption은 InitFromFile의 동작을 조정합니다.
type InitOption struct {
	// ConfigName: 설정 파일 이름 (확장자 제외). 기본값 "sysconfig"
	ConfigName string
	// ConfigType: 설정 파일 형식. 기본값 "yaml"
	ConfigType string
	// ConfigPaths: 설정 파일 탐색 경로 목록. 기본값 ["."]
	ConfigPaths []string
	// HotReload: 설정 파일 변경 감지 여부.
	// nil(생략)이면 기본값 true가 적용됩니다.
	// false로 명시할 때만 꺼집니다:
	//   f := false
	//   logger.MustInitFromFile(logger.InitOption{HotReload: &f})
	HotReload *bool
	// FileNameSuffix: 🆕 2026-10-28 — 설정의 logging.file_name 확장자 앞에 붙일 접미사.
	// 같은 설정 파일을 쓰는 여러 프로세스가 로그 파일을 나눠 쓰기 위한 옵션
	// (예: "./logs/ncc-xen.log" + "-analyzer" → "./logs/ncc-xen-analyzer.log").
	// 비어 있으면 기존과 동일. 핫리로드 중 file_name을 바꾸면 기동 시 계산한 경로가 유지된다.
	FileNameSuffix string
}

// hotReloadEnabled는 HotReload 포인터에서 실제 bool 값을 반환합니다.
// nil(생략)이면 기본값 true를 반환합니다.
func (o InitOption) hotReloadEnabled() bool {
	if o.HotReload == nil {
		return true // 기본값: hot reload 켜짐
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

// stdWatcher는 전역 Watcher 인스턴스입니다.
// GC 수거를 막고 hot reload가 지속 동작하도록 패키지 수준에서 보관합니다.
var stdWatcher *Watcher

// initOnce는 InitFromFile이 한 번만 실행되도록 보장합니다.
// 중복 호출 시 두 번째 호출부터는 조용히 무시됩니다.
var initOnce sync.Once

// InitFromFile은 설정 파일에서 Logger를 초기화합니다.
//
// 서비스 main.go의 최상단에서 한 번만 호출합니다.
// 두 번 이상 호출해도 첫 번째 호출만 적용됩니다.
// 이후 모든 패키지에서 logger.Info() 등을 바로 사용할 수 있습니다.
//
// 기본 사용 (sysconfig.yaml, 현재 디렉터리, hot reload 켜짐):
//
//	logger.InitFromFile()
//
// 옵션 지정 — HotReload를 생략하면 기본값 true가 적용됩니다:
//
//	logger.InitFromFile(logger.InitOption{
//	    ConfigName: "config",
//	})
//
// hot reload를 끄려면 명시적으로 false 포인터를 전달합니다:
//
//	f := false
//	logger.InitFromFile(logger.InitOption{HotReload: &f})
func InitFromFile(opts ...InitOption) error {
	var initErr error
	initOnce.Do(func() {
		initErr = doInitFromFile(opts...)
	})
	return initErr
}

func doInitFromFile(opts ...InitOption) error {
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
		// HotReload: nil이면 기본값(true) 유지, 포인터로 전달된 값만 적용
		if o.HotReload != nil {
			opt.HotReload = o.HotReload
		}
		opt.FileNameSuffix = o.FileNameSuffix
	}

	vp := viper.New()
	vp.SetConfigName(opt.ConfigName)
	vp.SetConfigType(opt.ConfigType)
	for _, p := range opt.ConfigPaths {
		vp.AddConfigPath(p)
	}

	if err := vp.ReadInConfig(); err != nil {
		return fmt.Errorf("logger: config read failed (%s.%s): %w",
			opt.ConfigName, opt.ConfigType, err)
	}

	// 🆕 2026-10-28 — FileNameSuffix: viper.Set은 파일 재읽기(핫리로드)보다 우선하므로 계속 유지된다.
	if opt.FileNameSuffix != "" {
		vp.Set("logging.file_name", suffixedFileName(vp.GetString("logging.file_name"), opt.FileNameSuffix))
	}

	// Watcher를 패키지 전역 변수에 보관 — GC 수거 방지 및 hot reload 지속 보장
	stdWatcher = NewWatcher(vp, Std())

	if err := stdWatcher.LoadAndApply(); err != nil {
		return fmt.Errorf("logger: init failed: %w", err)
	}

	if opt.hotReloadEnabled() {
		Std().Logger().Debug("hot reload 활성화",
			"config_file", vp.ConfigFileUsed(),
		)
		stdWatcher.Watch()
	}

	return nil
}

// suffixedFileName은 로그 파일 경로의 확장자 앞에 suffix를 붙입니다(🆕 2026-10-28).
// fn이 비어 있으면 기본 경로(DefaultLoggingConfig)를 기준으로 합니다.
func suffixedFileName(fn, suffix string) string {
	if fn == "" {
		fn = DefaultLoggingConfig().FileName
	}
	ext := filepath.Ext(fn)
	return strings.TrimSuffix(fn, ext) + suffix + ext
}

// MustInitFromFile은 InitFromFile과 동일하지만 오류 시 즉시 종료합니다.
//
//	func main() {
//	    logger.MustInitFromFile(logger.InitOption{ConfigName: "config"})
//	    logger.Info("service started")
//	}
func MustInitFromFile(opts ...InitOption) {
	if err := InitFromFile(opts...); err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] %v\n", err)
		os.Exit(1)
	}
}

// InitFromDir은 특정 디렉터리의 설정 파일로 초기화합니다.
//
//	logger.InitFromDir("/etc/ncc-xen")
func InitFromDir(dir string, opts ...InitOption) error {
	opt := defaultOption()
	if len(opts) > 0 {
		opt = opts[0]
	}
	opt.ConfigPaths = []string{dir}
	return InitFromFile(opt)
}
