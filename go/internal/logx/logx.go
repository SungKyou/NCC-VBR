// Package logx 는 원본 BS_Tsyslog / TRAN_LOG3 과 같은 형식의 파일 로그를 쓴다.
//
// 형식과 교체 규칙은 운영 중인 조회 도구와의 호환을 위해 원본을 그대로 따른다.
//   - server.log      : "[YYYY-MM-DD HH:MM:SS]메시지"
//   - transaction.log : "\x00[YYYY-MM-DD HH:MM:SS:mmm]메시지\n"
//   - 기록 후 파일 크기가 최대값을 넘으면 "<파일>.old" 로 바꾼다(이전 .old 는 덮어쓴다).
//
// 원본과 다른 점: 여러 작업자가 동시에 써도 줄이 섞이지 않도록 잠금을 건다.
package logx

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// ServerLogFile 은 원본 LOG_FILE 이다.
	ServerLogFile = "server.log"
	// TransactionLogFile 은 원본 TRAN_LOG_FILE2 다.
	TransactionLogFile = "transaction.log"
	// DefaultMaxSize 는 원본 BS_initialize 의 기본 최대 크기다.
	DefaultMaxSize int64 = 50_000_000
)

// Logger 는 한 디렉터리 아래의 로그 파일 묶음이다.
type Logger struct {
	mu      sync.Mutex
	dir     string
	maxSize int64
	now     func() time.Time
}

// New 는 dir 아래에 로그를 쓰는 Logger 를 만든다.
func New(dir string) *Logger {
	return &Logger{dir: dir, maxSize: DefaultMaxSize, now: time.Now}
}

// SetMaxSize 는 로그 파일 교체 기준 크기(byte)를 바꾼다. 0 이하면 무시한다.
func (l *Logger) SetMaxSize(size int64) {
	if size <= 0 {
		return
	}
	l.mu.Lock()
	l.maxSize = size
	l.mu.Unlock()
}

// Printf 는 server.log 에 한 건을 쓴다. 줄바꿈은 호출하는 쪽이 넣는다(원본과 같다).
func (l *Logger) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.printfLocked(format, args...)
}

func (l *Logger) printfLocked(format string, args ...any) {
	stamp := l.now().Format("2006-01-02 15:04:05")
	line := "[" + stamp + "]" + fmt.Sprintf(format, args...)
	size, err := l.appendLine(ServerLogFile, line)
	if err != nil {
		fmt.Println("Log file open error")
		return
	}
	l.rotateLocked(ServerLogFile, size)
}

// Tran 은 transaction.log 에 한 건을 쓴다. 줄바꿈은 자동으로 붙는다.
func (l *Logger) Tran(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	stamp := fmt.Sprintf("%s:%03d", now.Format("2006-01-02 15:04:05"), now.Nanosecond()/1_000_000)
	line := "\x00[" + stamp + "]" + fmt.Sprintf(format, args...) + "\n"
	size, err := l.appendLine(TransactionLogFile, line)
	if err != nil {
		l.printfLocked("Transaction Log file open error\n")
		return
	}
	l.rotateLocked(TransactionLogFile, size)
}

// Append 는 작업 디렉터리의 name 파일에 text 를 그대로 덧붙인다.
// rotate 가 참이면 최대 크기를 넘었을 때 .old 로 바꾼다(원본 BACKUP_LOG).
// 거짓이면 바꾸지 않는다(원본 TRAN_LOG2: 월별 파일이라 크기로 바꾸지 않는다).
// 파일을 열지 못하면 server.log 에 openError 를 남긴다.
func (l *Logger) Append(name, text string, rotate bool, openError string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	size, err := l.appendLine(name, text)
	if err != nil {
		l.printfLocked("%s\n", openError)
		return
	}
	if rotate {
		l.rotateLocked(name, size)
	}
}

// Now 는 로그에 쓰는 현재 시각이다(시험에서 바꿀 수 있다).
func (l *Logger) Now() time.Time {
	return l.now()
}

func (l *Logger) appendLine(name, line string) (int64, error) {
	path := filepath.Join(l.dir, name)
	fp, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		return 0, err
	}
	defer fp.Close()

	if _, err := fp.WriteString(line); err != nil {
		return 0, err
	}
	info, err := fp.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (l *Logger) rotateLocked(name string, size int64) {
	if size <= l.maxSize {
		return
	}
	path := filepath.Join(l.dir, name)
	shown := "./" + name
	if err := os.Rename(path, path+".old"); err != nil {
		l.printfLocked("LOG FILE switch failed : current file size [%s][%d]\n", shown, size)
		return
	}
	l.printfLocked("LOG FILE switch success : before file size [%s][%d]\n", shown, size)
}
