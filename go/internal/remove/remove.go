// Package remove 는 보존 기간이 지난 이전 버전 파일을 지운다(원본 vbr_remove.c 1.000).
//
// 대상은 vbr_cifs 가 남긴 "<파일>-vbr-YYYYMMDDHHMMSS.zip" 뿐이다. 다른 파일은 건드리지 않는다.
// 기본은 LOG 모드로, 지우지 않고 지울 명령만 delete.log 에 적는다.
// 설정의 "Delete Mode" 가 "DELETE" 일 때만 실제로 지운다.
package remove

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"ncc-vbr/internal/config"
	"ncc-vbr/internal/walk"
)

const (
	// Version 은 원본 1.000 을 Go 로 옮긴 첫 판이다.
	Version = "2.000"
	// Title 은 제품명이자 node_info.cfg 섹션명이다(원본 PGM_NAME).
	Title = "File Remove (BATCH) for NCC-VBR"

	// MaxWorkers 는 원본 MAX_CIFS_THREAD 다.
	MaxWorkers = 50
	// MaxDirList 는 원본 MAX_2ND_DIR 다.
	MaxDirList = 10

	// DeleteLogFile 은 LOG 모드에서 지울 명령을 적는 파일이다(원본 DELETE_FILE).
	DeleteLogFile = "delete.log"

	versionMark   = "-vbr-"
	versionSuffix = ".zip"
	stampLayout   = "20060102150405"
	// tailLength 는 "-vbr-" + 14자리 시각 + ".zip" 의 길이다.
	tailLength = len(versionMark) + len(stampLayout) + len(versionSuffix)
)

// Logger 는 이 패키지가 쓰는 로그 창구다.
type Logger interface {
	Printf(format string, args ...any)
}

// Job 은 삭제 한 번의 실행 조건이다.
type Job struct {
	Dir    string // 검색할 디렉터리
	Days   int    // 보존 일수. 이보다 오래된 이전 버전을 지운다.
	Config config.Section
	Log    Logger
	Now    func() time.Time // 시험용. nil 이면 time.Now
}

// Summary 는 삭제 결과다.
type Summary struct {
	Files   uint64
	Dirs    uint64
	Deleted uint64
	Skipped uint64
	Errors  uint64
	Cutoff  string // 삭제 기준 시각(YYYYMMDDHHMMSS). 이 시각 이전 버전이 대상이다.
	Delete  bool   // 실제로 지웠는지(false 면 LOG 모드)
}

const maxRetentionDays = int((1<<63 - 1) / int64(24*time.Hour))

// ParseDays 는 보존 일수 인자가 날짜 계산 범위 안에 있는지 검사한다.
func ParseDays(text string) (int, error) {
	days, err := strconv.Atoi(text)
	if err != nil || days < 0 || days > maxRetentionDays {
		return 0, fmt.Errorf("days must be an integer between 0 and %d: [%s]", maxRetentionDays, text)
	}
	return days, nil
}

// Run 은 삭제를 한 번 수행한다.
func Run(ctx context.Context, job Job) (Summary, error) {
	if job.Days < 0 || job.Days > maxRetentionDays {
		return Summary{}, fmt.Errorf("days must be between 0 and %d", maxRetentionDays)
	}
	if job.Now == nil {
		job.Now = time.Now
	}
	log := job.Log
	now := job.Now()
	cutoff := now.Add(-time.Duration(job.Days) * 24 * time.Hour)
	summary := Summary{Cutoff: cutoff.Format(stampLayout), Delete: deleteMode(job)}

	log.Printf("%s ver %s start.\n", Title, Version)
	log.Printf("%s start.  delete dir = [%s], base(delete) days = [%d]\n", Title, job.Dir, job.Days)
	log.Printf("Current base date = %s\n", now.Format("2006-01-02 15:04:05"))
	log.Printf("Delete base date = %s\n", cutoff.Format("2006-01-02 15:04:05"))

	// 이전 실행의 delete.log 는 미리 지운다.
	_ = os.Remove(DeleteLogFile)

	remover := &remover{cutoff: summary.Cutoff, delete: summary.Delete, log: log}
	result, err := walk.Run(ctx, walkOptions(job), remover, func(report walk.TaskReport) {
		logTask(log, report)
	})
	summary.fill(result.Total)
	if err != nil {
		log.Printf("%s TASK register : Internal error.  STOP RUN!!! [%v]\n", Title, err)
		return summary, err
	}

	log.Printf("NCC-VBR batch finished.  Directory = [%s], delete base time = [%s]\n", job.Dir, summary.Cutoff)
	log.Printf("Total File = [%d], Total Dir = [%d], Delete files = [%d] Skip files = [%d], Error = [%d]\n",
		summary.Files, summary.Dirs, summary.Deleted, summary.Skipped, summary.Errors)
	return summary, nil
}

func (s *Summary) fill(total walk.Counters) {
	// 원본과 같이 알 수 없는 유형(심볼릭 링크 등)은 파일 수와 건너뛴 수에 함께 넣는다.
	s.Files = total.Files + total.Others
	s.Dirs = total.Dirs
	s.Deleted = total.Changed
	s.Skipped = total.Others
	s.Errors = total.Errors
}

func deleteMode(job Job) bool {
	const key = "Delete Mode"
	mode, found := job.Config.String(key)
	switch {
	case !found:
		job.Log.Printf("Config file [%s] parameter not found. LOG mode default setting.\n", key)
		return false
	case mode == "DELETE":
		job.Log.Printf("%s File DELETE mode.\n", Title)
		return true
	}
	job.Log.Printf("%s LOG mode.\n", Title)
	return false
}

func walkOptions(job Job) walk.Options {
	return walk.Options{
		Root:        job.Dir,
		Workers:     workerCount(job),
		SecondLevel: dirList(job, "2ND SEARCH DIR", "2nd search directory"),
		Exclude:     dirList(job, "EXCLUSION DIR", "exclusion directory"),
		Name:        Title,
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
		job.Log.Printf("%s Thread cnt is %d, MAX Thread limit is %d, change thread cnt = %d.\n",
			Title, count, MaxWorkers, MaxWorkers)
		return MaxWorkers
	case count < 1:
		job.Log.Printf("%s Thread cnt is %d, change thread cnt = 1.\n", Title, count)
		return 1
	}
	job.Log.Printf("%s Thread cnt = [%d]\n", Title, count)
	return int(count)
}

func dirList(job Job, key, label string) []string {
	list := job.Config.Fields(key)
	if len(list) == 0 {
		job.Log.Printf("%s is not defined.  File Delete mode is standard.\n", key)
		return nil
	}
	if len(list) > MaxDirList {
		job.Log.Printf("%s overflow. change to standard mode. [%d]\n", key, len(list))
		return nil
	}
	for i, dir := range list {
		job.Log.Printf("Special Delete policy (%s) [%d]th [%s] setup.\n", label, i, dir)
	}
	return list
}

func logTask(log Logger, report walk.TaskReport) {
	if report.Failed {
		log.Printf("%s thread [%d]th internal error: [%s]\n", Title, report.Worker, report.Path)
		return
	}
	count := report.Counters
	log.Printf(
		"%s thread [%d]th finished: [%s]\n"+
			"   Total File = [%d], Total Dir = [%d], Delete file = [%d], Skip file = [%d]\n",
		Title, report.Worker, report.Path, count.Files+count.Others, count.Dirs, count.Changed, count.Others)
}

// remover 는 순회 엔진이 부르는 삭제 동작이다(walk.Visitor).
type remover struct {
	cutoff string
	delete bool
	log    Logger
	mu     sync.Mutex // delete.log 쓰기 보호
}

// File 은 원본 file_delete 다. 보존 기간이 지난 이전 버전 파일이면 지운다.
func (r *remover) File(path, _ string) (bool, error) {
	if !IsExpiredVersion(path, r.cutoff) {
		return false, nil
	}
	if !r.delete {
		return true, r.appendDeleteLog(path)
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	r.log.Printf("File Name [%s] : Delete finished.\n", path)
	return true, nil
}

// EnterDir 은 할 일이 없다. 디렉터리는 지우지 않는다.
func (r *remover) EnterDir(_, _ string) (bool, error) {
	return false, nil
}

// TopDir 은 할 일이 없다.
func (r *remover) TopDir(string) {}

// IsExpiredVersion 은 경로가 "…-vbr-YYYYMMDDHHMMSS.zip" 이고 그 시각이 기준 시각 이하인지 알려준다.
// 원본과 달리 시각 자리가 숫자 14자리인지도 확인한다.
func IsExpiredVersion(path, cutoff string) bool {
	path = filepath.Base(path)
	if len(path) < tailLength+1 || !strings.HasSuffix(path, versionSuffix) {
		return false
	}
	tail := path[len(path)-tailLength:]
	if !strings.HasPrefix(tail, versionMark) {
		return false
	}
	stamp := tail[len(versionMark) : len(versionMark)+len(stampLayout)]
	if !isDigits(stamp) {
		return false
	}
	return stamp <= cutoff
}

func isDigits(text string) bool {
	for _, ch := range text {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// appendDeleteLog 는 원본 BS_Delete 다. 셸에서 그대로 실행할 수 있는 삭제 명령을 한 줄 적는다.
func (r *remover) appendDeleteLog(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	fp, err := os.OpenFile(DeleteLogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		return fmt.Errorf("delete log open: %w", err)
	}
	if _, err := fp.WriteString("rm -f -- " + shellQuote(path) + "\n"); err != nil {
		fp.Close()
		return fmt.Errorf("delete log write: %w", err)
	}
	return fp.Close()
}

// shellQuote 는 경로를 작은따옴표로 감싼다. 경로 안의 작은따옴표는 '\” 로 바꾼다.
func shellQuote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}
