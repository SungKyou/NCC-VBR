package job

// vm_backup_restore.go — VM_BACKUP action=restore (XVA import)
// 🆕 2026-09-01: vm_backup.go에서 기계적으로 분리됨 (로직 변경 없음).
// 원본 설계 주석(restore 파라미터, is_a_template 후처리 함정 등)은
// vm_backup.go 상단 참고.

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ncc/xen-backend/internal/i18n"
	"github.com/ncc/xen-backend/pkg/xapi/mixins"
)

func (e *VMBackupExecutor) runImport(ctx context.Context, req Request) {
	filePathOpt, _ := strOpt(req.Payload.Options, "file_path")
	if filePathOpt == "" {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m029"))
		return
	}
	srUUID, _ := strOpt(req.Payload.Options, "sr_uuid")
	if srUUID == "" {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m030"))
		return
	}
	nameLabel, _ := strOpt(req.Payload.Options, "name_label")
	restoreOriginalUUID := boolOpt(req.Payload.Options, "restore_original_uuid", false)
	force := boolOpt(req.Payload.Options, "force", false)
	// 🆕 2026-09-10 (프론트 요청) — home_server_uuid(선택) — XenCenter의 Home
	// Server 지정과 동일한 목적. 복원 완료 후 VM.set_affinity 호출
	// (vm_create.go template_create의 home_server_uuid와 동일한 best-effort
	// 정책 — 실패해도 restore job 자체는 실패시키지 않음). 미지정 시 기존
	// 동작(affinity 설정 안 함) 그대로 유지.
	homeServerUUID, hasHomeServer := strOpt(req.Payload.Options, "home_server_uuid")

	// ── 경로 해석 및 검증 (verify와 동일 규칙) ─────────────────
	fullPath := filepath.Clean(filePathOpt)
	if containsParentRef(fullPath) {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m008", filePathOpt))
		return
	}
	// ── 경로 해석 및 검증 ────────────────────────────────────
	if !filepath.IsAbs(fullPath) {
		fullPath = filepath.Join(e.settings.BasePath, fullPath)
	} else {
		// 2026-09-18: 절대경로는 SandboxMode=true일 때만 BasePath/
		// AllowedAbsolutePaths 하위로 제한(vm_backup_util.go 참고).
		if _, ok := resolvePathWithSandbox(e.settings, fullPath); !ok {
			e.sendFailure(ctx, req, i18n.T("vm_backup.m106", filePathOpt))
			return
		}
	}
	fileInfo, statErr := os.Stat(fullPath)
	if statErr != nil {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m031", statErr))
		return
	}
	fileSize := fileInfo.Size()

	// 🆕 2026-08-21: zstd 압축 파일 자동 감지 — XAPI /import는 zstd를 모르므로
	// 업로드 전 임시파일로 전체 압축해제한다(스트리밍 해제는 Content-Length를 미리 알 수 없어
	// 실기검증된 아래 업로드 로직을 건드려야 해서 채택하지 않음 — 임시파일 방식으로 기존 업로드 경로 완전 무변경 유지).
	// gzip은 XAPI가 서버측에서 자동 인식하므로(기존 동작) 이 분기와 무관 — 그대로 fullPath를 업로드.
	restorePath := fullPath
	restoreSize := fileSize
	isZstdRestore := false

	// 🆕 2026-08-21 수정: 압축해제 단계 진행률 보고 추가.
	// export의 zstd 압축 단계와 동일한 문제(진행률 보고 없이 조용히 돌다가 갑자기 완료되는 것처럼 보임)가
	// 여기도 구조적으로 동일하게 발생할 수 있어 실기테스트 전에 선제 수정(SK 2026-08-21 export 케이스 실기확인).
	// zstd가 아닌 파일이면 decompressZstdIfNeeded가 매직바이트만 확인하고 거의 즉시 반환되므로
	// 아래 ticker는 사실상 발동하지 않는다.
	var decompProgress int64
	decompStopProgress := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-decompStopProgress:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				read := atomic.LoadInt64(&decompProgress)
				percent := 1
				if fileSize > 0 {
					percent = int(float64(read)/float64(fileSize)*98) + 1
					if percent < 1 {
						percent = 1
					}
					if percent > 99 {
						percent = 99
					}
				}
				e.log.Info(i18n.T("vm_backup.l059"),
					"taskId", req.TaskID, "percent", percent, "bytesRead", read)
				_ = e.gw.SendProcessing(ctx,
					req.TaskID, "VM_BACKUP", req.Action, "FILE", req.Payload.TargetUUID,
					i18n.T("vm_backup.m105", percent), percent,
					map[string]interface{}{"step": "decompressing", "file_path": fullPath})
			}
		}
	}()
	zPath, zSize, zDetected, zErr := decompressZstdIfNeeded(fullPath, &decompProgress)
	close(decompStopProgress)
	if zErr != nil {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m103", zErr))
		return
	} else if zDetected {
		restorePath = zPath
		restoreSize = zSize
		isZstdRestore = true
		defer os.Remove(zPath)
		e.log.Info(i18n.T("vm_backup.l057"),
			"taskId", req.TaskID, "originalPath", fullPath, "tempPath", zPath, "decompressedSizeBytes", zSize)
		// 🆕 2026-10-16: decompressing 단계 종료를 100%로 명시 전송 — 이게 없으면 바로 뒤의
		// importing 단계가 1%부터 다시 시작해 Middleware 입장에서 99%→1%로 역행하는 것처럼
		// 보이는 문제가 있었다(export의 구 zstd 2-pass 구조에서 발견된 것과 동일한 패턴).
		_ = e.gw.SendProcessing(ctx,
			req.TaskID, "VM_BACKUP", req.Action, "FILE", req.Payload.TargetUUID,
			i18n.T("vm_backup.m105", 100), 100,
			map[string]interface{}{"step": "decompressing", "file_path": fullPath})
	}

	// ── Pool 세션 ────────────────────────────────────────────
	session, err := e.findSession(req.Payload.MasterIP, req.Payload.PoolUUID)
	if err != nil {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m002", err))
		return
	}
	client, err := e.ensureConnected(ctx, session)
	if err != nil {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m003", err))
		return
	}
	vmClient := mixins.NewVMClient(client)
	srClient := mixins.NewSRClient(client)

	// ── 대상 SR (OpaqueRef 필요 — SR 관련 XAPI 확정 동작) ──────
	srRef, err := srClient.GetByUUID(ctx, srUUID)
	if err != nil {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m032", err))
		return
	}

	f, err := os.Open(restorePath) // 🆕 2026-08-21: zstd였으면 압축해제된 임시파일 경로 사용 (아니면 기존과 동일하게 fullPath)
	if err != nil {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m031", err))
		return
	}
	defer f.Close()

	// ── XAPI Task 생성 (진행률 추적용) ────────────────────────
	taskRef, err := vmClient.TaskCreate(ctx, "NCC-Xen VM Restore", "VM_BACKUP restore taskId="+req.TaskID)
	if err != nil {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m015", err))
		return
	}
	defer func() {
		dctx, dcancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dcancel()
		if derr := vmClient.TaskDestroy(dctx, taskRef); derr != nil {
			e.log.Warn(i18n.T("vm_backup.l017"), "taskId", req.TaskID, "error", derr)
		}
	}()

	// ── import URL 구성 ────────────────────────────────────────
	// ⚠ sr_id/restore/force 파라미터명은 실기검증 전 — 확인 후 조정 가능
	q := url.Values{}
	q.Set("session_id", client.SessionRef())
	q.Set("task_id", taskRef)
	q.Set("sr_id", srRef)
	q.Set("restore", strconv.FormatBool(restoreOriginalUUID))
	if force {
		q.Set("force", "true")
	}
	importURL := fmt.Sprintf("https://%s/import?%s", session.masterIP, q.Encode())

	e.log.Info(i18n.T("vm_backup.l013"),
		"taskId", req.TaskID,
		"filePath", fullPath,
		"srUUID", srUUID,
		"restoreOriginalUUID", restoreOriginalUUID,
	)

	// ── HTTP 스트리밍 업로드 ────────────────────────────────────
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: !session.verifySSL},
		},
	}
	counter := &countingReader{r: f}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, importURL, counter)
	if err != nil {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m033", err))
		return
	}
	httpReq.ContentLength = restoreSize // 🆕 2026-08-21: zstd 해제 시 실제로 전송되는 바이트 수(압축 전 원본 크기)로 정확히 설정

	stopProgress := make(chan struct{})
	start := time.Now()
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopProgress:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				progress, perr := vmClient.TaskGetProgress(ctx, taskRef)
				if perr != nil {
					// 기존에는 무음으로 continue하여 진행 실패 원인을 알 수 없었음(2026-07-28 실기테스트 확인).
					// 원인 파악을 위해 경고 로그로 남긴다 (재시도 로직 자체는 변경 없음).
					e.log.Warn(i18n.T("vm_backup.l021"), "taskId", req.TaskID, "error", perr)
					continue
				}
				percent := int(progress*98) + 1
				if percent < 1 {
					percent = 1
				}
				if percent > 99 {
					percent = 99
				}
				e.log.Info(i18n.T("vm_backup.l014"),
					"taskId", req.TaskID, "percent", percent,
					"bytesSent", atomic.LoadInt64(&counter.n))
				_ = e.gw.SendProcessing(ctx,
					req.TaskID, "VM_BACKUP", req.Action, "FILE", req.Payload.TargetUUID,
					i18n.T("vm_backup.m041", percent), percent,
					map[string]interface{}{
						"step":      "importing",
						"file_path": fullPath,
					})
			}
		}
	}()

	e.log.Info(i18n.T("vm_backup.l020"), "taskId", req.TaskID, "url", importURL, "fileSizeBytes", fileSize)

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		close(stopProgress)
		e.sendFailure(ctx, req, i18n.T("vm_backup.m033", err))
		return
	}
	defer resp.Body.Close()
	bytesSent := atomic.LoadInt64(&counter.n)
	e.log.Info(i18n.T("vm_backup.l022"),
		"taskId", req.TaskID, "statusCode", resp.StatusCode,
		"bytesSent", bytesSent, "fileSizeBytes", fileSize)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		close(stopProgress)
		e.sendFailure(ctx, req, i18n.T("vm_backup.m034", resp.StatusCode, strings.TrimSpace(string(body))))
		return
	}
	// ⚠ 2026-07-28 실기테스트 확인: HTTP 200 시점의 bytesSent가 fileSize보다 작아도
	// Go http.Transport가 백그라운드에서 나머지 body를 계속 전송하며, 실제로 6GB 파일이
	// 56초 뒤 정상적으로 task 완료(success)까지 이어졌다. 즉 이 시점의 bytesSent는
	// "최종 전송량"이 아니라 "Do() 반환 시점의 스냅샷"일 뿐이므로 여기서 실패 처리하면 안 된다.
	// (이전 커밋에서 추가했던 조기 실패 로직은 이 확인에 따라 제거함 — 로그만 남긴다.)

	// ── 업로드 종료 ≠ import 완료 — Task 최종 상태로 판정 ─────
	// 대용량 VM은 스트림 종료 후에도 서버 측 disk import 처리가 계속될 수 있어
	// export의 12회(12초) 폴백보다 넉넉한 주기로 대기한다 (외부 상한은 ctx의
	// settings.TimeoutHours가 담당 — 실기검증 후 주기 조정 가능).
	// ⚠ 진행률 고루틴(stopProgress)은 여기서 닫지 않고 최종 상태 확정 후에 닫는다 —
	// 실제 소요 시간의 대부분이 이 대기 구간이라, 여기서 먼저 닫으면 진행률이 전혀
	// 안 보이는 문제가 있었다(2026-07-28 실기테스트 확인: 60초 가까이 진행률 로그 0건).
	finalStatus := ""
	for {
		select {
		case <-ctx.Done():
			close(stopProgress)
			e.sendFailure(ctx, req, i18n.T("vm_backup.m027", e.settings.TimeoutHours))
			return
		default:
		}
		st, serr := vmClient.TaskGetStatus(ctx, taskRef)
		if serr != nil {
			e.log.Warn(i18n.T("vm_backup.l023"), "taskId", req.TaskID, "error", serr)
			break // task 조회 불가 시 스트림 완료(HTTP 200)를 신뢰
		}
		finalStatus = st
		if st != "pending" && st != "cancelling" {
			break
		}
		time.Sleep(2 * time.Second)
	}
	close(stopProgress)
	if finalStatus == "failure" || finalStatus == "cancelled" {
		errInfo, _ := vmClient.TaskGetErrorInfo(ctx, taskRef)
		e.sendFailure(ctx, req, i18n.T("vm_backup.m040", errInfo))
		return
	}

	// ── import 결과 VM ref 조회 ─────────────────────────────────
	refs, rErr := vmClient.TaskGetResultRefs(ctx, taskRef)
	if rErr != nil || len(refs) == 0 {
		// 원문 진단 로그: TaskGetResultRefs의 정규식이 실제 XAPI 응답 포맷과
		// 맞지 않을 가능성이 있어, 다음 조정을 위해 raw 값을 그대로 남긴다.
		raw, rawErr := vmClient.TaskGetRawResult(ctx, taskRef)
		e.log.Warn(i18n.T("vm_backup.l024"), "taskId", req.TaskID, "rawResult", raw, "rawResultError", rawErr)
		e.sendFailure(ctx, req, i18n.T("vm_backup.m035", req.TaskID))
		return
	}
	if len(refs) > 1 {
		e.log.Warn(i18n.T("vm_backup.l018"), "taskId", req.TaskID, "refCount", len(refs))
	}
	newVMRef := refs[0]

	record, err := vmClient.GetRecord(ctx, newVMRef)
	if err != nil {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m036", err))
		return
	}
	newVMUUID, _ := record["uuid"].(string)

	// ── 알려진 함정 후처리: 스냅샷 기반 XVA는 is_a_template=true로 생성됨 ──
	isTemplateFixed := false
	var templateFixErr string
	var allowedOpsFixErr string // 🆕 2026-08-21: UpdateAllowedOperations 실패 시 원문 에러 보관
	if xapiBoolField(record["is_a_template"]) {
		e.log.Info(i18n.T("vm_backup.l016"), "taskId", req.TaskID, "vmUUID", newVMUUID)
		if serr := vmClient.SetIsATemplate(ctx, newVMRef, false); serr != nil {
			templateFixErr = serr.Error()
			e.log.Warn(i18n.T("vm_backup.l019"), "taskId", req.TaskID, "vmUUID", newVMUUID, "error", serr)
		} else {
			isTemplateFixed = true
			// 🆕 2026-08-21: zstd restore 실기테스트 중 발견 — SetIsATemplate(false)만으로는
			// allowed_operations가 재계산되지 않아 "provision"(템플릿 전용)은 남고 "start"는
			// 빠진 채로 고정되어 XenCenter Start 버튼이 비활성화되는 XAPI 동작 확인.
			// 반드시 명시적으로 재계산을 요청해야 함 — 압축방식(zstd/gzip/none)과 무관한 공통 버그.
			if uErr := vmClient.UpdateAllowedOperations(ctx, newVMRef); uErr != nil {
				allowedOpsFixErr = uErr.Error()
				e.log.Warn(i18n.T("vm_backup.l060"), "taskId", req.TaskID, "vmUUID", newVMUUID, "error", uErr)
			}
		}
	}

	var nameLabelErr string
	if nameLabel != "" {
		if lerr := vmClient.SetNameLabelByRef(ctx, newVMRef, nameLabel); lerr != nil {
			nameLabelErr = lerr.Error()
		}
	}

	// 🆕 2026-09-10 — Home Server 지정 (선택, best-effort).
	// vm_create.go template_create의 home_server_uuid와 완전히 동일한 정책:
	// VM.set_affinity 실패해도 restore job 자체는 성공 유지.
	var homeServerErr string
	homeServerApplied := false
	if hasHomeServer && homeServerUUID != "" {
		if err := vmClient.SetAffinity(ctx, newVMUUID, homeServerUUID); err != nil {
			homeServerErr = err.Error()
			e.log.Warn(i18n.T("vm_backup.l062"), "taskId", req.TaskID, "vmUUID", newVMUUID, "homeServerUUID", homeServerUUID, "error", err)
		} else {
			homeServerApplied = true
			e.log.Info(i18n.T("vm_backup.l061"), "taskId", req.TaskID, "vmUUID", newVMUUID, "homeServerUUID", homeServerUUID)
		}
	}

	durationSec := int(time.Since(start).Seconds())
	e.log.Info(i18n.T("vm_backup.l015"),
		"taskId", req.TaskID,
		"vmUUID", newVMUUID,
		"srUUID", srUUID,
		"durationSec", durationSec,
	)

	options := map[string]interface{}{
		"vm_uuid":               newVMUUID,
		"sr_uuid":               srUUID,
		"file_path":             fullPath,
		"file_size_bytes":       fileSize,
		"restore_original_uuid": restoreOriginalUUID,
		"is_a_template_fixed":   isTemplateFixed,
		"duration_seconds":      durationSec,
		"completedAt":           completedAt(),
	}
	if nameLabel != "" {
		options["name_label"] = nameLabel
	}
	if templateFixErr != "" {
		// 후처리 실패는 restore job 자체는 성공 유지 + Middleware 후속 조치용 원문 에러 포함 (합의 사항과 동일 패턴)
		options["is_a_template_fix_error"] = templateFixErr
	}
	if allowedOpsFixErr != "" {
		// 🆕 2026-08-21: allowed_operations 재계산 실패 — job은 성공 유지, Middleware/사용자가
		// 수동으로 VM.update_allowed_operations를 재시도하거나 XenCenter에서 새로고침 필요할 수 있음
		options["allowed_operations_fix_error"] = allowedOpsFixErr
	}
	if nameLabelErr != "" {
		options["name_label_set_error"] = nameLabelErr
	}
	if hasHomeServer && homeServerUUID != "" {
		options["home_server_uuid"] = homeServerUUID
		options["home_server_applied"] = homeServerApplied
		if homeServerErr != "" {
			// 후처리 실패는 restore job 자체는 성공 유지 + 원문 에러 포함(기존 name_label_set_error와 동일 패턴)
			options["home_server_set_error"] = homeServerErr
		}
	}
	if len(refs) > 1 {
		options["additional_vm_refs_ignored"] = len(refs) - 1
	}
	if isZstdRestore {
		// 🆕 2026-08-21: file_size_bytes는 기존 계약 유지(원본 파일=.zst 크기) — 압축해제 후 실제
		// 업로드된 크기는 별도 필드로 투명하게 노출
		options["compression_detected"] = "zstd"
		options["decompressed_size_bytes"] = restoreSize
	}

	e.sendSuccess(ctx, req, "VM", newVMUUID, i18n.T("vm_backup.m038", fullPath), options)
}

// ─────────────────────────────────────────────────────────────────
// 내부 헬퍼
// ─────────────────────────────────────────────────────────────────

// countingWriter는 쓰여진 바이트 수를 원자적으로 집계합니다 (진행률 로그용).
