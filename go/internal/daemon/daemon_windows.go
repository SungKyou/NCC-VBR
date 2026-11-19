//go:build windows

package daemon

import "syscall"

const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

func detachedAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: detachedProcess | createNewProcessGroup,
	}
}

// prepareChild 는 Windows 에서는 할 일이 없다(umask 개념이 없다).
func prepareChild() {}
