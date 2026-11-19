// Package cifs 는 NAS(CIFS) 디렉터리 증분 백업이다(원본 vbr_cifs.c 2.0672).
//
// 한 번 실행하면 원본 디렉터리 전체를 순회하며 바뀐 파일만 대상 디렉터리로 복사한다.
// 대상에 있던 이전 내용은 "<대상파일>-vbr-YYYYMMDDHHMMSS.zip"(gzip 형식)으로 남긴다.
// 이 이전 버전 파일은 vbr_remove 가 보존 기간이 지나면 지운다.
package cifs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ncc-vbr/internal/config"
	"ncc-vbr/internal/fsx"
	"ncc-vbr/internal/portal"
	"ncc-vbr/internal/walk"
)

const (
	// Version 은 원본 2.0672 를 Go 로 옮긴 첫 판이다.
	Version = "3.0000"
	// Title 은 "-version" 출력에 쓰는 제품명이다.
	Title = "NCC-VBR CIFS Manager"
	// Section 은 node_info.cfg 섹션명이다.
	Section = "VBR Manager"

	// MaxWorkers 는 원본 MAX_CIFS_THREAD 다.
	MaxWorkers = 50
	// MaxDirList 는 원본 MAX_2ND_DIR 다(2단계·제외 목록 최대 개수).
	MaxDirList = 200

	historyDir  = "history"
	finishEvent = "CIFSFIN00"
	dirMode     = 0o755
)

// Logger 는 이 패키지가 쓰는 로그 창구다.
type Logger interface {
	Printf(format string, args ...any)
	Tran(format string, args ...any)
}

// Job 은 백업 한 번의 실행 조건이다.
type Job struct {
	Source string // 원본 디렉터리
	Target string // 대상 디렉터리
	Seq    string // 호출자가 준 작업 번호(완료 이벤트 메시지 앞 17자)
	Config config.Section
	Log    Logger
	Now    func() time.Time // 시험용. nil 이면 time.Now
}

// Summary 는 백업 결과다.
type Summary struct {
	Files        uint64
	Dirs         uint64
	Changed      uint64
	DirsCreated  uint64
	Errors       uint64
	BaseTime     int64 // 이번 실행이 쓴 변경 판단 기준 시각(직전 성공 실행의 시작 시각)
	StartTime    int64 // 이번 실행의 시작 시각
	HistorySaved bool  // 기준 시각을 이번 시작 시각으로 갱신했는지
}

// Run 은 백업을 한 번 수행한다.
func Run(ctx context.Context, job Job) (Summary, error) {
	if job.Now == nil {
		job.Now = time.Now
	}
	log := job.Log
	log.Printf("%s ver %s start.\n", Title, Version)
	log.Printf("VBR CIFS backup start.  source dir = [%s], target dir = [%s]\n", job.Source, job.Target)
	log.Tran("VBR CIFS backup start.   source dir = [%s], target dir = [%s]", job.Source, job.Target)

	if err := requireDir(job.Target); err != nil {
		log.Printf("Target directory [%s] is not valid.[%v]!!!\n", job.Target, err)
		log.Printf("VBR TASK Internal error.  STOP RUN!!!\n")
		return Summary{}, err
	}

	history := historyPath(job.Source, job.Target)
	start := job.Now()
	summary := Summary{BaseTime: readHistory(log, history), StartTime: start.Unix()}
	if fullScanEnabled(job, start) {
		summary.BaseTime = 0
	}
	log.Printf("Backup base time  = [%d]\n", summary.BaseTime)
	log.Printf("Backup start time = [%d]\n", summary.StartTime)
	log.Printf("BEGIN : %s\n", historyLine(start))

	copier := &copier{source: job.Source, target: job.Target, baseTime: summary.BaseTime, log: log, now: job.Now}
	result, err := walk.Run(ctx, walkOptions(job), copier, func(report walk.TaskReport) {
		logTask(log, job, report)
	})
	summary.fill(result.Total)
	if err != nil {
		log.Printf("VBR TASK Internal error.  STOP RUN!!! [%v]\n", err)
		return summary, err
	}
	log.Printf(
		"VBR CIFS main thread finished: [%s]=>[%s]\n"+
			"   Total File = [%d], Total Dir = [%d], Change file = [%d], Create dir = [%d]\n",
		job.Source, job.Target,
		result.Root.Files, result.Root.Dirs, result.Root.Changed, result.Root.DirsCreated)

	summary.HistorySaved = saveHistory(log, history, start, summary.Errors)
	finish(job, summary)
	return summary, nil
}

func fullScanEnabled(job Job, start time.Time) bool {
	const key = "Full Scan Day"
	value, found := job.Config.String(key)
	if !found {
		value = "0"
		job.Log.Printf("Config file[%s] [%s] parameter not found.(Sunday default setting)..\n",
			job.Config.Name(), key)
	}
	// 2.068 은 첫 글자에서 '0' 을 뺀 값을 요일과 비교만 한다. 0~6 이 아니면 어느 요일과도
	// 같지 않아 로그 없이 꺼진다. 전환본도 같은 로그만 남긴다.
	if len(value) == 0 || value[0] < '0' || value[0] > '6' {
		return false
	}
	if time.Weekday(value[0]-'0') != start.Weekday() {
		return false
	}
	job.Log.Printf("Full search mode enable.[%c]\n", value[0])
	return true
}

func (s *Summary) fill(total walk.Counters) {
	s.Files = total.Files
	s.Dirs = total.Dirs
	s.Changed = total.Changed
	s.DirsCreated = total.DirsCreated
	s.Errors = total.Errors
}

func requireDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}

func walkOptions(job Job) walk.Options {
	return walk.Options{
		Root:        job.Source,
		Workers:     workerCount(job),
		SecondLevel: dirList(job, "2ND SEARCH DIR", "2nd search directory"),
		Exclude:     dirList(job, "EXCLUSION DIR", "exclusion directory"),
		Name:        "VBR CIFS",
		Log:         job.Log,
	}
}

func workerCount(job Job) int {
	const key = "MAX Thread cnt"
	count, found := job.Config.Int64(key)
	switch {
	case !found:
		job.Log.Printf("Config file [%s] parameter not found.(%d default setting)..\n", key, MaxWorkers)
		return MaxWorkers
	case count > MaxWorkers:
		job.Log.Printf("Thread cnt is %d, MAX Thread limit is %d, change thread cnt = %d.\n",
			count, MaxWorkers, MaxWorkers)
		return MaxWorkers
	case count < 1:
		job.Log.Printf("Thread cnt is %d, change thread cnt = 1.\n", count)
		return 1
	}
	job.Log.Printf("CIFS Thread cnt = [%d]\n", count)
	return int(count)
}

func dirList(job Job, key, label string) []string {
	list := job.Config.Fields(key)
	if len(list) == 0 {
		job.Log.Printf("%s is not defined.  Backup mode is standard.\n", key)
		return nil
	}
	if len(list) > MaxDirList {
		job.Log.Printf("%s overflow. change to standard backup mode. [%d]\n", key, len(list))
		return nil
	}
	for i, dir := range list {
		job.Log.Printf("Special backup policy (%s) [%d]th [%s] setup.\n", label, i, dir)
	}
	return list
}

func logTask(log Logger, job Job, report walk.TaskReport) {
	target := filepath.Join(job.Target, report.Rel)
	if report.Failed {
		log.Printf("VBR CIFS thread [%d]th internal error: [%s]=>[%s]\n", report.Worker, report.Path, target)
		return
	}
	count := report.Counters
	log.Printf(
		"VBR CIFS thread [%d]th finished: [%s]=>[%s]\n"+
			"   Total File = [%d], Total Dir = [%d], Change file = [%d], Create dir = [%d]\n",
		report.Worker, report.Path, target, count.Files, count.Dirs, count.Changed, count.DirsCreated)
}

// historyPath 는 원본·대상 쌍마다 하나인 기준 시각 파일 경로다.
func historyPath(source, target string) string {
	name := fsx.FlattenPath(source) + "-" + fsx.FlattenPath(target) + ".vbr"
	return filepath.Join(historyDir, name)
}

func historyLine(at time.Time) string {
	return fmt.Sprintf("%d [%s]", at.Unix(), at.Format("2006-01-02 15:04:05"))
}

// readHistory 는 직전 성공 실행의 시작 시각을 읽는다. 파일이 없으면 0(전체 비교)이다.
func readHistory(log Logger, path string) int64 {
	log.Printf("history file search.[%s]\n", path)
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Backup history file [%s]is not found.\n", path)
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	base, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0
	}
	return base
}

// saveHistory 는 이번 시작 시각을 다음 실행의 기준 시각으로 남긴다.
// 오류가 한 건이라도 있으면 남기지 않는다. 그래야 실패한 파일을 다음 실행이 다시 확인한다.
func saveHistory(log Logger, path string, start time.Time, errorCount uint64) bool {
	if errorCount > 0 {
		log.Printf("Backup history file [%s] is not updated. error count = [%d]\n", path, errorCount)
		return false
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		log.Printf("Backup history file write [%s]open error.[%v]\n", path, err)
		return false
	}
	if err := os.WriteFile(path, []byte(historyLine(start)+"\n"), 0o666); err != nil {
		log.Printf("Backup history file write [%s]open error.[%v]\n", path, err)
		return false
	}
	log.Printf("END : %s\n", historyLine(start))
	return true
}

func finish(job Job, summary Summary) {
	log := job.Log
	log.Printf("VBR CIFS backup finished.  source dir = [%s], target dir = [%s]\n", job.Source, job.Target)
	log.Tran("VBR CIFS backup finished : source dir = [%s], target dir = [%s]", job.Source, job.Target)

	totals := fmt.Sprintf("Total File = [%d], Total Dir = [%d], Change file = [%d], Create dir = [%d]",
		summary.Files, summary.Dirs, summary.Changed, summary.DirsCreated)
	log.Printf("%s, Error = [%d]\n", totals, summary.Errors)
	log.Tran("%s, Error = [%d]", totals, summary.Errors)

	sendFinishEvent(job)
}

func sendFinishEvent(job Job) {
	client, ok := portalClient(job)
	if !ok {
		return
	}
	seq := job.Seq
	if len(seq) > 17 {
		seq = seq[:17]
	}
	message := fmt.Sprintf("%s BACKUP: %s => %s Finished.", seq, job.Source, job.Target)
	event := portal.Event{
		Type:     'N',
		HostName: "VBR CIFS",
		HostUUID: "NONE",
		Time:     job.Now(),
		Code:     finishEvent,
		Message:  message,
	}
	reply, err := client.Send(event)
	if err != nil {
		job.Log.Printf("Event [%s][%s] Send to VBR WEB Portal failed.[%v]\n", finishEvent, message, err)
		return
	}
	job.Log.Printf("Event [%s][%s] Send to VBR WEB Portal success.\n", finishEvent, message)
	job.Log.Printf("=====   RESULT   =====\n%s\n", reply)
}

func portalClient(job Job) (portal.Client, bool) {
	host, found := job.Config.String("WEB server(IP)")
	if !found || host == "" {
		job.Log.Printf("Warning. Config file [%s] parameter not found.\n", "WEB server(IP)")
		return portal.Client{}, false
	}
	port, found := job.Config.Int64("WEB server(PORT)")
	if !found {
		job.Log.Printf("Warning. Config file [%s] parameter not found.\n", "WEB server(PORT)")
		return portal.Client{}, false
	}
	job.Log.Printf("WEB Portal ip = %s, port = %d\n", host, port)
	return portal.Client{Host: host, Port: int(port)}, true
}
