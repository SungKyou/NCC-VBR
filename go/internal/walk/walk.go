// Package walk 는 vbr_cifs 와 vbr_remove 가 함께 쓰는 디렉터리 순회 엔진이다.
//
// 원본 두 프로그램은 같은 순회 구조(cifs_root → 메시지 큐 → cifs_processing/cifs_sub)를
// 각자 복사해 갖고 있었다. 이 패키지는 그 구조를 하나로 모으고, 파일·디렉터리마다 할 일만
// Visitor 로 받는다.
//
// 처리 순서(원본과 같다)
//  1. 주 작업자(0번)가 시작 디렉터리를 읽는다. 바로 아래 파일은 직접 처리하고,
//     바로 아래 디렉터리는 작업 목록에 올린다.
//  2. 제외 목록에 있는 디렉터리는 건너뛴다.
//  3. 2단계 목록에 있는 디렉터리는 주 작업자가 한 단계 더 내려가 그 아래 디렉터리를 작업 목록에 올린다.
//  4. 작업자 N개가 작업 목록의 디렉터리를 하나씩 가져가 끝까지 재귀 처리한다.
//
// 원본과 다른 점: System V 메시지 큐 대신 프로세스 안의 채널을 쓴다. 큐 용량(기본 16KB) 때문에
// 등록 단계에서 멈추는 일과, 같은 키를 쓰는 다른 프로세스와 작업이 섞이는 일이 없다.
package walk

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"ncc-vbr/internal/fsx"
)

// readBatch 는 디렉터리 항목을 한 번에 읽는 개수다. 항목이 매우 많은 디렉터리의 메모리 사용을 제한한다.
const readBatch = 1024

// Visitor 는 순회 중 만난 항목에 대해 할 일을 정한다.
// 같은 Visitor 가 여러 작업자에서 동시에 호출되므로 구현은 동시 호출에 안전해야 한다.
type Visitor interface {
	// File 은 일반 파일마다 호출된다. changed 는 "변경(복사·삭제) 처리함"을 뜻한다.
	File(path, rel string) (changed bool, err error)
	// EnterDir 은 작업자가 디렉터리 안으로 들어가기 직전에 호출된다.
	// created 는 "대상 디렉터리를 새로 만듦"을 뜻한다. err 이면 그 디렉터리는 처리하지 않는다.
	EnterDir(path, rel string) (created bool, err error)
	// TopDir 은 시작 디렉터리 바로 아래의 디렉터리마다, 작업 목록에 올리기 전에 호출된다.
	TopDir(name string)
}

// Logger 는 순회 엔진이 쓰는 로그 창구다.
type Logger interface {
	Printf(format string, args ...any)
}

// Counters 는 처리 건수다.
type Counters struct {
	Files       uint64 // 일반 파일 수
	Dirs        uint64 // 디렉터리 수
	Changed     uint64 // 변경 처리한 파일 수
	DirsCreated uint64 // 새로 만든 대상 디렉터리 수
	Others      uint64 // 일반 파일도 디렉터리도 아닌 항목 수(심볼릭 링크 등)
	Errors      uint64 // 오류 수
}

func (c *Counters) add(other Counters) {
	c.Files += other.Files
	c.Dirs += other.Dirs
	c.Changed += other.Changed
	c.DirsCreated += other.DirsCreated
	c.Others += other.Others
	c.Errors += other.Errors
}

// Options 는 순회 조건이다.
type Options struct {
	Root        string   // 시작 디렉터리
	Workers     int      // 작업자 수(1 이상)
	SecondLevel []string // 2단계까지 내려가 나눠 처리할 디렉터리(전체 경로)
	Exclude     []string // 처리하지 않을 디렉터리(전체 경로)
	Name        string   // 로그에 쓰는 프로그램 이름
	Log         Logger
}

// TaskReport 는 작업 하나(디렉터리 하나)가 끝날 때 넘겨주는 결과다.
type TaskReport struct {
	Worker   int
	Path     string
	Rel      string
	Counters Counters
	Failed   bool
}

// Result 는 전체 순회 결과다.
type Result struct {
	Root  Counters // 주 작업자(0번)가 처리한 건수
	Total Counters // 전체 합계
	Tasks int      // 작업 목록에 올린 디렉터리 수
}

type task struct {
	path string
	rel  string
}

type engine struct {
	ctx     context.Context
	opt     Options
	visitor Visitor
	tasks   []task
	onTask  func(TaskReport)
}

// Run 은 순회를 수행한다. 시작 디렉터리를 열 수 없으면 오류를 돌려준다.
// onTask 는 작업 하나가 끝날 때마다 호출된다(nil 가능).
func Run(ctx context.Context, opt Options, visitor Visitor, onTask func(TaskReport)) (Result, error) {
	if opt.Workers < 1 {
		opt.Workers = 1
	}
	e := &engine{ctx: ctx, opt: opt, visitor: visitor, onTask: onTask}

	var result Result
	if err := e.scanRoot(opt.Root, "", true, &result.Root); err != nil {
		return result, err
	}
	result.Tasks = len(e.tasks)
	result.Total = result.Root
	result.Total.add(e.runWorkers())
	return result, ctx.Err()
}

// scanRoot 는 원본 cifs_root 다. 주 작업자가 한 디렉터리를 읽어 파일은 처리하고 디렉터리는 나눈다.
func (e *engine) scanRoot(dir, rel string, top bool, count *Counters) error {
	return e.readDir(dir, count, func(name string, info fs.FileInfo) {
		path := filepath.Join(dir, name)
		childRel := filepath.Join(rel, name)
		switch {
		case info.Mode().IsRegular():
			e.visitFile(path, childRel, count)
		case info.IsDir():
			count.Dirs++
			e.dispatchDir(path, childRel, name, top, count)
		default:
			count.Others++
			e.opt.Log.Printf("Contents [%s] Unknown type.\n", path)
		}
	})
}

func (e *engine) dispatchDir(path, rel, name string, top bool, count *Counters) {
	if matchAny(path, e.opt.Exclude) {
		e.opt.Log.Printf("Exclusion directory found. SKIP directory. [%s]\n", path)
		return
	}
	if top {
		e.visitor.TopDir(name)
	}
	if matchAny(path, e.opt.SecondLevel) {
		e.scanSecondLevel(path, rel, count)
		return
	}
	e.tasks = append(e.tasks, task{path: path, rel: rel})
	e.opt.Log.Printf("%s TASK registered=>[%s]\n", e.opt.Name, path)
}

func (e *engine) scanSecondLevel(path, rel string, count *Counters) {
	created, err := e.visitor.EnterDir(path, rel)
	if err != nil {
		count.Errors++
		e.opt.Log.Printf("%s directory prepare error.[%s][%v]\n", e.opt.Name, path, err)
		return
	}
	if created {
		count.DirsCreated++
	}
	if err := e.scanRoot(path, rel, false, count); err != nil {
		count.Errors++
	}
}

func (e *engine) runWorkers() Counters {
	queue := make(chan task, len(e.tasks))
	for _, t := range e.tasks {
		queue <- t
	}
	close(queue)

	totals := make([]Counters, e.opt.Workers+1)
	var wait sync.WaitGroup
	for seq := 1; seq <= e.opt.Workers; seq++ {
		wait.Add(1)
		go func(seq int) {
			defer wait.Done()
			e.work(seq, queue, &totals[seq])
		}(seq)
	}
	wait.Wait()

	var sum Counters
	for _, c := range totals {
		sum.add(c)
	}
	return sum
}

// work 는 원본 cifs_processing 이다. 작업 목록이 빌 때까지 디렉터리를 하나씩 처리한다.
func (e *engine) work(seq int, queue <-chan task, total *Counters) {
	for t := range queue {
		if e.ctx.Err() != nil {
			break
		}
		e.opt.Log.Printf("%s thread [%d]th job start: [%s]\n", e.opt.Name, seq, t.path)
		var count Counters
		failed := !e.walkDir(t.path, t.rel, &count)
		total.add(count)
		if e.onTask != nil {
			e.onTask(TaskReport{Worker: seq, Path: t.path, Rel: t.rel, Counters: count, Failed: failed})
		}
	}
	e.opt.Log.Printf("%s thread [%d]th finished.\n", e.opt.Name, seq)
}

// walkDir 는 원본 cifs_sub 다. 한 디렉터리를 끝까지 재귀 처리한다.
func (e *engine) walkDir(dir, rel string, count *Counters) bool {
	created, err := e.visitor.EnterDir(dir, rel)
	if err != nil {
		count.Errors++
		e.opt.Log.Printf("%s directory prepare error.[%s][%v]\n", e.opt.Name, dir, err)
		return false
	}
	if created {
		count.DirsCreated++
	}

	err = e.readDir(dir, count, func(name string, info fs.FileInfo) {
		path := filepath.Join(dir, name)
		childRel := filepath.Join(rel, name)
		switch {
		case info.Mode().IsRegular():
			e.visitFile(path, childRel, count)
		case info.IsDir():
			count.Dirs++
			e.walkDir(path, childRel, count)
		default:
			count.Others++
			e.opt.Log.Printf("[DEBUG] Contents [%s] Unknown type.\n", path)
		}
	})
	return err == nil
}

func (e *engine) visitFile(path, rel string, count *Counters) {
	count.Files++
	changed, err := e.visitor.File(path, rel)
	if err != nil {
		count.Errors++
		e.opt.Log.Printf("%s file processing error.[%s][%v]\n", e.opt.Name, path, err)
		return
	}
	if changed {
		count.Changed++
	}
}

// readDir 은 디렉터리 항목을 나눠 읽으며 each 를 호출한다. ".", ".." 과 스냅샷 디렉터리는 건너뛴다.
func (e *engine) readDir(dir string, count *Counters, each func(name string, info fs.FileInfo)) error {
	fp, err := os.Open(dir)
	if err != nil {
		count.Errors++
		e.opt.Log.Printf("Directory [%s] is not valid.[%v]!!!\n", dir, err)
		return err
	}
	defer fp.Close()

	for {
		entries, err := fp.ReadDir(readBatch)
		for _, entry := range entries {
			if e.ctx.Err() != nil {
				return e.ctx.Err()
			}
			e.visitEntry(dir, entry, count, each)
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			count.Errors++
			e.opt.Log.Printf("Directory [%s] read error.[%v]!!!\n", dir, err)
			return err
		}
	}
}

func (e *engine) visitEntry(dir string, entry fs.DirEntry, count *Counters, each func(string, fs.FileInfo)) {
	name := entry.Name()
	if IsSnapshotName(name) {
		e.opt.Log.Printf("snapshot skip.\n")
		return
	}
	info, err := entry.Info()
	if err != nil {
		count.Errors++
		e.opt.Log.Printf("lstat internal error[%s][%v]!!!\n", filepath.Join(dir, name), err)
		return
	}
	each(name, info)
}

// IsSnapshotName 은 NAS 스냅샷 디렉터리 이름인지 알려준다.
// 원본 규칙: 첫 글자를 뺀 나머지가 "snapshot" 이다(".snapshot", "~snapshot" 등).
func IsSnapshotName(name string) bool {
	return len(name) > 1 && name[1:] == "snapshot"
}

func matchAny(path string, list []string) bool {
	for _, item := range list {
		if fsx.SamePath(path, item) {
			return true
		}
	}
	return false
}
