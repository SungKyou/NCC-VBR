// copied from ncc-xen backend/internal/job/vm_backup_compress.go decompressZstdIfNeeded @043c6d5 (CP-VM-08), 2026-11-19.
// 바뀐 부분(docs/vm/08-copied-code-register.md CP-VM-08):
//   - 쓰지 않는 2-pass 압축 함수(compressFileZstd)는 가져오지 않았다(ncc-xen 2026-10-16 이후 미사용).
//   - 임시 파일 이름 접두를 ".ncc-zstd-restore-" 에서 ".vbr-vm-zstd-" 로 바꿨다.
//   - 해제 중 ctx 취소를 본다(새로 더함). 취소되면 임시 파일을 지운다.

package xva

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/klauspost/compress/zstd"
)

var zstdMagic = []byte{0x28, 0xB5, 0x2F, 0xFD}

// ctxReader 는 읽을 때마다 ctx 를 확인한다.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// DecompressIfZstd 는 파일이 zstd 면 같은 디렉터리에 임시 파일로 전부 풀고 (임시 경로, 푼 크기, 참)을 돌려준다.
// zstd 가 아니면 ("", 0, 거짓)이다. XAPI /import 는 zstd 를 모르기 때문이다(gzip 은 XAPI 가 안다).
// read 가 nil 이 아니면 원본에서 읽은 바이트를 누적한다(진행률용).
func DecompressIfZstd(ctx context.Context, path string, read *int64) (string, int64, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, false, &Error{Code: CodeFileIO, Err: err}
	}
	defer f.Close()
	magic := make([]byte, 4)
	n, rErr := io.ReadFull(f, magic)
	if rErr != nil && rErr != io.ErrUnexpectedEOF && rErr != io.EOF {
		return "", 0, false, &Error{Code: CodeFileIO, Err: rErr}
	}
	if n < 4 || !bytes.Equal(magic, zstdMagic) {
		return "", 0, false, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", 0, false, &Error{Code: CodeFileIO, Err: err}
	}
	var src io.Reader = ctxReader{ctx: ctx, r: f}
	if read != nil {
		src = &progressReader{r: src, n: read}
	}
	dec, err := zstd.NewReader(src)
	if err != nil {
		return "", 0, false, &Error{Code: CodeFileIO, Err: err}
	}
	defer dec.Close()
	tmp, err := os.CreateTemp(filepath.Dir(path), ".vbr-vm-zstd-*.xva")
	if err != nil {
		return "", 0, false, &Error{Code: CodeFileIO, Err: err}
	}
	tmpPath := tmp.Name()
	size, err := io.Copy(tmp, dec)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmpPath)
		if e := ctxError(ctx); e != nil {
			return "", 0, false, e
		}
		return "", 0, false, &Error{Code: CodeFileIO, Err: errorf("zstd decompress failed: %v", err)}
	}
	return tmpPath, size, true, nil
}

type progressReader struct {
	r io.Reader
	n *int64
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	atomic.AddInt64(p.n, int64(n))
	return n, err
}
