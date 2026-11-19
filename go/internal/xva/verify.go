// copied from ncc-xen backend/internal/job/vm_backup_verify.go runVerify @043c6d5 (CP-VM-07), 2026-11-19.
// 바뀐 부분(docs/vm/08-copied-code-register.md CP-VM-07):
//   - 경로 해석과 결과 보고는 vm 패키지로 옮기고, 이 함수는 "열린 파일 검증"만 한다.
//   - 실패 사유 문장을 i18n 키 대신 고정 영어 문장으로 쓴다(사유는 기계가 읽는 진단이라 번역하지 않는다).
//   - 규약 2 processing 송신 대신 Progress 콜백, ctx 취소를 읽기마다 본다(새로 더함).
//   - 검증 판정 규칙(파일 열기 실패만 오류, 손상은 Verified=false)은 그대로다.

package xva

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha1" //nolint:gosec // XVA 내부 .checksum 형식이 SHA1 이다
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"
)

var xvaChunkNamePattern = regexp.MustCompile(`^Ref:\d+/\d+$`)

// maxReasons 는 실패 사유를 이만큼 모으면 검사를 멈추는 수다(ncc-xen 과 같다).
const maxReasons = 10

// VerifyResult 는 검증 결과다(05 문서 verify 결과 필드).
type VerifyResult struct {
	Verified            bool
	ChunkCount          int
	ChecksumEntries     int
	CompressionDetected string
	SHA256              string // 끝까지 읽었고 통과했을 때만
	Size                int64
	Reasons             []string
}

// Verify 는 XVA 파일의 압축 형식을 알아내고 tar 구조, ova.xml, 청크 SHA1 과 .checksum 을 대조한다.
// 파일을 열 수 없을 때만 오류다. 손상은 Verified=false 와 Reasons 로 알린다.
func Verify(ctx context.Context, path string, interval time.Duration, progress Progress) (VerifyResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return VerifyResult{}, &Error{Code: CodeFileIO, Err: err}
	}
	defer f.Close()
	res := VerifyResult{CompressionDetected: CompressionNone}
	if st, err := f.Stat(); err == nil {
		res.Size = st.Size()
	}
	cr := &countingReader{r: ctxReader{ctx: ctx, r: f}}
	hasher := sha256.New()
	br := bufio.NewReaderSize(io.TeeReader(cr, hasher), 1<<20)
	stop := pollRead(ctx, interval, res.Size, func() int64 { return atomic.LoadInt64(&cr.n) }, progress)
	defer stop()

	var src io.Reader = br
	if magic, err := br.Peek(4); err == nil && len(magic) == 4 &&
		magic[0] == 0x28 && magic[1] == 0xB5 && magic[2] == 0x2F && magic[3] == 0xFD {
		zr, zErr := zstd.NewReader(br)
		if zErr != nil {
			res.CompressionDetected = CompressionZstd
			res.Reasons = []string{"zstd reader failed: " + zErr.Error()}
			return res, nil
		}
		defer zr.Close()
		src = zr
		res.CompressionDetected = CompressionZstd
	} else if magic, err := br.Peek(2); err == nil && len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, gErr := gzip.NewReader(br)
		if gErr != nil {
			res.CompressionDetected = CompressionGzip
			res.Reasons = []string{"gzip reader failed: " + gErr.Error()}
			return res, nil
		}
		defer gz.Close()
		src = gz
		res.CompressionDetected = CompressionGzip
	}

	fullyRead := scanTar(src, &res)
	if err := ctx.Err(); err != nil {
		return VerifyResult{}, ctxError(ctx)
	}
	res.Verified = len(res.Reasons) == 0
	if res.Verified && fullyRead {
		if _, err := io.Copy(io.Discard, br); err == nil {
			res.SHA256 = hex.EncodeToString(hasher.Sum(nil))
		}
	}
	return res, nil
}

// scanTar 는 tar 항목을 돌며 청크 SHA1 을 .checksum 과 대조한다. 끝까지 읽었으면 참이다.
func scanTar(src io.Reader, res *VerifyResult) bool {
	tr := tar.NewReader(src)
	chunkSums := map[string]string{}
	ovaFound := false
	fullyRead := true
	fail := func(name string, err error) {
		res.Reasons = append(res.Reasons, "read failed ["+name+"]: "+err.Error())
		fullyRead = false
	}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			fail("", err)
			break
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		switch {
		case name == "ova.xml":
			ovaFound = true
			if _, err := io.Copy(io.Discard, tr); err != nil {
				fail(name, err)
			}
		case strings.HasSuffix(name, ".checksum"):
			res.ChecksumEntries++
			buf, err := io.ReadAll(io.LimitReader(tr, 256))
			if err != nil {
				fail(name, err)
				continue
			}
			want := strings.TrimSpace(string(buf))
			base := strings.TrimSuffix(name, ".checksum")
			got, ok := chunkSums[base]
			if !ok || !strings.EqualFold(got, want) {
				res.Reasons = append(res.Reasons, "checksum mismatch ["+base+"] want="+want+" got="+got)
			}
			delete(chunkSums, base)
		case xvaChunkNamePattern.MatchString(name):
			res.ChunkCount++
			h := sha1.New() //nolint:gosec // XVA 형식
			if _, err := io.Copy(h, tr); err != nil {
				fail(name, err)
				continue
			}
			chunkSums[name] = hex.EncodeToString(h.Sum(nil))
		default:
			if _, err := io.Copy(io.Discard, tr); err != nil {
				fail(name, err)
			}
		}
		if len(res.Reasons) >= maxReasons {
			return false
		}
	}
	if !ovaFound {
		res.Reasons = append(res.Reasons, "ova.xml not found")
	}
	return fullyRead
}

// pollRead 는 interval 마다 읽은 바이트 / 전체 크기로 진행률을 알린다.
func pollRead(ctx context.Context, interval time.Duration, total int64, read func() int64, report Progress) func() {
	if report == nil || total <= 0 {
		return func() {}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
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
				n := read()
				report(percentOf(float64(n)/float64(total)), n)
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}
