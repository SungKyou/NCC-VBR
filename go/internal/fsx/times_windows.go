//go:build windows

package fsx

import (
	"io/fs"
	"syscall"
	"time"
)

// TimesOf 는 Windows 파일 속성에서 시각(초)을 꺼낸다.
//
// Windows 에는 Linux 의 ctime(상태 변경 시각)에 해당하는 값을 표준 API 로 얻을 수 없어
// 생성 시각을 대신 쓴다. 새로 만들거나 복사해 넣은 파일을 변경분으로 잡는 용도로는 충분하다.
func TimesOf(info fs.FileInfo) Times {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		modified := info.ModTime().Unix()
		return Times{Access: modified, Modify: modified, Change: modified}
	}
	return Times{
		Access: time.Unix(0, data.LastAccessTime.Nanoseconds()).Unix(),
		Modify: time.Unix(0, data.LastWriteTime.Nanoseconds()).Unix(),
		Change: time.Unix(0, data.CreationTime.Nanoseconds()).Unix(),
	}
}
