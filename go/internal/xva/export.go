// copied from ncc-xen backend/internal/job/vm_backup_export.go runExport 203~375행 @043c6d5 (CP-VM-05), 2026-11-19.
// 바뀐 부분(docs/vm/08-copied-code-register.md CP-VM-05):
//   - 요청 해석·세션 찾기·VM 상태 판단·임시 스냅샷은 vm 패키지로 옮기고, 이 함수는 "uuid 를 파일로 받기"만 한다.
//   - 규약 2 processing 송신 대신 Progress 콜백을 부른다.
//   - 작업이 취소되면 XAPI task 를 취소한다(1단계 agent/transfer.go watchCancel).
//   - task 최종 상태 확인을 12초 제한에서 ctx 까지로 바꿨다(1단계와 같음).
//   - 파일 권한 0666(원본 vbr_agent chmod)을 유지했다.
//   - 오류를 i18n 문장 대신 코드가 있는 *Error 로 돌려준다.

package xva

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"

	"ncc-vbr/internal/xapi"
)

// exportFileMode 는 1단계 vbr_agent(원본 chmod 0666)와 같다.
const exportFileMode = 0o666

// ExportParams 는 내보내기 하나의 입력이다.
type ExportParams struct {
	Client      *xapi.Client
	VM          *xapi.VMClient
	HTTP        *http.Client
	UUID        string // 내보낼 VM 또는 스냅샷
	OutputPath  string // 확장자까지 붙은 최종 파일 경로(OutputName)
	Compression string // none / gzip / zstd
	Overwrite   bool
	TaskLabel   string
	Interval    time.Duration // 진행률·task 상태 확인 간격
	Progress    Progress
}

// ExportResult 는 내보낸 파일의 정보다.
type ExportResult struct {
	Size     int64
	SHA256   string // 디스크에 남은 파일 전체의 SHA256
	Duration time.Duration
}

// Export 는 UUID 를 XVA 파일로 받는다. 쓰는 동안은 ".partial" 이름이고, 스트림이 끝나고
// XAPI task 가 성공이어야 최종 이름으로 바꾼다. 실패·취소면 ".partial" 을 지운다.
func Export(ctx context.Context, p ExportParams) (ExportResult, error) {
	if _, err := os.Stat(p.OutputPath); err == nil && !p.Overwrite {
		return ExportResult{}, &Error{Code: CodeFileExists, Err: errorf("file already exists: %s", p.OutputPath)}
	}
	taskRef, err := p.VM.TaskCreate(ctx, p.TaskLabel, "vbr_vm export "+p.UUID)
	if err != nil {
		return ExportResult{}, &Error{Code: CodeXAPI, Err: err}
	}
	defer destroyTask(p.VM, taskRef)
	stopWatch := watchCancel(ctx, p.VM, taskRef)
	defer stopWatch()

	q := url.Values{}
	q.Set("session_id", p.Client.SessionRef())
	q.Set("uuid", p.UUID)
	q.Set("task_id", taskRef)
	if p.Compression == CompressionGzip {
		q.Set("use_compression", "true")
	}
	exportURL, err := endpoint(p.Client, "/export", q)
	if err != nil {
		return ExportResult{}, &Error{Code: CodeXAPI, Err: err}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, exportURL, nil)
	if err != nil {
		return ExportResult{}, &Error{Code: CodeXAPI, Err: err}
	}
	resp, err := p.HTTP.Do(httpReq)
	if err != nil {
		if e := ctxError(ctx); e != nil {
			return ExportResult{}, e
		}
		return ExportResult{}, &Error{Code: CodeXAPI, Err: errorf("export request failed: %v", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return ExportResult{}, &Error{Code: CodeXAPI,
			Err: errorf("export HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	}

	counter := &countingWriter{}
	stopProgress := pollTask(ctx, p.VM, taskRef, p.Interval, func() int64 { return atomic.LoadInt64(&counter.n) }, p.Progress)
	partial := p.OutputPath + ".partial"
	start := time.Now()
	sum, copyErr := writePartial(partial, resp.Body, counter, p.Compression)
	stopProgress()
	if copyErr == nil {
		copyErr = waitTask(ctx, p.VM, taskRef, p.Interval, CodeExportIncomplete)
	}
	if copyErr != nil {
		_ = os.Remove(partial)
		var xe *Error
		if errors.As(copyErr, &xe) {
			return ExportResult{}, xe
		}
		if e := ctxError(ctx); e != nil {
			return ExportResult{}, e
		}
		return ExportResult{}, &Error{Code: CodeFileIO, Err: copyErr}
	}
	if p.Overwrite {
		_ = os.Remove(p.OutputPath) // Windows 의 rename 은 기존 파일을 덮지 못한다
	}
	if err := os.Rename(partial, p.OutputPath); err != nil {
		_ = os.Remove(partial)
		return ExportResult{}, &Error{Code: CodeFileIO, Err: errorf("backup file rename failed: %v", err)}
	}
	_ = os.Chmod(p.OutputPath, exportFileMode)
	size := atomic.LoadInt64(&counter.n)
	if info, err := os.Stat(p.OutputPath); err == nil {
		size = info.Size()
	}
	return ExportResult{Size: size, SHA256: sum, Duration: time.Since(start)}, nil
}

// writePartial 은 스트림을 partial 파일에 쓰고 디스크에 남는 내용의 SHA256 을 돌려준다.
// zstd 면 받으면서 압축한다(ncc-xen 2026-10-16 동시 압축). counter 는 받은 바이트(압축 전)를 센다.
func writePartial(partial string, body io.Reader, counter *countingWriter, compression string) (string, error) {
	out, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, exportFileMode)
	if err != nil {
		return "", errorf("backup file create failed: %v", err)
	}
	hasher := sha256.New()
	var copyErr error
	if compression == CompressionZstd {
		enc, encErr := zstd.NewWriter(io.MultiWriter(out, hasher))
		if encErr != nil {
			_ = out.Close()
			return "", errorf("zstd encoder failed: %v", encErr)
		}
		_, copyErr = io.Copy(io.MultiWriter(enc, counter), body)
		if cerr := enc.Close(); cerr != nil && copyErr == nil {
			copyErr = cerr
		}
	} else {
		_, copyErr = io.Copy(io.MultiWriter(out, hasher, counter), body)
	}
	if cerr := out.Close(); copyErr == nil {
		copyErr = cerr // 닫기 오류(디스크 가득 등)도 실패로 본다
	}
	if copyErr != nil {
		return "", copyErr
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
