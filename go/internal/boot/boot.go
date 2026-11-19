// Package boot 는 배치형 서비스(vbr_cifs, vbr_remove)의 공통 기동 절차다.
//
// 절차: 명령행 해석 → 작업 디렉터리 이동 → 백그라운드 분리 → 로그·설정 준비 → 종료 신호 연결.
// 새 서비스를 전환할 때 main 은 이 패키지의 Start 를 부르고 업무 패키지의 Run 만 연결하면 된다.
package boot

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"ncc-vbr/internal/config"
	"ncc-vbr/internal/daemon"
	"ncc-vbr/internal/logx"
)

// 종료 코드. 원본은 정상 0, 그 외 -1(255)만 썼다.
const (
	ExitOK         = 0 // 정상 종료
	ExitFatal      = 1 // 시작 실패 또는 중단
	ExitWithErrors = 2 // 끝까지 처리했으나 일부 항목에서 오류
	ExitUsage      = 255
)

// Spec 은 서비스별로 다른 기동 정보다.
type Spec struct {
	Title    string // "-version" 출력에 쓰는 제품명
	Version  string
	Usage    string // 위치 인자 설명(예: "source-dir target-dir seq-no")
	ArgCount int    // 위치 인자 개수
	Section  string // node_info.cfg 섹션명
	// Check 는 위치 인자를 검사한다(nil 가능). 백그라운드로 분리하기 전에 불러
	// 잘못된 인자를 호출자가 바로 알 수 있게 한다.
	Check func(args []string) error
}

// App 은 기동이 끝난 실행 환경이다.
type App struct {
	Args    []string // 위치 인자
	Log     *logx.Logger
	Config  config.Section
	Context context.Context
	stop    context.CancelFunc
}

// Close 는 종료 신호 연결을 푼다.
func (a *App) Close() {
	a.stop()
}

// Start 는 공통 기동 절차를 수행한다. 사용법 오류나 버전 출력이면 여기서 프로세스를 끝낸다.
func Start(spec Spec) *App {
	opts := parse(spec)

	if opts.home != "" {
		if err := os.Chdir(opts.home); err != nil {
			fmt.Printf("FATAL Error : home directory [%s] change failed. %v\n", opts.home, err)
			os.Exit(ExitFatal)
		}
	}

	log := logx.New(".")
	if !opts.foreground {
		if err := daemon.Detach(); err != nil {
			log.Printf("FATAL Error : %s STOP RUN(Daemonized fail). %v\n", spec.Title, err)
			fmt.Printf("FATAL Error : %s STOP RUN(Daemonized fail).\n", spec.Title)
			os.Exit(ExitFatal)
		}
	}

	section := loadSection(log, opts.configFile, spec.Section)
	applyMaxLogSize(log, section)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	return &App{Args: opts.args, Log: log, Config: section, Context: ctx, stop: stop}
}

type options struct {
	home       string
	configFile string
	foreground bool
	args       []string
}

func parse(spec Spec) options {
	var opts options
	var version bool

	flags := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&version, "version", false, "print version")
	flags.BoolVar(&version, "ver", false, "print version")
	flags.BoolVar(&opts.foreground, "foreground", false, "run in foreground")
	flags.BoolVar(&opts.foreground, "fg", false, "run in foreground")
	flags.StringVar(&opts.home, "home", "", "working directory")
	flags.StringVar(&opts.configFile, "config", config.DefaultFile, "config file")

	err := flags.Parse(os.Args[1:])
	if version {
		fmt.Printf("%s version = %s\n", spec.Title, spec.Version)
		os.Exit(ExitOK)
	}
	if err != nil || flags.NArg() != spec.ArgCount {
		usage(spec)
	}
	opts.args = flags.Args()
	if spec.Check != nil {
		if err := spec.Check(opts.args); err != nil {
			fmt.Printf("%s : %v\n", spec.Title, err)
			usage(spec)
		}
	}
	return opts
}

func usage(spec Spec) {
	fmt.Printf("Usage : %s [-home dir] [-config file] [-foreground] %s\n", os.Args[0], spec.Usage)
	logx.New(".").Printf("Usage : %s %s\n", os.Args[0], spec.Usage)
	os.Exit(ExitUsage)
}

func loadSection(log *logx.Logger, path, name string) config.Section {
	file, err := config.Load(path)
	if err != nil {
		log.Printf("FATAL ERROR : %v\n", err)
		return config.Section{}
	}
	return file.Section(name)
}

func applyMaxLogSize(log *logx.Logger, section config.Section) {
	const key = "MAX Log size"
	size, found := section.Int64(key)
	if !found {
		log.Printf("Config file [%s] parameter not found.(%d default setting)..\n", key, logx.DefaultMaxSize)
		return
	}
	log.SetMaxSize(size)
	log.Printf("Log File Max size is [%d]\n", size)
}
