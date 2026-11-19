package cifs

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ncc-vbr/internal/fsx"
)

const (
	// copyBufferSize 는 원본 CIFS_BUFFER 다.
	copyBufferSize = 640000
	// gzipLevel 은 원본 gzsetparams(zfp, 5, ...) 다.
	gzipLevel = 5
	// disabledPrefix 는 사용 중지된 폴더에 붙는 접두어다.
	disabledPrefix = "dis_"
)

var copyBuffers = sync.Pool{
	New: func() any {
		buffer := make([]byte, copyBufferSize)
		return &buffer
	},
}

// copier 는 순회 엔진이 부르는 백업 동작이다(walk.Visitor).
type copier struct {
	source   string
	target   string
	baseTime int64
	log      Logger
	now      func() time.Time
}

// File 은 원본 file_check 다. 파일 하나를 비교해 필요하면 복사한다.
func (c *copier) File(source, rel string) (bool, error) {
	target := filepath.Join(c.target, rel)

	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return false, fmt.Errorf("lstat source: %w", err)
	}
	targetInfo, err := os.Lstat(target)
	if errors.Is(err, fs.ErrNotExist) {
		// 대상에 없으면 원본 시각과 관계없이 새로 복사한다.
		return true, copyFile(source, target)
	}
	if err != nil {
		return false, fmt.Errorf("lstat target: %w", err)
	}

	// 기준 시각 0은 첫 실행 또는 전체 비교일이므로 시각 필터를 생략한다.
	if c.baseTime != 0 && fsx.TimesOf(sourceInfo).NotAfter(c.baseTime) {
		return false, nil
	}
	if !changed(sourceInfo, targetInfo) {
		return false, nil
	}

	keepErr := c.keepOldVersion(target)
	if err := copyFile(source, target); err != nil {
		return false, err
	}
	// 이전 버전 보관에 실패해도 원본과 같이 새 내용은 복사한다. 실패는 오류로 집계한다.
	return true, keepErr
}

func changed(source, target fs.FileInfo) bool {
	if source.Size() != target.Size() {
		return true
	}
	return source.ModTime().Unix() > target.ModTime().Unix()
}

// keepOldVersion 은 대상 파일의 현재 내용을 gzip 으로 압축해 옆에 남긴다.
func (c *copier) keepOldVersion(target string) error {
	backup := target + "-vbr-" + c.now().Format("20060102150405") + ".zip"
	if err := gzipFile(target, backup); err != nil {
		if removeErr := os.Remove(backup); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			c.log.Printf("FILE COPY : backup temp file remove internal error[%s][%v]!!!\n", backup, removeErr)
		}
		c.log.Printf("FILE COPY : backup file compress internal error[%s][%s][%v]!!!\n", target, backup, err)
		return fmt.Errorf("keep old version: %w", err)
	}
	return nil
}

func gzipFile(source, target string) (err error) {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return err
	}
	defer closeKeepingError(out, &err)

	zip, err := gzip.NewWriterLevel(out, gzipLevel)
	if err != nil {
		return err
	}
	if err := copyStream(zip, in); err != nil {
		return err
	}
	return zip.Close()
}

// copyFile 은 원본 file_copy 다. 대상을 같은 이름으로 덮어쓴다(시각·권한은 옮기지 않는다).
// 원본과 달리 쓰기·닫기 오류를 확인한다.
func copyFile(source, target string) (err error) {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("source open: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return fmt.Errorf("target open: %w", err)
	}
	defer closeKeepingError(out, &err)

	if err := copyStream(out, in); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	return nil
}

// copyStream 은 정해진 크기의 버퍼로 읽고 쓴다.
// 커널의 파일 간 직접 복사 기능은 네트워크 파일 시스템에서 동작이 제각각이라 쓰지 않는다.
func copyStream(dst io.Writer, src io.Reader) error {
	buffer := copyBuffers.Get().(*[]byte)
	defer copyBuffers.Put(buffer)
	_, err := io.CopyBuffer(struct{ io.Writer }{dst}, struct{ io.Reader }{src}, *buffer)
	return err
}

func closeKeepingError(closer io.Closer, err *error) {
	if closeErr := closer.Close(); closeErr != nil && *err == nil {
		*err = closeErr
	}
}

// EnterDir 은 원본 dir_check 다. 대상 디렉터리가 없으면 만든다.
func (c *copier) EnterDir(_, rel string) (bool, error) {
	target := filepath.Join(c.target, rel)
	_, err := os.Lstat(target)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("lstat target dir: %w", err)
	}
	if err := os.Mkdir(target, dirMode); err != nil {
		return false, fmt.Errorf("create target dir: %w", err)
	}
	return true, nil
}

// TopDir 은 원본 prepare_dis_target_dir 다.
//
// 원본에서 폴더 "X" 가 "dis_X" 로 이름이 바뀌면, 대상의 "X" 도 "dis_X" 로 바꿔 이어서 백업한다.
// 그렇게 하지 않으면 "dis_X" 전체를 새로 복사하게 된다.
func (c *copier) TopDir(name string) {
	if !strings.HasPrefix(name, disabledPrefix) || len(name) <= len(disabledPrefix) {
		return
	}
	oldName := name[len(disabledPrefix):]
	oldSource := filepath.Join(c.source, oldName)
	if _, err := os.Lstat(oldSource); !errors.Is(err, fs.ErrNotExist) {
		if err == nil {
			c.log.Printf("dis_ original source path still exists. Rename skip.[%s].\n", oldSource)
		} else {
			c.log.Printf("dis_ original source path check failed. Rename skip.[%s][%v].\n", oldSource, err)
		}
		return
	}

	oldDir := filepath.Join(c.target, oldName)
	newDir := filepath.Join(c.target, name)
	if err := c.renameDisabled(oldDir, newDir); err != nil {
		c.log.Printf("dis_ target preparation failed. Continue normal backup.[%s][%v]\n", newDir, err)
	}
}

func (c *copier) renameDisabled(oldDir, newDir string) error {
	oldInfo, err := os.Lstat(oldDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !oldInfo.IsDir() {
		c.log.Printf("dis_ source directory target is not directory. Rename skip.[%s].\n", oldDir)
		return nil
	}

	proceed, err := c.clearEmptyTarget(oldDir, newDir)
	if err != nil || !proceed {
		return err
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		return err
	}
	c.log.Printf("dis_ target directory rename success.[%s]=>[%s].\n", oldDir, newDir)
	return nil
}

// clearEmptyTarget 은 바꿀 이름의 자리를 확인한다. 비어 있는 디렉터리면 지우고, 내용이 있으면 그대로 둔다.
func (c *copier) clearEmptyTarget(oldDir, newDir string) (proceed bool, err error) {
	newInfo, err := os.Lstat(newDir)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !newInfo.IsDir() {
		c.log.Printf("dis_ target path already exists and is not directory. Rename skip.[%s].\n", newDir)
		return false, nil
	}
	empty, err := isEmptyDir(newDir)
	if err != nil {
		return false, err
	}
	if !empty {
		c.log.Printf("dis_ target directory already contains data. Rename skip.[%s]=>[%s].\n", oldDir, newDir)
		return false, nil
	}
	return true, os.Remove(newDir)
}

func isEmptyDir(dir string) (bool, error) {
	fp, err := os.Open(dir)
	if err != nil {
		return false, err
	}
	defer fp.Close()

	_, err = fp.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	return false, err
}
