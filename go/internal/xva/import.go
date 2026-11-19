// copied from ncc-xen backend/internal/job/vm_backup_restore.go runImport 158~320행 @043c6d5 (CP-VM-06), 2026-11-19.
// 바뀐 부분(docs/vm/08-copied-code-register.md CP-VM-06):
//   - 경로 해석·zstd 해제·SR 조회·결과 VM 후처리는 vm 패키지로 옮기고, 이 함수는 "파일을 올려 VM 참조 받기"만 한다.
//   - 규약 2 processing 송신 대신 Progress 콜백을 부른다.
//   - 작업이 취소되면 XAPI task 를 취소한다(1단계 agent/transfer.go watchCancel).
//   - 오류를 i18n 문장 대신 코드가 있는 *Error 로 돌려준다.

package xva

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"ncc-vbr/internal/xapi"
)

// ImportParams 는 가져오기 하나의 입력이다.
type ImportParams struct {
	Client    *xapi.Client
	VM        *xapi.VMClient
	HTTP      *http.Client
	Path      string // 올릴 파일(gzip·비압축. zstd 는 미리 풀어 둔 임시 파일)
	SRRef     string // SR OpaqueRef(SR.get_by_uuid 결과)
	Restore   bool   // XAPI restore 파라미터(= restore_original_uuid, 기존 preserve)
	Force     bool
	TaskLabel string
	Interval  time.Duration
	Progress  Progress
}

// Import 는 XVA 파일을 /import 로 올리고 XAPI task 결과의 VM 참조 목록을 돌려준다.
// 업로드가 HTTP 200 으로 끝나도 XAPI 가 디스크를 계속 처리하므로 task 최종 상태까지 기다린다.
func Import(ctx context.Context, p ImportParams) ([]string, error) {
	f, err := os.Open(p.Path)
	if err != nil {
		return nil, &Error{Code: CodeFileIO, Err: errorf("restore file open failed: %v", err)}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, &Error{Code: CodeFileIO, Err: errorf("restore file stat failed: %v", err)}
	}

	taskRef, err := p.VM.TaskCreate(ctx, p.TaskLabel, "vbr_vm import "+p.Path)
	if err != nil {
		return nil, &Error{Code: CodeXAPI, Err: err}
	}
	defer destroyTask(p.VM, taskRef)
	stopWatch := watchCancel(ctx, p.VM, taskRef)
	defer stopWatch()

	q := url.Values{}
	q.Set("session_id", p.Client.SessionRef())
	q.Set("task_id", taskRef)
	q.Set("sr_id", p.SRRef)
	q.Set("restore", strconv.FormatBool(p.Restore))
	if p.Force {
		q.Set("force", "true")
	}
	importURL, err := endpoint(p.Client, "/import", q)
	if err != nil {
		return nil, &Error{Code: CodeXAPI, Err: err}
	}
	counter := &countingReader{r: f}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, importURL, counter)
	if err != nil {
		return nil, &Error{Code: CodeXAPI, Err: err}
	}
	httpReq.ContentLength = info.Size()

	// 진행률은 최종 대기 구간까지 계속 본다(ncc-xen 2026-07-28: 실제 시간의 대부분이 그 구간).
	stopProgress := pollTask(ctx, p.VM, taskRef, p.Interval, func() int64 { return atomic.LoadInt64(&counter.n) }, p.Progress)
	defer stopProgress()

	resp, err := p.HTTP.Do(httpReq)
	if err != nil {
		if e := ctxError(ctx); e != nil {
			return nil, e
		}
		return nil, &Error{Code: CodeXAPI, Err: errorf("import request failed: %v", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, &Error{Code: CodeXAPI,
			Err: errorf("import HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	}
	if err := waitTask(ctx, p.VM, taskRef, 2*time.Second, CodeXAPI); err != nil {
		return nil, err
	}
	refs, err := p.VM.TaskGetResultRefs(ctx, taskRef)
	if err != nil {
		return nil, &Error{Code: CodeXAPI, Err: err}
	}
	if len(refs) == 0 {
		return nil, &Error{Code: CodeXAPI, Err: errorf("VM import result has no VM reference")}
	}
	return refs, nil
}
