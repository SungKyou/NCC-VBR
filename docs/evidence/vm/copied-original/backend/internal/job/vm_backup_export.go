package job

// vm_backup_export.go — VM_BACKUP action=export (XVA)
// 🆕 2026-09-01: vm_backup.go에서 기계적으로 분리됨 (로직 변경 없음).
// 원본 설계 주석은 vm_backup.go 상단 참고.

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/ncc/xen-backend/internal/i18n"
	"github.com/ncc/xen-backend/pkg/xapi/mixins"
)

func (e *VMBackupExecutor) runExport(ctx context.Context, req Request) {
	vmUUID := req.Payload.TargetUUID
	if vmUUID == "" {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m004"))
		return
	}

	// ── format 분기 (🆕 v3.15 — OVF/OVA; ovf 낱개 파일 지원 추가, 2026-08-13) ──
	// format=ova/ovf는 XVA와 완전히 다른 파이프라인(VDI별 export_raw_vdi + 직접 조립)이라
	// 별도 함수로 분리한다. 기존 XVA 경로(아래)는 무변경.
	format, _ := strOpt(req.Payload.Options, "format")
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "ova" || format == "ovf" {
		e.runExportOVA(ctx, req)
		return
	}
	if format != "" && format != "xva" {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m043", format))
		return
	}

	// ── 옵션 파싱 ────────────────────────────────────────────
	fileName, _ := strOpt(req.Payload.Options, "file_name")
	exportPath, _ := strOpt(req.Payload.Options, "export_path")
	compression, _ := strOpt(req.Payload.Options, "compression")
	compression = strings.ToLower(strings.TrimSpace(compression))
	if compression == "" {
		compression = "none"
	}
	if compression != "none" && compression != "gzip" && compression != "zstd" { // 🆕 2026-08-21: zstd 추가
		e.sendFailure(ctx, req, i18n.T("vm_backup.m011", compression))
		return
	}
	keepSnapshot := boolOpt(req.Payload.Options, "keep_snapshot", false)
	overwrite := boolOpt(req.Payload.Options, "overwrite", false)

	// ── 경로 해석 및 검증 ────────────────────────────────────
	if fileName == "" {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m006"))
		return
	}
	if !isValidBackupFileName(fileName) {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m007", fileName))
		return
	}
	if !strings.HasSuffix(strings.ToLower(fileName), ".xva") {
		fileName += ".xva"
	}
	outDir := e.settings.BasePath
	if exportPath != "" {
		cleaned := filepath.Clean(exportPath)
		if containsParentRef(cleaned) {
			e.sendFailure(ctx, req, i18n.T("vm_backup.m008", exportPath))
			return
		}
		resolved, ok := resolvePathWithSandbox(e.settings, cleaned)
		if !ok {
			e.sendFailure(ctx, req, i18n.T("vm_backup.m106", exportPath))
			return
		}
		outDir = resolved
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m010", err))
		return
	}
	finalPath := filepath.Join(outDir, fileName)
	if _, err := os.Stat(finalPath); err == nil && !overwrite {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m009", finalPath))
		return
	}
	// 🆕 2026-08-21: zstd 압축 시 최종 산출물은 finalPath+".zst" — 별도 충돌 확인
	// (finalPath 자체는 다운로드 중간 스테이징 파일로만 쓰이고 압축 후 삭제됨 — 아래 참고)
	outputPath := finalPath
	if compression == "zstd" {
		outputPath = finalPath + ".zst"
		if _, err := os.Stat(outputPath); err == nil && !overwrite {
			e.sendFailure(ctx, req, i18n.T("vm_backup.m009", outputPath))
			return
		}
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

	// ── VM 상태 확인 및 export 대상 결정 ─────────────────────
	vmRef, err := vmClient.GetByUUID(ctx, vmUUID)
	if err != nil {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m005", err))
		return
	}
	record, err := vmClient.GetRecord(ctx, vmRef)
	if err != nil {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m005", err))
		return
	}
	powerState := ""
	if ps, ok := record["power_state"].(string); ok {
		powerState = strings.ToLower(ps)
	}
	isSnapshot := xapiBoolField(record["is_a_snapshot"])
	vmName, _ := record["name_label"].(string)

	exportUUID := vmUUID
	snapshotUsed := false
	tempSnapRef := ""
	tempSnapUUID := ""

	switch {
	case isSnapshot:
		// 스냅샷은 상태 무관 그대로 export 가능
	case powerState == "halted":
		// 그대로 export
	case powerState == "suspended":
		// Disk 기준 export. suspend 메모리(suspend_VDI_uuid)는 일반 VBD로 VM에
		// 연결되어 있지 않은 별도 필드이므로 /export가 VBD 체인을 순회할 때 자동으로
		// 제외된다 — 추가 파라미터 없이 halted와 동일 경로로 처리 가능 (2026-07-22 확정).
		// memory 포함 옵션(include_memory)은 XAPI 파라미터 확인 후 별도 지원 예정(17.4).
	case powerState == "running":
		// 임시 스냅샷 생성 (disk-only, crash-consistent — quiesced는 XS 8.x에서 제거됨)
		snapName := fmt.Sprintf("NCC-BACKUP-%s-%s", vmName, time.Now().Format("20060102150405"))
		snapRef, snapErr := vmClient.Snapshot(ctx, vmUUID, snapName)
		if snapErr != nil {
			e.sendFailure(ctx, req, i18n.T("vm_backup.m014", snapErr))
			return
		}
		snapUUID, uErr := vmClient.GetUUIDByRef(ctx, snapRef)
		if uErr != nil {
			// UUID 조회 실패 시 만들어진 스냅샷을 즉시 정리하고 실패 보고
			_ = vmClient.DestroyByRef(ctx, snapRef)
			e.sendFailure(ctx, req, i18n.T("vm_backup.m014", uErr))
			return
		}
		tempSnapRef, tempSnapUUID = snapRef, snapUUID
		exportUUID = snapUUID
		snapshotUsed = true
		e.log.Info(i18n.T("vm_backup.l002"),
			"taskId", req.TaskID, "snapshotUUID", snapUUID, "snapshotName", snapName)
	default:
		e.sendFailure(ctx, req, i18n.T("vm_backup.m013", powerState))
		return
	}

	// ── 임시 스냅샷 정리 (성공/실패 무관 항상 시도, keep_snapshot=true면 보존) ──
	snapshotCleanupErr := ""
	cleanupSnapshot := func() {
		if tempSnapRef == "" || keepSnapshot {
			return
		}
		cctx, ccancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer ccancel()
		if derr := vmClient.DestroyByRef(cctx, tempSnapRef); derr != nil {
			snapshotCleanupErr = derr.Error()
			e.log.Warn(i18n.T("vm_backup.l007"),
				"taskId", req.TaskID, "snapshotUUID", tempSnapUUID, "error", derr)
		} else {
			e.log.Info(i18n.T("vm_backup.l006"), "taskId", req.TaskID, "snapshotUUID", tempSnapUUID)
		}
		tempSnapRef = "" // 재시도 방지
	}
	fail := func(msg string) {
		cleanupSnapshot()
		e.sendFailure(ctx, req, msg)
	}

	// ── XAPI Task 생성 (진행률 추적용) ────────────────────────
	taskRef, err := vmClient.TaskCreate(ctx, "NCC-Xen VM Export", "VM_BACKUP export taskId="+req.TaskID)
	if err != nil {
		fail(i18n.T("vm_backup.m015", err))
		return
	}
	defer func() {
		dctx, dcancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dcancel()
		if derr := vmClient.TaskDestroy(dctx, taskRef); derr != nil {
			e.log.Warn(i18n.T("vm_backup.l010"), "taskId", req.TaskID, "error", derr)
		}
	}()

	// ── export URL 구성 ───────────────────────────────────────
	q := url.Values{}
	q.Set("session_id", client.SessionRef())
	q.Set("uuid", exportUUID)
	q.Set("task_id", taskRef)
	if compression == "gzip" {
		q.Set("use_compression", "true")
	}
	exportURL := fmt.Sprintf("https://%s/export?%s", session.masterIP, q.Encode())

	e.log.Info(i18n.T("vm_backup.l003"),
		"taskId", req.TaskID,
		"exportUUID", exportUUID,
		"snapshotUsed", snapshotUsed,
		"filePath", outputPath,
		"compression", compression,
	)

	// ── HTTP 스트리밍 다운로드 ────────────────────────────────
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: !session.verifySSL},
		},
		// 전체 Timeout은 두지 않음 — ctx(시간 단위)가 수명 관리
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, exportURL, nil)
	if err != nil {
		fail(i18n.T("vm_backup.m016", err))
		return
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		fail(i18n.T("vm_backup.m016", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		fail(i18n.T("vm_backup.m017", resp.StatusCode, strings.TrimSpace(string(body))))
		return
	}

	// 진행률 폴링 goroutine (vm_control copy와 동일 패턴: 1~99%)
	counter := &countingWriter{}
	stopProgress := make(chan struct{})
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
					continue
				}
				percent := int(progress*98) + 1
				if percent < 1 {
					percent = 1
				}
				if percent > 99 {
					percent = 99
				}
				e.log.Info(i18n.T("vm_backup.l004"),
					"taskId", req.TaskID, "percent", percent,
					"bytesWritten", atomic.LoadInt64(&counter.n))
				_ = e.gw.SendProcessing(ctx,
					req.TaskID, "VM_BACKUP", req.Action, "VM", vmUUID,
					i18n.T("vm_backup.m022", percent), percent,
					map[string]interface{}{
						"step":      "exporting",
						"file_path": outputPath,
					})
			}
		}
	}()

	partial := outputPath + ".partial"
	start := time.Now()
	outFile, err := os.Create(partial)
	if err != nil {
		close(stopProgress)
		fail(i18n.T("vm_backup.m018", err))
		return
	}
	hasher := sha256.New()

	// 🆕 2026-10-16: zstd는 다운로드와 동시에 스트리밍으로 압축한다 (export 완료 후
	// 비압축 XVA 전체를 다시 읽어서 압축하던 기존 2-pass 구조 제거 — 디스크 쓰기/읽기가
	// 각각 1회로 줄고, 비압축 원본을 임시로 보관할 피크 디스크 공간도 필요 없어짐).
	// gzip/none 경로는 기존과 완전히 동일(바로 io.MultiWriter로 기록), 무변경.
	var copyErr error
	if compression == "zstd" {
		enc, encErr := zstd.NewWriter(io.MultiWriter(outFile, hasher))
		if encErr != nil {
			_ = outFile.Close()
			close(stopProgress)
			e.removePartial(req, partial)
			fail(i18n.T("vm_backup.m102", encErr))
			return
		}
		_, copyErr = io.Copy(io.MultiWriter(enc, counter), resp.Body)
		if encCloseErr := enc.Close(); encCloseErr != nil && copyErr == nil {
			copyErr = encCloseErr
		}
	} else {
		_, copyErr = io.Copy(io.MultiWriter(outFile, hasher, counter), resp.Body)
	}
	closeErr := outFile.Close()
	close(stopProgress)

	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		e.removePartial(req, partial)
		if errors.Is(copyErr, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			fail(i18n.T("vm_backup.m027", e.settings.TimeoutHours))
			return
		}
		fail(i18n.T("vm_backup.m018", copyErr))
		return
	}

	// ── 스트림 종료 ≠ 성공 보장 — Task 최종 상태로 판정 (중간 절단 감지) ──
	finalStatus := ""
	for i := 0; i < 12; i++ {
		st, serr := vmClient.TaskGetStatus(ctx, taskRef)
		if serr != nil {
			break // task 조회 불가 시 스트림 완료를 신뢰
		}
		finalStatus = st
		if st != "pending" && st != "cancelling" {
			break
		}
		time.Sleep(time.Second)
	}
	if finalStatus == "failure" || finalStatus == "cancelled" {
		errInfo, _ := vmClient.TaskGetErrorInfo(ctx, taskRef)
		e.removePartial(req, partial)
		fail(i18n.T("vm_backup.m019", errInfo))
		return
	}

	// ── .partial → 최종 파일 rename ───────────────────────────
	if overwrite {
		// Windows에서 rename은 기존 파일을 덮지 못하므로 선제 삭제
		_ = os.Remove(outputPath)
	}
	if err := os.Rename(partial, outputPath); err != nil {
		e.removePartial(req, partial)
		fail(i18n.T("vm_backup.m020", err))
		return
	}

	cleanupSnapshot()

	// 🆕 2026-10-16: zstd는 이제 다운로드와 동시에 압축되므로 counter(다운로드 바이트 수)는
	// 압축 전 원본 크기를 가리킨다 — 실제 산출물 크기는 완성된 파일을 stat해서 구한다.
	// stat 실패 시에만 counter로 폴백(비압축 경로에서는 어차피 둘이 동일한 값).
	fileSize := atomic.LoadInt64(&counter.n)
	if info, statErr := os.Stat(outputPath); statErr == nil {
		fileSize = info.Size()
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))
	durationSec := int(time.Since(start).Seconds())

	e.log.Info(i18n.T("vm_backup.l005"),
		"taskId", req.TaskID,
		"filePath", outputPath,
		"fileSizeBytes", fileSize,
		"sha256", checksum,
		"durationSec", durationSec,
	)

	options := map[string]interface{}{
		"file_path":        outputPath,
		"file_size_bytes":  fileSize,
		"checksum_sha256":  checksum,
		"format":           "xva",
		"compression":      compression,
		"exported_uuid":    exportUUID,
		"power_state":      powerState,
		"snapshot_used":    snapshotUsed,
		"duration_seconds": durationSec,
		"completedAt":      completedAt(),
	}
	if powerState == "suspended" {
		// suspend 메모리는 VBD 미연결이라 export에 자동 포함되지 않음 — disk 기준 백업임을 명시.
		// memory 포함 옵션(include_memory)은 17.4절 향후 계획, 이 필드는 그때도 그대로 사용 예정.
		options["memory_included"] = false
	}
	if keepSnapshot && tempSnapUUID != "" {
		options["snapshot_uuid"] = tempSnapUUID
	}
	if snapshotCleanupErr != "" {
		// 정리 실패는 job 성공 유지 + Middleware가 후속 정리할 수 있도록 원문 에러 포함 (합의 사항)
		options["snapshot_cleanup_failed"] = tempSnapUUID
		options["snapshot_cleanup_error"] = snapshotCleanupErr
	}
	e.sendSuccess(ctx, req, "VM", vmUUID, i18n.T("vm_backup.m021", outputPath), options)
}

func (e *VMBackupExecutor) removePartial(req Request, partial string) {
	if rerr := os.Remove(partial); rerr != nil && !os.IsNotExist(rerr) {
		e.log.Warn(i18n.T("vm_backup.l011"), "taskId", req.TaskID, "path", partial, "error", rerr)
	}
}

// ─────────────────────────────────────────────────────────────────
// OVF/OVA export (3단계, 실기검증 전)
// ─────────────────────────────────────────────────────────────────

