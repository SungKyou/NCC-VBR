//go:build !linux && !windows

package fsx

import "io/fs"

// TimesOf 는 지원 대상이 아닌 OS 에서 수정 시각만으로 판단하게 한다.
func TimesOf(info fs.FileInfo) Times {
	modified := info.ModTime().Unix()
	return Times{Access: modified, Modify: modified, Change: modified}
}
