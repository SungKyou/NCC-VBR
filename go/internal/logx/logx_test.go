package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixed(l *Logger) {
	l.now = func() time.Time {
		return time.Date(2026, 11, 17, 9, 5, 3, 42_000_000, time.Local)
	}
}

func TestServerLogFormat(t *testing.T) {
	dir := t.TempDir()
	log := New(dir)
	fixed(log)
	log.Printf("Backup base time  = [%d]\n", 0)

	data, _ := os.ReadFile(filepath.Join(dir, ServerLogFile))
	if want := "[2026-11-17 09:05:03]Backup base time  = [0]\n"; string(data) != want {
		t.Errorf("got %q, want %q", data, want)
	}
}

func TestTransactionLogFormat(t *testing.T) {
	dir := t.TempDir()
	log := New(dir)
	fixed(log)
	log.Tran("VBR CIFS backup start.   source dir = [%s]", "/a")

	data, _ := os.ReadFile(filepath.Join(dir, TransactionLogFile))
	want := "\x00[2026-11-17 09:05:03:042]VBR CIFS backup start.   source dir = [/a]\n"
	if string(data) != want {
		t.Errorf("got %q, want %q", data, want)
	}
}

func TestRotateWhenOverMaxSize(t *testing.T) {
	dir := t.TempDir()
	log := New(dir)
	fixed(log)
	log.SetMaxSize(100)
	for i := 0; i < 4; i++ {
		log.Printf("%s\n", strings.Repeat("x", 30))
	}

	old, err := os.ReadFile(filepath.Join(dir, ServerLogFile+".old"))
	if err != nil {
		t.Fatal("rotated file not found:", err)
	}
	if len(old) <= 100 {
		t.Errorf("rotated file size = %d, want > 100", len(old))
	}
	current, _ := os.ReadFile(filepath.Join(dir, ServerLogFile))
	if !strings.Contains(string(current), "LOG FILE switch success : before file size [./server.log]") {
		t.Errorf("switch message not found: %q", current)
	}
}
