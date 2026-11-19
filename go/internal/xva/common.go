// Package xva 는 XAPI 의 /export, /import HTTP 핸들러로 XVA 파일을 주고받고, XVA 파일을 검증한다.
// 작업 상태와 결과 보고는 모른다(vm 패키지가 한다). 출처는 파일마다 머리 주석에 있다.
package xva

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"sync/atomic"
	"time"

	"ncc-vbr/internal/xapi"
)

// 압축 방식(05 문서 export options.compression).
const (
	CompressionNone = "none"
	CompressionGzip = "gzip"
	CompressionZstd = "zstd"
)

// 오류 코드(05 문서 9장 작업 단계). vm 패키지가 작업 상태의 error.code 로 옮긴다.
const (
	CodeInvalidPath      = "INVALID_FIELD"
	CodePathNotAllowed   = "PATH_NOT_ALLOWED"
	CodeFileExists       = "FILE_EXISTS"
	CodeFileIO           = "FILE_IO_ERROR"
	CodeExportIncomplete = "EXPORT_INCOMPLETE"
	CodeXAPI             = "XAPI_ERROR"
	CodeCanceled         = "CANCELED"
	CodeTimeout          = "TIMEOUT"
)

// Error 는 코드가 붙은 오류다. Detail 은 XAPI·파일 오류 원문이다(번역하지 않는다).
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func errorf(format string, args ...interface{}) error { return fmt.Errorf(format, args...) }

// ctxError 는 ctx 가 끝나서 난 오류를 취소·시간 초과 코드로 바꾼다. 그 밖이면 nil 이다.
func ctxError(ctx context.Context) *Error {
	switch ctx.Err() {
	case context.Canceled:
		return &Error{Code: CodeCanceled, Err: context.Canceled}
	case context.DeadlineExceeded:
		return &Error{Code: CodeTimeout, Err: context.DeadlineExceeded}
	}
	return nil
}

// Progress 는 진행률(0~100)과 지금까지 옮긴 바이트를 받는다. nil 이면 보고하지 않는다.
type Progress func(percent int, bytes int64)

// countingWriter·countingReader 는 옮긴 바이트를 센다(ncc-xen vm_backup_restore.go 하단과 같은 역할).
type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) {
	atomic.AddInt64(&c.n, int64(len(p)))
	return len(p), nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	atomic.AddInt64(&c.n, int64(n))
	return n, err
}

// percentOf 는 XAPI task 진행률(0~1)을 1~99 로 바꾼다(ncc-xen 과 같은 식).
func percentOf(progress float64) int {
	p := int(progress*98) + 1
	if p < 1 {
		return 1
	}
	if p > 99 {
		return 99
	}
	return p
}

// pollTask 는 interval 마다 XAPI task 진행률을 읽어 report 에 넘긴다. 돌려준 함수로 멈춘다.
func pollTask(ctx context.Context, vm *xapi.VMClient, taskRef string, interval time.Duration,
	bytes func() int64, report Progress) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	if report == nil {
		close(done)
		return func() {}
	}
	go func() {
		defer close(done)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				pr, err := vm.TaskGetProgress(ctx, taskRef)
				if err != nil {
					continue
				}
				report(percentOf(pr), bytes())
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

// watchCancel 은 ctx 가 끝나면 XAPI task 를 취소한다(1단계 agent/transfer.go 와 같다).
func watchCancel(ctx context.Context, vm *xapi.VMClient, taskRef string) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			cctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			_ = vm.CancelTask(cctx, taskRef)
		case <-done:
		}
	}()
	return func() { close(done) }
}

// waitTask 는 task 가 끝날 때까지 interval 마다 본다. 실패·취소면 EXPORT_INCOMPLETE·XAPI 오류다.
// task 를 조회할 수 없으면 스트림 완료를 믿는다(ncc-xen 과 같다).
func waitTask(ctx context.Context, vm *xapi.VMClient, taskRef string, interval time.Duration, code string) error {
	for {
		st, err := vm.TaskGetStatus(ctx, taskRef)
		if err != nil {
			if e := ctxError(ctx); e != nil {
				return e
			}
			return nil
		}
		switch st {
		case "pending", "cancelling":
		case "failure", "cancelled":
			if e := ctxError(ctx); e != nil {
				return e
			}
			info, _ := vm.TaskGetErrorInfo(context.Background(), taskRef)
			return &Error{Code: code, Err: errorf("XAPI task %s: %s", st, info)}
		default:
			return nil
		}
		select {
		case <-ctx.Done():
			return ctxError(ctx)
		case <-time.After(interval):
		}
	}
}

// settleTimeout 은 취소한 XAPI task 가 끝나기를 기다리는 최대 시간이다.
var settleTimeout = 60 * time.Second

// settleIfCanceled 는 작업이 취소됐으면 XAPI task 가 pending·cancelling 을 벗어날 때까지 기다린다.
// 기다리지 않으면 XenServer 가 아직 스냅샷 디스크를 잡고 있어 뒤이은 정리가 VDI_IN_USE 로
// 실패한다(2026-11-19 실기 I07). 취소와 무관한 새 ctx 로 본다.
func settleIfCanceled(ctx context.Context, vm *xapi.VMClient, taskRef string) {
	if ctx.Err() == nil {
		return
	}
	sctx, cancel := context.WithTimeout(context.Background(), settleTimeout)
	defer cancel()
	for {
		st, err := vm.TaskGetStatus(sctx, taskRef)
		if err != nil || (st != "pending" && st != "cancelling") {
			return
		}
		select {
		case <-sctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func destroyTask(vm *xapi.VMClient, taskRef string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = vm.TaskDestroy(ctx, taskRef)
}

// endpoint 는 /export, /import 주소다. 마스터 전환으로 바뀐 BaseURL 을 따른다(1단계 agent/transfer.go).
func endpoint(c *xapi.Client, path string, q url.Values) (string, error) {
	u, err := url.Parse(c.BaseURL())
	if err != nil {
		return "", errorf("XAPI address is not valid: %v", err)
	}
	u.Path = path
	u.RawQuery = q.Encode()
	return u.String(), nil
}
