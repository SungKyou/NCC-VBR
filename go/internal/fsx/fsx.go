// Package fsx 는 OS 마다 다른 파일 시스템 처리를 한곳에 모은다.
package fsx

import (
	"path/filepath"
	"runtime"
	"strings"
)

// Times 는 파일의 세 가지 시각(Unix 초)이다. 원본 struct stat 의 st_atime/st_mtime/st_ctime 에 대응한다.
type Times struct {
	Access int64
	Modify int64
	Change int64
}

// NotAfter 는 세 시각이 모두 기준 시각 이하인지 알려준다.
func (t Times) NotAfter(base int64) bool {
	return t.Access <= base && t.Modify <= base && t.Change <= base
}

// SamePath 는 두 경로가 같은 위치를 가리키는지 문자열 기준으로 비교한다.
// 끝의 구분자와 중복 구분자는 무시하고, Windows 에서는 대소문자를 구분하지 않는다.
func SamePath(a, b string) bool {
	left := filepath.Clean(a)
	right := filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

// FlattenPath 는 경로를 파일명에 쓸 수 있게 바꾼다.
// 원본 규칙('/' 를 '_' 로 바꾸고 첫 글자를 버린다)을 따르고, Windows 의 '\' 와 ':' 도 '_' 로 바꾼다.
func FlattenPath(path string) string {
	flat := strings.ReplaceAll(path, "/", "_")
	if runtime.GOOS == "windows" {
		flat = strings.NewReplacer("\\", "_", ":", "_").Replace(flat)
	}
	if flat == "" {
		return flat
	}
	return flat[1:]
}
