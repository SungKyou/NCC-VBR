package cifs

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"ncc-vbr/internal/config"
	"ncc-vbr/internal/testkit"
)

// env 는 시험 한 건의 작업 공간이다. home 아래에 src, tgt, history 가 생긴다.
type env struct {
	t      *testing.T
	home   string
	source string
	target string
	log    *testkit.Log
	clock  time.Time
	cfg    string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	home := t.TempDir()
	e := &env{
		t:      t,
		home:   home,
		source: filepath.Join(home, "src"),
		target: filepath.Join(home, "tgt"),
		log:    &testkit.Log{},
		clock:  time.Now().Add(time.Hour).Truncate(time.Second),
		cfg:    "[VBR Manager]\nMAX Thread cnt = 3\n",
	}
	for _, dir := range []string{e.source, e.target} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	testkit.Chdir(t, home)
	return e
}

func (e *env) src(rel string) string { return filepath.Join(e.source, filepath.FromSlash(rel)) }
func (e *env) tgt(rel string) string { return filepath.Join(e.target, filepath.FromSlash(rel)) }

// run 은 시계를 1분 앞으로 돌린 뒤 백업을 한 번 수행한다.
func (e *env) run() (Summary, error) {
	e.t.Helper()
	e.clock = e.clock.Add(time.Minute)
	path := filepath.Join(e.home, "node_info.cfg")
	testkit.WriteFile(e.t, path, e.cfg)
	file, err := config.Load(path)
	if err != nil {
		e.t.Fatal(err)
	}
	return Run(context.Background(), Job{
		Source: e.source,
		Target: e.target,
		Seq:    "20261117090503001",
		Config: file.Section(Section),
		Log:    e.log,
		Now:    func() time.Time { return e.clock },
	})
}

func (e *env) mustRun() Summary {
	e.t.Helper()
	summary, err := e.run()
	if err != nil {
		e.t.Fatal(err)
	}
	return summary
}

func gunzip(t *testing.T, path string) string {
	t.Helper()
	fp, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fp.Close()
	reader, err := gzip.NewReader(fp)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFirstRunCopiesEverything(t *testing.T) {
	e := newEnv(t)
	testkit.WriteFile(t, e.src("root.txt"), "root")
	testkit.WriteFile(t, e.src("a/one.txt"), "one")
	testkit.WriteFile(t, e.src("a/deep/two.txt"), "two")
	testkit.WriteFile(t, e.src("b/with space/three.txt"), "three")
	testkit.WriteFile(t, e.src(".snapshot/hidden.txt"), "hidden")

	summary := e.mustRun()

	if summary.Files != 4 || summary.Changed != 4 {
		t.Errorf("files = %d, changed = %d, want 4, 4", summary.Files, summary.Changed)
	}
	if summary.Dirs != 4 || summary.DirsCreated != 4 {
		t.Errorf("dirs = %d, created = %d, want 4, 4", summary.Dirs, summary.DirsCreated)
	}
	if got := testkit.ReadFile(t, e.tgt("b/with space/three.txt")); got != "three" {
		t.Errorf("copied content = %q", got)
	}
	if testkit.Exists(e.tgt(".snapshot")) {
		t.Error("snapshot directory must be skipped")
	}
	if !summary.HistorySaved {
		t.Error("history must be saved")
	}
}

func TestSecondRunCopiesNothing(t *testing.T) {
	e := newEnv(t)
	e.cfg += "Full Scan Day = " + strconv.Itoa(int(e.clock.AddDate(0, 0, -1).Weekday())) + "\n"
	testkit.WriteFile(t, e.src("a/one.txt"), "one")
	e.mustRun()

	summary := e.mustRun()

	if summary.Files != 1 || summary.Changed != 0 || summary.DirsCreated != 0 {
		t.Errorf("summary = %+v", summary)
	}
	if summary.BaseTime == 0 {
		t.Error("base time must come from history file")
	}
}

func TestChangedFileKeepsOldVersion(t *testing.T) {
	e := newEnv(t)
	testkit.WriteFile(t, e.src("a/one.txt"), "old content")
	e.mustRun()

	testkit.WriteFile(t, e.src("a/one.txt"), "new content!!")
	testkit.Touch(t, e.src("a/one.txt"), e.clock.Add(30*time.Second))
	summary := e.mustRun()

	if summary.Changed != 1 {
		t.Fatalf("changed = %d, want 1", summary.Changed)
	}
	if got := testkit.ReadFile(t, e.tgt("a/one.txt")); got != "new content!!" {
		t.Errorf("target content = %q", got)
	}
	version := e.tgt("a/one.txt") + "-vbr-" + e.clock.Format("20060102150405") + ".zip"
	if got := gunzip(t, version); got != "old content" {
		t.Errorf("old version content = %q", got)
	}
}

func TestMissingTargetFileIsCopiedAgain(t *testing.T) {
	e := newEnv(t)
	testkit.WriteFile(t, e.src("a/one.txt"), "one")
	e.mustRun()
	if err := os.Remove(e.tgt("a/one.txt")); err != nil {
		t.Fatal(err)
	}

	summary := e.mustRun()

	if summary.Changed != 1 || !testkit.Exists(e.tgt("a/one.txt")) {
		t.Errorf("file must be copied again: %+v", summary)
	}
}

// 원본 규칙: 직전 실행 이후 원본 파일에 변화가 없으면 대상과 크기가 달라도 비교하지 않는다.
func TestUntouchedSourceIsNotCompared(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("depends on Linux atime/ctime")
	}
	e := newEnv(t)
	e.clock = time.Now().Add(-2 * time.Minute).Truncate(time.Second)
	e.cfg += "Full Scan Day = " + strconv.Itoa(int(e.clock.AddDate(0, 0, -1).Weekday())) + "\n"
	testkit.WriteFile(t, e.src("a/one.txt"), "one")
	past := time.Now().Add(-24 * time.Hour)
	testkit.Touch(t, e.src("a/one.txt"), past)
	e.mustRun()
	// 첫 실행의 시작 시각이 원본의 ctime 이후가 되도록 기준 시각 파일을 직접 고친다.
	history := historyPath(e.source, e.target)
	testkit.WriteFile(t, history, historyLine(time.Now().Add(time.Minute))+"\n")
	testkit.WriteFile(t, e.tgt("a/one.txt"), "damaged target")

	summary := e.mustRun()

	if summary.Changed != 0 {
		t.Errorf("changed = %d, want 0", summary.Changed)
	}
}

func TestFullScanDayComparesFilesOlderThanHistory(t *testing.T) {
	sunday := time.Date(2040, time.January, 1, 0, 30, 0, 0, time.FixedZone("KST", 9*60*60))
	cases := []struct {
		name     string
		start    time.Time
		config   string
		fullScan bool
	}{
		{name: "configured Tuesday", start: sunday.AddDate(0, 0, 2), config: "Full Scan Day = 2\n", fullScan: true},
		{name: "another weekday", start: sunday.AddDate(0, 0, 1), config: "Full Scan Day = 2\n"},
		{name: "default Sunday", start: sunday, fullScan: true},
		{name: "invalid setting", start: sunday, config: "Full Scan Day = 7\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.clock = tc.start.Add(-time.Minute)
			e.cfg += tc.config
			const file = "doc.txt"
			testkit.WriteFile(t, e.src(file), "original file")
			testkit.Touch(t, e.src(file), tc.start.AddDate(0, 0, -2))
			testkit.WriteFile(t, e.tgt(file), "old")
			history := historyPath(e.source, e.target)
			prior := tc.start.AddDate(0, 0, -1)
			testkit.WriteFile(t, history, historyLine(prior)+"\n")

			wantBase, wantChanged, wantContent := prior.Unix(), uint64(0), "old"
			if tc.fullScan {
				wantBase, wantChanged, wantContent = 0, 1, "original file"
			}
			summary := e.mustRun()
			if summary.BaseTime != wantBase || summary.Changed != wantChanged || summary.Errors != 0 {
				t.Fatalf("unexpected summary: %+v", summary)
			}
			if got := testkit.ReadFile(t, e.tgt(file)); got != wantContent {
				t.Errorf("target content = %q, want %q", got, wantContent)
			}
			if !summary.HistorySaved || readHistory(e.log, history) != tc.start.Unix() {
				t.Error("successful run must save its real start time")
			}
		})
	}
}

func TestExclusionAndSecondLevel(t *testing.T) {
	e := newEnv(t)
	testkit.WriteFile(t, e.src("skip/x.txt"), "x")
	testkit.WriteFile(t, e.src("big/direct.txt"), "direct")
	testkit.WriteFile(t, e.src("big/p1/1.txt"), "1")
	testkit.WriteFile(t, e.src("big/p2/2.txt"), "2")
	testkit.WriteFile(t, e.src("normal/n.txt"), "n")
	e.cfg = "[VBR Manager]\nMAX Thread cnt = 2\n" +
		"2ND SEARCH DIR = " + e.src("big") + "\n" +
		"EXCLUSION DIR = " + e.src("skip") + "\n"

	summary := e.mustRun()

	if testkit.Exists(e.tgt("skip")) {
		t.Error("excluded directory must not be copied")
	}
	for _, rel := range []string{"big/direct.txt", "big/p1/1.txt", "big/p2/2.txt", "normal/n.txt"} {
		if !testkit.Exists(e.tgt(rel)) {
			t.Errorf("%s must be copied", rel)
		}
	}
	if summary.Files != 4 || summary.Changed != 4 {
		t.Errorf("summary = %+v", summary)
	}
	for _, task := range []string{e.src("big/p1"), e.src("big/p2"), e.src("normal")} {
		if !e.log.Contains("TASK registered=>[" + task + "]") {
			t.Errorf("task %s must be registered", task)
		}
	}
	if e.log.Contains("TASK registered=>[" + e.src("big") + "]") {
		t.Error("second level directory itself must not be a task")
	}
}

func TestDisabledFolderRenamesTarget(t *testing.T) {
	e := newEnv(t)
	testkit.WriteFile(t, e.src("team/doc.txt"), "doc")
	e.mustRun()
	if err := os.Rename(e.src("team"), e.src("dis_team")); err != nil {
		t.Fatal(err)
	}

	summary := e.mustRun()

	if testkit.Exists(e.tgt("team")) || !testkit.Exists(e.tgt("dis_team/doc.txt")) {
		t.Error("target folder must be renamed to dis_team")
	}
	if summary.Changed != 0 || summary.DirsCreated != 0 {
		t.Errorf("nothing must be copied again: %+v", summary)
	}
}

func TestDisabledFolderKeepsExistingData(t *testing.T) {
	e := newEnv(t)
	testkit.WriteFile(t, e.src("dis_team/doc.txt"), "doc")
	testkit.WriteFile(t, e.tgt("team/old.txt"), "old")
	testkit.WriteFile(t, e.tgt("dis_team/kept.txt"), "kept")

	e.mustRun()

	if !testkit.Exists(e.tgt("team/old.txt")) || !testkit.Exists(e.tgt("dis_team/kept.txt")) {
		t.Error("existing target data must be kept")
	}
	if !e.log.Contains("already contains data. Rename skip.") {
		t.Error("skip reason must be logged")
	}
}

func TestDisabledFolderPreservesActiveSourceBackup(t *testing.T) {
	e := newEnv(t)
	testkit.WriteFile(t, e.src("team/live.txt"), "live")
	testkit.WriteFile(t, e.src("dis_team/disabled.txt"), "disabled")
	testkit.WriteFile(t, e.tgt("team/history.txt"), "preserve")

	e.mustRun()

	if got := testkit.ReadFile(t, e.tgt("team/history.txt")); got != "preserve" {
		t.Fatalf("active target history changed: %q", got)
	}
	if testkit.Exists(e.tgt("dis_team/history.txt")) {
		t.Error("active backup was moved into the disabled folder")
	}
	if got := testkit.ReadFile(t, e.tgt("dis_team/disabled.txt")); got != "disabled" {
		t.Fatalf("disabled source was not copied: %q", got)
	}
}

func TestErrorKeepsHistoryUnchanged(t *testing.T) {
	e := newEnv(t)
	testkit.WriteFile(t, e.src("a/one.txt"), "one")
	// 대상에 같은 이름의 디렉터리가 있어 파일을 쓸 수 없다.
	if err := os.MkdirAll(e.tgt("a/one.txt"), 0o755); err != nil {
		t.Fatal(err)
	}

	summary := e.mustRun()

	if summary.Errors == 0 {
		t.Fatal("copy error must be counted")
	}
	if summary.HistorySaved || testkit.Exists(historyPath(e.source, e.target)) {
		t.Error("history must not be saved when errors exist")
	}
}

func TestMissingTargetRootStops(t *testing.T) {
	e := newEnv(t)
	testkit.WriteFile(t, e.src("a/one.txt"), "one")
	if err := os.Remove(e.target); err != nil {
		t.Fatal(err)
	}

	if _, err := e.run(); err == nil {
		t.Error("missing target root must stop the run")
	}
}

func TestSymlinkIsSkipped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink needs privilege on Windows")
	}
	e := newEnv(t)
	testkit.WriteFile(t, e.src("a/one.txt"), "one")
	if err := os.Symlink(e.src("a/one.txt"), e.src("a/link.txt")); err != nil {
		t.Fatal(err)
	}

	summary := e.mustRun()

	if summary.Files != 1 || testkit.Exists(e.tgt("a/link.txt")) {
		t.Errorf("symlink must be skipped: %+v", summary)
	}
}

func TestHistoryFileName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux path rule")
	}
	got := historyPath("/nas/src", "/backup/tgt")
	if want := filepath.Join("history", "nas_src-backup_tgt.vbr"); got != want {
		t.Errorf("history path = %q, want %q", got, want)
	}
}
