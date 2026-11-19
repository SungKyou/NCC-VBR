//go:build linux

package fsx

import (
	"io/fs"
	"syscall"
)

// TimesOf 는 lstat 결과에서 접근·수정·상태변경 시각(초)을 꺼낸다.
func TimesOf(info fs.FileInfo) Times {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		modified := info.ModTime().Unix()
		return Times{Access: modified, Modify: modified, Change: modified}
	}
	return Times{
		Access: int64(stat.Atim.Sec),
		Modify: int64(stat.Mtim.Sec),
		Change: int64(stat.Ctim.Sec),
	}
}
