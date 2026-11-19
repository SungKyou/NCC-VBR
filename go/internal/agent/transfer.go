// 출처: ncc-xen backend/internal/job/vm_backup_export.go runExport 의 203~375행,
//       vm_backup_restore.go runImport 의 158~353행 (커밋 043c6d5, 2026-11-17 복사)
// 바뀐 부분: docs/agent/08-copied-code-register.md CP-06, CP-07
//   - 요청·설정·세션 구조(Request, BackupSettings, poolSession) 대신 인자로 받는다
//   - 진행률 게이트웨이 송신, zstd, sha256, OVF 분기를 뺐다
//   - 오류 문장을 i18n 키에서 고정 영어 문장으로 바꿨다
//   - 작업이 취소되면 XAPI task 도 취소한다(새로 더함)
//   - 복구 결과가 VM 하나가 아니면 실패로 본다(원본 vbr: vm-import 출력이 uuid 하나여야 성공)

package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"ncc-vbr/internal/xapi"
)

// exportFileMode 는 원본 chmod(file_name, 0666) 이다.
const exportFileMode = 0o666

// endpoint 는 /export, /import 주소다. 마스터 전환으로 바뀐 BaseURL 을 따른다.
func endpoint(c *xapi.Client, path string, q url.Values) (string, error) {
	u, err := url.Parse(c.BaseURL())
	if err != nil {
		return "", fmt.Errorf("XAPI address is not valid: %w", err)
	}
	u.Path = path
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// watchCancel 은 ctx 가 끝나면 XAPI task 를 취소한다. 돌려준 함수로 감시를 끝낸다.
func (x *Xen) watchCancel(ctx context.Context, vm *xapi.VMClient, taskRef string) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			cctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
			defer cancel()
			if err := vm.CancelTask(cctx, taskRef); err != nil {
				x.log.Printf("XAPI task cancel failed [%s] %v\n", taskRef, err)
			}
		case <-done:
		}
	}()
	return func() { close(done) }
}

// waitTask 는 task 가 끝날 때까지 기다린다. 실패·취소면 오류다.
// (원본 ncc-xen: export 는 12초, import 는 ctx 까지. 여기서는 둘 다 ctx 까지 기다린다.)
func waitTask(ctx context.Context, vm *xapi.VMClient, taskRef string) error {
	for {
		st, err := vm.TaskGetStatus(ctx, taskRef)
		if err != nil {
			return nil // task 조회 불가 시 스트림 완료를 신뢰(ncc-xen 과 같다)
		}
		switch st {
		case "pending", "cancelling":
		case "failure", "cancelled":
			info, _ := vm.TaskGetErrorInfo(ctx, taskRef)
			return fmt.Errorf("XAPI task %s: %s", st, info)
		default:
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func destroyTask(vm *xapi.VMClient, taskRef string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = vm.TaskDestroy(ctx, taskRef)
}

// export 는 스냅샷을 XVA(gzip) 파일로 받는다. 원본 xe vm-export vm=… filename=… compress=true 다.
func (x *Xen) export(ctx context.Context, c *xapi.Client, vm *xapi.VMClient, snapUUID, fileName string) error {
	taskRef, err := vm.TaskCreate(ctx, "NCC-VBR VM Export", "vbr_agent export "+snapUUID)
	if err != nil {
		return err
	}
	defer destroyTask(vm, taskRef)
	stopWatch := x.watchCancel(ctx, vm, taskRef)
	defer stopWatch()

	q := url.Values{}
	q.Set("session_id", c.SessionRef())
	q.Set("uuid", snapUUID)
	q.Set("task_id", taskRef)
	q.Set("use_compression", "true")
	exportURL, err := endpoint(c, "/export", q)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, exportURL, nil)
	if err != nil {
		return err
	}
	resp, err := x.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("export request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("export HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// 쓰는 동안은 .partial 이름을 쓴다(NX-03). 원본은 최종 이름으로 바로 썼다.
	partial := fileName + ".partial"
	out, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, exportFileMode)
	if err != nil {
		return fmt.Errorf("backup file create failed: %w", err)
	}
	start := time.Now()
	n, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr == nil {
		copyErr = closeErr // 닫기 오류(디스크 가득 등)도 실패로 본다
	}
	if copyErr == nil {
		copyErr = waitTask(ctx, vm, taskRef)
	}
	if copyErr != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("export write failed: %w", copyErr)
	}
	if err := os.Rename(partial, fileName); err != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("backup file rename failed: %w", err)
	}
	_ = os.Chmod(fileName, exportFileMode)
	x.log.Printf("VM export file [%s] size [%d] elapsed [%s]\n", fileName, n, time.Since(start).Round(time.Second))
	return nil
}

// importFile 은 XVA 파일을 올려 VM 을 만든다. 원본 xe vm-import filename=… sr-uuid=… preserve=… 다.
func (x *Xen) importFile(ctx context.Context, c *xapi.Client, vm *xapi.VMClient, req RestoreRequest) (string, error) {
	srRef, err := vm.SRByUUID(ctx, req.SRUUID)
	if err != nil {
		return "", err
	}
	f, err := os.Open(req.FileName)
	if err != nil {
		return "", fmt.Errorf("restore file open failed: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("restore file stat failed: %w", err)
	}

	taskRef, err := vm.TaskCreate(ctx, "NCC-VBR VM Import", "vbr_agent import "+req.FileName)
	if err != nil {
		return "", err
	}
	defer destroyTask(vm, taskRef)
	stopWatch := x.watchCancel(ctx, vm, taskRef)
	defer stopWatch()

	// sr_id / restore 파라미터는 ncc-xen 에서도 실기검증 전이다(NX-08, PT-02).
	// restore=true 가 xe vm-import preserve=true 와 같다고 보고 있다.
	q := url.Values{}
	q.Set("session_id", c.SessionRef())
	q.Set("task_id", taskRef)
	q.Set("sr_id", srRef)
	q.Set("restore", strconv.FormatBool(req.Preserve))
	importURL, err := endpoint(c, "/import", q)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, importURL, f)
	if err != nil {
		return "", err
	}
	httpReq.ContentLength = info.Size()
	resp, err := x.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("import request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("import HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	// 업로드 종료 ≠ import 완료 — task 최종 상태로 판정(ncc-xen 2026-07-28 실기 확인)
	if err := waitTask(ctx, vm, taskRef); err != nil {
		return "", err
	}
	refs, err := vm.TaskGetResultRefs(ctx, taskRef)
	if err != nil {
		return "", err
	}
	if len(refs) != 1 {
		return "", fmt.Errorf("VM import result is not a single VM. [%d]", len(refs))
	}
	record, err := vm.GetRecord(ctx, refs[0])
	if err != nil {
		return "", err
	}
	uuid, _ := record["uuid"].(string)
	// 스냅샷 기반 XVA 는 템플릿으로 만들어지는 XAPI 동작이 있다(ncc-xen 후처리, 원본 vbr 은 내보내기 전에 바꿨다).
	if t, _ := record["is_a_template"].(string); t == "1" || t == "true" {
		if err := vm.SetIsATemplate(ctx, refs[0], false); err != nil {
			x.log.Printf("imported VM is_a_template fix failed (ignored) [%s] %v\n", uuid, err)
		} else if err := vm.UpdateAllowedOperations(ctx, refs[0]); err != nil {
			x.log.Printf("imported VM update_allowed_operations failed (ignored) [%s] %v\n", uuid, err)
		}
	}
	return uuid, nil
}
