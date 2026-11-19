package job

// vm_backup_verify.go — VM_BACKUP action=verify (XVA 무결성 검증, gzip/zstd 압축 자동 감지)
// 🆕 2026-09-01: vm_backup.go에서 기계적으로 분리됨 (로직 변경 없음).
// 원본 설계 주석은 vm_backup.go 상단 참고.

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/ncc/xen-backend/internal/i18n"
)

var xvaChunkNamePattern = regexp.MustCompile(`^Ref:\d+/\d+$`)

func (e *VMBackupExecutor) runVerify(ctx context.Context, req Request) {
	filePathOpt, _ := strOpt(req.Payload.Options, "file_path")
	if filePathOpt == "" {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m023"))
		return
	}
	fullPath := filepath.Clean(filePathOpt)
	if containsParentRef(fullPath) {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m008", filePathOpt))
		return
	}
	if !filepath.IsAbs(fullPath) {
		fullPath = filepath.Join(e.settings.BasePath, fullPath)
	} else {
		if _, ok := resolvePathWithSandbox(e.settings, fullPath); !ok {
			e.sendFailure(ctx, req, i18n.T("vm_backup.m106", filePathOpt))
			return
		}
	}

	f, err := os.Open(fullPath)
	if err != nil {
		e.sendFailure(ctx, req, i18n.T("vm_backup.m024", err))
		return
	}
	defer f.Close()
	totalSize := int64(0)
	if st, sErr := f.Stat(); sErr == nil {
		totalSize = st.Size()
	}

	e.log.Info(i18n.T("vm_backup.l008"), "taskId", req.TaskID, "filePath", fullPath, "fileSizeBytes", totalSize)

	// 읽기 체인: file → countingReader(진행률) → TeeReader(SHA256) → bufio(매직바이트 Peek) → [gzip] → tar
	cr := &countingReader{r: f}
	hasher := sha256.New()
	br := bufio.NewReaderSize(io.TeeReader(cr, hasher), 1<<20)

	// 진행률 goroutine (읽은 바이트 / 파일 크기)
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
				if totalSize <= 0 {
					continue
				}
				read := atomic.LoadInt64(&cr.n)
				percent := int(float64(read)/float64(totalSize)*98) + 1
				if percent < 1 {
					percent = 1
				}
				if percent > 99 {
					percent = 99
				}
				_ = e.gw.SendProcessing(ctx,
					req.TaskID, "VM_BACKUP", req.Action, req.Payload.TargetType, req.Payload.TargetUUID,
					i18n.T("vm_backup.m028", percent), percent,
					map[string]interface{}{"step": "verifying", "file_path": fullPath})
			}
		}
	}()
	defer close(stopProgress)

	compressionDetected := "none"
	var src io.Reader = br
	// 🆕 2026-08-21: zstd 매직바이트(28 B5 2F FD, 4바이트) 자동 감지 추가.
	// gzip(2바이트 매직)과 서로 겹치지 않으므로 zstd를 먼저 확인 후 기존 gzip 분기로 폴백 — 기존 gzip 로직 자체는 무변경.
	if magic, pErr := br.Peek(4); pErr == nil && len(magic) == 4 &&
		magic[0] == 0x28 && magic[1] == 0xB5 && magic[2] == 0x2F && magic[3] == 0xFD {
		zr, zErr := zstd.NewReader(br)
		if zErr != nil {
			e.sendVerifyResult(ctx, req, fullPath, totalSize, "", "zstd", false, 0, 0,
				[]string{i18n.T("vm_backup.e005", zErr)})
			return
		}
		defer zr.Close()
		src = zr
		compressionDetected = "zstd"
	} else if magic, pErr := br.Peek(2); pErr == nil && len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, gErr := gzip.NewReader(br)
		if gErr != nil {
			e.sendVerifyResult(ctx, req, fullPath, totalSize, "", "gzip", false, 0, 0,
				[]string{i18n.T("vm_backup.e004", gErr)})
			return
		}
		defer gz.Close()
		src = gz
		compressionDetected = "gzip"
	}

	tr := tar.NewReader(src)
	chunkSums := map[string]string{} // 청크명 → SHA1 hex (직후의 .checksum 항목과 대조 후 삭제)
	reasons := []string{}
	ovaFound := false
	chunkCount := 0
	checksumEntries := 0
	fullyRead := true

	for {
		hdr, tErr := tr.Next()
		if tErr == io.EOF {
			break
		}
		if tErr != nil {
			reasons = append(reasons, i18n.T("vm_backup.e001", "", tErr))
			fullyRead = false
			break
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		switch {
		case name == "ova.xml":
			ovaFound = true
			if _, cErr := io.Copy(io.Discard, tr); cErr != nil {
				reasons = append(reasons, i18n.T("vm_backup.e001", name, cErr))
				fullyRead = false
			}
		case strings.HasSuffix(name, ".checksum"):
			checksumEntries++
			buf, rErr := io.ReadAll(io.LimitReader(tr, 256))
			if rErr != nil {
				reasons = append(reasons, i18n.T("vm_backup.e001", name, rErr))
				fullyRead = false
				continue
			}
			want := strings.TrimSpace(string(buf))
			base := strings.TrimSuffix(name, ".checksum")
			got, ok := chunkSums[base]
			if !ok || !strings.EqualFold(got, want) {
				reasons = append(reasons, i18n.T("vm_backup.e002", base, want, got))
			}
			delete(chunkSums, base)
		case xvaChunkNamePattern.MatchString(name):
			chunkCount++
			h := sha1.New()
			if _, cErr := io.Copy(h, tr); cErr != nil {
				reasons = append(reasons, i18n.T("vm_backup.e001", name, cErr))
				fullyRead = false
				continue
			}
			chunkSums[name] = hex.EncodeToString(h.Sum(nil))
		default:
			// 알 수 없는 항목은 건너뜀 (형식 확장 대비)
			if _, cErr := io.Copy(io.Discard, tr); cErr != nil {
				reasons = append(reasons, i18n.T("vm_backup.e001", name, cErr))
				fullyRead = false
			}
		}
		if len(reasons) >= 10 {
			// 사유가 과도하게 누적되면 조기 종료 (손상 파일)
			fullyRead = false
			break
		}
	}

	if !ovaFound {
		reasons = append(reasons, i18n.T("vm_backup.e003"))
	}
	if checksumEntries == 0 && ovaFound && len(reasons) == 0 {
		// .checksum 항목이 없는 XVA — tar 구조 검증만으로 판정 (설계상 폴백)
		e.log.Info(i18n.T("vm_backup.l012"), "taskId", req.TaskID, "filePath", fullPath)
	}

	verified := len(reasons) == 0

	// SHA256은 전체를 읽었을 때만 유효 (gzip 사용 시 잔여 바이트까지 소진)
	checksum := ""
	if verified && fullyRead {
		if _, dErr := io.Copy(io.Discard, br); dErr == nil {
			checksum = hex.EncodeToString(hasher.Sum(nil))
		}
	}

	e.sendVerifyResult(ctx, req, fullPath, totalSize, checksum, compressionDetected,
		verified, chunkCount, checksumEntries, reasons)
}

func (e *VMBackupExecutor) sendVerifyResult(ctx context.Context, req Request,
	fullPath string, totalSize int64, checksum, compressionDetected string,
	verified bool, chunkCount, checksumEntries int, reasons []string,
) {
	e.log.Info(i18n.T("vm_backup.l009"),
		"taskId", req.TaskID,
		"filePath", fullPath,
		"verified", verified,
		"chunkCount", chunkCount,
		"checksumEntries", checksumEntries,
	)
	options := map[string]interface{}{
		"file_path":            fullPath,
		"file_size_bytes":      totalSize,
		"verified":             verified,
		"chunk_count":          chunkCount,
		"checksum_entries":     checksumEntries,
		"compression_detected": compressionDetected,
		"format":               "xva",
		"completedAt":          completedAt(),
	}
	if checksum != "" {
		options["checksum_sha256"] = checksum
	}
	message := i18n.T("vm_backup.m025", fullPath)
	if !verified {
		options["failure_reasons"] = reasons
		message = i18n.T("vm_backup.m026", reasons[0])
	}
	// 검증 불일치도 verify 작업 자체는 success — verified 필드로 결과 전달 (합의 사항)
	e.sendSuccess(ctx, req, "FILE", req.Payload.TargetUUID, message, options)
}

// ─────────────────────────────────────────────────────────────────
// restore (XVA import, 2단계 신규 — 실기검증 전)
// ─────────────────────────────────────────────────────────────────

