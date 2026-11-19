//go:build unix

package daemon

import "syscall"

func detachedAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// prepareChild 는 원본의 umask(0) 을 재현한다. 생성 파일은 0666, 디렉터리는 0755 가 된다.
func prepareChild() {
	syscall.Umask(0)
}
