// Package testkit 은 시험에서 함께 쓰는 도우미다.
package testkit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Log 는 로그를 메모리에 모으는 시험용 Logger 다.
type Log struct {
	mu    sync.Mutex
	lines []string
}

// Printf 는 server.log 대신 메모리에 적는다.
func (l *Log) Printf(format string, args ...any) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

// Tran 은 transaction.log 대신 메모리에 적는다.
func (l *Log) Tran(format string, args ...any) {
	l.Printf("TRAN "+format+"\n", args...)
}

// Contains 는 모은 로그에 text 가 있는지 알려준다.
func (l *Log) Contains(text string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Contains(strings.Join(l.lines, ""), text)
}

// Chdir 은 시험 동안 작업 디렉터리를 dir 로 옮긴다.
func Chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

// WriteFile 은 상위 디렉터리를 만들고 파일을 쓴다.
func WriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ReadFile 은 파일 내용을 문자열로 읽는다.
func ReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Exists 는 경로가 있는지 알려준다.
func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// Touch 는 파일의 접근·수정 시각을 바꾼다.
func Touch(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}
