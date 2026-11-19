// vbr_vm 은 XenServer VM 백업·복구 JSON API 서비스다(NCC-VBR 2단계, docs/vm/).
// 별도 서버에서 XAPI 로 원격 실행한다. 앞에서 실행하며(DEC-VM-24) systemd 가 감시한다.
//
//	vbr_vm [-config <sysconfig.yaml>] [-version]
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ncc-vbr/internal/i18n"
	"ncc-vbr/internal/jobstore"
	logger "ncc-vbr/internal/stdlogger"
	"ncc-vbr/internal/vm"
	"ncc-vbr/internal/vm/api"
	"ncc-vbr/internal/vmconfig"
)

const (
	exitOK    = 0
	exitFatal = 1
	exitUsage = 255
)

// shutdownWait 는 종료 때 처리 중 작업의 정리를 기다리는 시간이다(03 설계 6장).
const shutdownWait = 30 * time.Second

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("vbr_vm", flag.ContinueOnError)
	configPath := fs.String("config", "", "sysconfig.yaml path")
	showVersion := fs.Bool("version", false, "print version")
	showVer := fs.Bool("ver", false, "print version")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "Usage : vbr_vm [-config <sysconfig.yaml>] [-version]")
		return exitUsage
	}
	if *showVersion || *showVer {
		fmt.Printf("vbr_vm %s\n", api.Version)
		return exitOK
	}

	file, err := vmconfig.Locate(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vbr_vm : %v\n", err)
		return exitFatal
	}
	// 로그(stdlogger 는 같은 파일을 따로 감시해 logging.* 을 즉시 반영한다)
	name := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	if err := logger.InitFromFile(logger.InitOption{ConfigName: name, ConfigType: "yaml",
		ConfigPaths: []string{filepath.Dir(file)}}); err != nil {
		fmt.Fprintf(os.Stderr, "vbr_vm : %v\n", err)
		return exitFatal
	}
	log := logger.With("component", "vbr_vm")

	cfgStore, warns, err := vmconfig.Load(file, logf)
	for _, w := range warns {
		log.Warn(i18n.T("vm.log.config_warn"), "detail", w)
	}
	if err != nil {
		log.Error(err.Error())
		fmt.Fprintf(os.Stderr, "vbr_vm : %v\n", err)
		return exitFatal
	}
	cfg := cfgStore.Current()
	i18n.SetLocale(cfg.Locale)
	warnTestSwitches(cfg)

	store, interrupted, err := jobstore.Open(cfg.TaskStore.Dir, retention(cfg), nil)
	if err != nil {
		log.Error("task store open failed", "dir", cfg.TaskStore.Dir, "error", err)
		fmt.Fprintf(os.Stderr, "vbr_vm : task store open failed: %v\n", err)
		return exitFatal
	}
	defer store.Close()
	outbox := jobstore.NewOutbox(cfg.TaskStore.Dir, func() jobstore.OutboxSettings { return outboxSettings(cfgStore.Current()) }, logf)
	svc := vm.New(cfgStore, store, outbox)
	if len(interrupted) > 0 {
		ids := make([]string, 0, len(interrupted))
		for _, r := range interrupted {
			ids = append(ids, r.TaskID)
		}
		log.Warn(i18n.T("vm.log.restart_recovery"), "count", len(interrupted), "taskIds", strings.Join(ids, ","))
		svc.ReportRecords(interrupted)
	}

	cfgStore.Subscribe(func(old, cur *vmconfig.Loaded) {
		i18n.SetLocale(cur.Locale)
		store.SetRetention(retention(cur))
		svc.AdjustWorkers()
		if old.APIAuth.Enabled != cur.APIAuth.Enabled || old.Crypto.Enabled != cur.Crypto.Enabled {
			warnTestSwitches(cur)
		}
	})
	cfgStore.Watch()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go outbox.Run(ctx)
	go compactLoop(ctx, store)
	svc.Start()

	addr := net.JoinHostPort(cfg.Server.Listen, strconv.Itoa(cfg.Server.Port))
	srv := &http.Server{Addr: addr, Handler: api.NewRouter(svc, cfgStore),
		ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("listen failed", "addr", addr, "error", err)
		fmt.Fprintf(os.Stderr, "vbr_vm : listen failed: %v\n", err)
		svc.Shutdown(shutdownWait)
		return exitFatal
	}
	log.Info(i18n.T("vm.log.service_start"), "version", api.Version, "config", file, "addr", addr,
		"maxJobs", cfg.Concurrency.MaxJobs, "taskStore", cfg.TaskStore.Dir, "locale", cfg.Locale)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	code := exitOK
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server failed", "error", err)
			code = exitFatal
		}
	}
	svc.Shutdown(shutdownWait)
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
	log.Info(i18n.T("vm.log.service_stop"))
	return code
}

func logf(level, msg string, args ...any) {
	e := logger.With("component", "vbr_vm")
	switch level {
	case "error":
		e.Error(msg, args...)
	case "warn":
		e.Warn(msg, args...)
	default:
		e.Info(msg, args...)
	}
}

func warnTestSwitches(cfg *vmconfig.Loaded) {
	for _, w := range vmconfig.TestSwitchWarnings(cfg) {
		logger.With("component", "vbr_vm").Warn(i18n.T("vm.log.test_switch"), "detail", w)
	}
}

func retention(cfg *vmconfig.Loaded) time.Duration {
	return time.Duration(cfg.TaskStore.RetentionDays) * 24 * time.Hour
}

func outboxSettings(cfg *vmconfig.Loaded) jobstore.OutboxSettings {
	return jobstore.OutboxSettings{
		URL:              cfg.ResultReport.URL,
		Token:            cfg.GatewayToken,
		MaxAge:           time.Duration(cfg.ResultReport.MaxAgeHours) * time.Hour,
		DeadLetterRetain: time.Duration(cfg.ResultReport.DeadLetterRetentionDays) * 24 * time.Hour,
	}
}

func compactLoop(ctx context.Context, store *jobstore.Store) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := store.Compact(); err != nil {
				logger.With("component", "vbr_vm").Error(i18n.T("vm.log.compact_failed"), "error", err)
			}
		}
	}
}
