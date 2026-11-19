package job

// vm_backup_compress.go — zstd 압축/해제 헬퍼 (🆕 2026-08-21, VM_BACKUP 전용)
// 🆕 2026-09-01: vm_backup.go에서 기계적으로 분리됨 (로직 변경 없음).
//
// XAPI 네이티브 zstd(use_compression=zstd)는 XCP-ng/Xen Orchestra 전용 확장이며
// Citrix XenServer(8.2 CU1/9)엔 없음(2026-08-19 조사 결론). 따라서 XAPI 압축
// 옵션에 기대지 않고, 클라이언트(Go, klauspost/compress/zstd 순수 구현) 측에서
// 로컬로 압축·해제한다. export는 다운로드 완료 후 로컬 압축, restore/verify는
// 매직바이트(28 B5 2F FD) 자동 감지 후 임시파일로 압축 해제하는 방식.
//
// 다음 zstd 관련 작업은 이 파일만 보면 됩니다.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/klauspost/compress/zstd"
)

type countingWriter struct{ n int64 }

func (w *countingWriter) Write(p []byte) (int, error) {
	atomic.AddInt64(&w.n, int64(len(p)))
	return len(p), nil
}

// countingReader는 읽은 바이트 수를 원자적으로 집계합니다 (verify 진행률용).
type countingReader struct {
	r io.Reader
	n int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	atomic.AddInt64(&r.n, int64(n))
	return n, err
}

// ── zstd 압축/해제 헬퍼 (🆕 2026-08-21) ──────────────────────────────
// XAPI 네이티브 zstd(use_compression=zstd)는 XCP-ng/Xen Orchestra 전용 확장이며
// Citrix XenServer(현재 타겟 8.2 CU1 / 9)엔 없음 — 2026-08-19 조사 결론.
// 따라서 export/verify/restore의 기존 실기검증된 raw 다운로드·업로드 경로는 그대로 두고,
// 클라이언트(Go, klauspost/compress/zstd 순수 구현) 측에서 로컬로 압축·해제한다.

// zstdMagic은 zstd 프레임의 매직바이트입니다 (RFC 8878).
var zstdMagic = []byte{0x28, 0xB5, 0x2F, 0xFD}

// progressReader는 읽은 바이트 수를 외부 atomic 카운터에 누적합니다
// (zstd 압축/압축해제 진행률 보고용, 2026-08-21 신규 — countingReader와 달리 외부에서
// 공유하는 *int64를 갱신하므로 별도 goroutine이 진행 중에도 값을 폴링할 수 있습니다).
type progressReader struct {
	r        io.Reader
	progress *int64
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	if pr.progress != nil && n > 0 {
		atomic.AddInt64(pr.progress, int64(n))
	}
	return n, err
}

// compressFileZstd는 srcPath 파일을 압축레벨 기본값(SpeedDefault)으로 압축하여 dstPath에
// 저장하고, 압축된 파일의 크기와 SHA256을 단일 패스로 반환합니다 (export 완료 후 후처리 단계).
// progress가 non-nil이면 srcPath에서 읽은 바이트 수를 실시간으로 누적합니다 (진행률 보고용,
// 2026-08-21 추가 — 호출부에서 별도 goroutine으로 폴링).
// 실패 시 dstPath에 남은 부분 파일은 정리합니다. srcPath(압축 전 원본)는 이 함수 안에서
// 건드리지 않습니다 — 삭제는 호출부(runExport)의 책임입니다.
func compressFileZstd(srcPath, dstPath string, progress *int64) (size int64, checksum string, err error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return 0, "", err
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return 0, "", err
	}
	cleanup := func() {
		_ = dst.Close()
		_ = os.Remove(dstPath)
	}

	hasher := sha256.New()
	counter := &countingWriter{}
	enc, err := zstd.NewWriter(io.MultiWriter(dst, hasher, counter))
	if err != nil {
		cleanup()
		return 0, "", err
	}
	var srcReader io.Reader = src
	if progress != nil {
		srcReader = &progressReader{r: src, progress: progress}
	}
	if _, err := io.Copy(enc, srcReader); err != nil {
		_ = enc.Close()
		cleanup()
		return 0, "", err
	}
	if err := enc.Close(); err != nil {
		cleanup()
		return 0, "", err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(dstPath)
		return 0, "", err
	}
	return atomic.LoadInt64(&counter.n), hex.EncodeToString(hasher.Sum(nil)), nil
}

// decompressZstdIfNeeded는 fullPath 파일의 매직바이트를 확인해 zstd 압축 파일이면
// fullPath와 같은 디렉토리에 임시파일로 전체 압축해제하고 (임시경로, 해제된 크기, true, nil)을
// 반환합니다. zstd가 아니면 ("", 0, false, nil)을 반환합니다 (gzip은 XAPI가 서버측에서
// 자동 인식하므로 이 함수의 대상이 아님 — 기존 동작 그대로).
// progress가 non-nil이면 압축 해제를 위해 fullPath에서 읽은(=압축된 원본 기준) 바이트 수를
// 실시간으로 누적합니다 (진행률 보고용, 2026-08-21 추가 — 호출부에서 별도 goroutine으로 폴링).
// 스트리밍 해제(임시파일 없이 바로 업로드)가 아니라 임시파일 방식을 쓰는 이유: 압축해제 후
// 크기를 미리 알 수 없으면 restore 업로드의 실기검증된 Content-Length 설정 로직을 바꿔야
// 하므로, 그 대신 이 함수가 앞단에서 완결된 파일을 만들어 기존 업로드 경로를 무변경으로 유지한다.
func decompressZstdIfNeeded(fullPath string, progress *int64) (tempPath string, size int64, isZstd bool, err error) {
	f, err := os.Open(fullPath)
	if err != nil {
		return "", 0, false, err
	}
	defer f.Close()

	magic := make([]byte, 4)
	n, rErr := io.ReadFull(f, magic)
	if rErr != nil && rErr != io.ErrUnexpectedEOF && rErr != io.EOF {
		return "", 0, false, rErr
	}
	if n < 4 || !bytes.Equal(magic, zstdMagic) {
		return "", 0, false, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", 0, false, err
	}

	var fileReader io.Reader = f
	if progress != nil {
		fileReader = &progressReader{r: f, progress: progress}
	}
	dec, err := zstd.NewReader(fileReader)
	if err != nil {
		return "", 0, false, err
	}
	defer dec.Close()

	tmp, err := os.CreateTemp(filepath.Dir(fullPath), ".ncc-zstd-restore-*.xva")
	if err != nil {
		return "", 0, false, err
	}
	tmpPath := tmp.Name()

	counter := &countingWriter{}
	if _, err := io.Copy(io.MultiWriter(tmp, counter), dec); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", 0, false, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", 0, false, err
	}
	return tmpPath, atomic.LoadInt64(&counter.n), true, nil
}

// isValidBackupFileName은 파일명에 경로 구분자 및 OS 금지 문자가 없는지 확인합니다.
// (Windows 금지 문자 집합 기준 — 양쪽 OS 모두 안전)
