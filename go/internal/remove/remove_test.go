package remove

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ncc-vbr/internal/config"
	"ncc-vbr/internal/testkit"
)

func TestIsExpiredVersion(t *testing.T) {
	const cutoff = "20261101000000"
	cases := []struct {
		path string
		want bool
	}{
		{"/nas/a/report.xlsx-vbr-20261031235959.zip", true},
		{"/nas/a/report.xlsx-vbr-20261101000000.zip", true},
		{"/nas/a/report.xlsx-vbr-20261101000001.zip", false},
		{"/nas/a/report.xlsx", false},
		{"/nas/a/archive.zip", false},
		{"/nas/a/report.xlsx-vbr-20261031235959.zip.bak", false},
		{"/nas/a/report.xlsx-VBR-20261031235959.zip", false},
		{"/nas/a/report.xlsx-vbr-2026103123595X.zip", false},
		{"-vbr-20200101000000.zip", false},
		{"/nas/a/-vbr-20200101000000.zip", false},
		{"a-vbr-20200101000000.zip", true},
	}
	for _, c := range cases {
		if got := IsExpiredVersion(c.path, cutoff); got != c.want {
			t.Errorf("IsExpiredVersion(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestParseDays(t *testing.T) {
	for _, text := range []string{"abc", "-1", "", "3d", "106752"} {
		if _, err := ParseDays(text); err == nil {
			t.Errorf("ParseDays(%q) must fail", text)
		}
	}
	if days, err := ParseDays("90"); err != nil || days != 90 {
		t.Errorf("ParseDays(90) = %d, %v", days, err)
	}
}

func TestRunRejectsOverflowingDaysBeforeTraversing(t *testing.T) {
	_, err := Run(context.Background(), Job{Days: maxRetentionDays + 1})
	if err == nil {
		t.Fatal("out-of-range retention must be rejected")
	}
}

func TestShellQuote(t *testing.T) {
	if got, want := shellQuote("/a/it's.zip"), `'/a/it'\''s.zip'`; got != want {
		t.Errorf("shellQuote = %s, want %s", got, want)
	}
}

type env struct {
	t    *testing.T
	home string
	root string
	log  *testkit.Log
	old  string
	new  string
	keep string
}

// 기준일은 2026-11-17 에서 30일 전인 2026-10-18 이다.
var clock = time.Date(2026, 11, 17, 12, 0, 0, 0, time.Local)

func newEnv(t *testing.T) *env {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, "nas")
	e := &env{
		t:    t,
		home: home,
		root: root,
		log:  &testkit.Log{},
		old:  filepath.Join(root, "a", "deep", "doc.txt-vbr-20260901101010.zip"),
		new:  filepath.Join(root, "a", "doc.txt-vbr-20261110101010.zip"),
		keep: filepath.Join(root, "a", "doc.txt"),
	}
	for _, path := range []string{e.old, e.new, e.keep} {
		testkit.WriteFile(t, path, "x")
	}
	testkit.WriteFile(t, filepath.Join(root, "top.txt-vbr-20250101000000.zip"), "x")
	testkit.WriteFile(t, filepath.Join(root, "skip", "s.txt-vbr-20250101000000.zip"), "x")
	testkit.Chdir(t, home)
	return e
}

func (e *env) run(cfg string) Summary {
	e.t.Helper()
	path := filepath.Join(e.home, "node_info.cfg")
	testkit.WriteFile(e.t, path, cfg)
	file, err := config.Load(path)
	if err != nil {
		e.t.Fatal(err)
	}
	summary, err := Run(context.Background(), Job{
		Dir:    e.root,
		Days:   30,
		Config: file.Section(Title),
		Log:    e.log,
		Now:    func() time.Time { return clock },
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return summary
}

func TestLogModeDeletesNothing(t *testing.T) {
	e := newEnv(t)
	testkit.WriteFile(t, DeleteLogFile, "stale line\n")

	summary := e.run("[" + Title + "]\nMAX Thread cnt = 2\n")

	if summary.Delete || summary.Deleted != 3 || summary.Files != 5 {
		t.Errorf("summary = %+v", summary)
	}
	if !testkit.Exists(e.old) {
		t.Error("LOG mode must not delete files")
	}
	lines := strings.Split(strings.TrimSpace(testkit.ReadFile(t, DeleteLogFile)), "\n")
	if len(lines) != 3 {
		t.Fatalf("delete.log lines = %d, want 3: %v", len(lines), lines)
	}
	found := false
	for _, line := range lines {
		if line == "rm -f -- '"+e.old+"'" {
			found = true
		}
	}
	if !found {
		t.Errorf("delete.log must contain command for %s: %v", e.old, lines)
	}
}

func TestDeleteModeRemovesOnlyExpiredVersions(t *testing.T) {
	e := newEnv(t)
	cfg := "[" + Title + "]\nDelete Mode = DELETE\nMAX Thread cnt = 2\n" +
		"EXCLUSION DIR = " + filepath.Join(e.root, "skip") + "\n"

	summary := e.run(cfg)

	if !summary.Delete || summary.Deleted != 2 || summary.Cutoff != "20261018120000" {
		t.Errorf("summary = %+v", summary)
	}
	if testkit.Exists(e.old) {
		t.Error("expired version must be deleted")
	}
	for _, path := range []string{e.new, e.keep, filepath.Join(e.root, "skip", "s.txt-vbr-20250101000000.zip")} {
		if !testkit.Exists(path) {
			t.Errorf("%s must be kept", path)
		}
	}
	if testkit.Exists(DeleteLogFile) {
		t.Error("DELETE mode must not write delete.log")
	}
}

func TestMissingDirectoryFails(t *testing.T) {
	testkit.Chdir(t, t.TempDir())
	_, err := Run(context.Background(), Job{
		Dir:    filepath.Join(os.TempDir(), "ncc-vbr-no-such-dir"),
		Days:   1,
		Log:    &testkit.Log{},
		Config: config.Section{},
	})
	if err == nil {
		t.Error("missing directory must fail")
	}
}
