// Package daemon 은 원본 BS_Daemonize(fork + setsid + umask(0))를 대신한다.
//
// Go 프로세스는 fork 로 자신을 복제할 수 없으므로, 같은 실행 파일을 터미널에서 분리된
// 자식으로 다시 띄우고 부모는 종료 코드 0 으로 끝낸다. 호출자(스케줄러·관리 데몬)가 보는
// 동작은 원본과 같다. 명령을 실행하면 바로 돌아오고 작업은 백그라운드에서 계속된다.
package daemon

import (
	"fmt"
	"os"
	"os/exec"
)

// childMarker 는 분리된 자식에게 다시 분리하지 말라고 알리는 환경 변수다.
const childMarker = "NCC_VBR_DAEMON_CHILD"

// IsChild 는 현재 프로세스가 이미 분리된 자식인지 알려준다.
func IsChild() bool {
	return os.Getenv(childMarker) == "1"
}

// Detach 는 현재 프로세스가 분리된 자식이 아니면 자식을 띄우고 부모를 종료한다.
// 자식에서 호출하면 실행 환경(세션, umask)만 맞추고 돌아온다.
func Detach() error {
	if IsChild() {
		prepareChild()
		return nil
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("daemonize: executable path: %w", err)
	}
	child := exec.Command(self, os.Args[1:]...)
	child.Env = append(os.Environ(), childMarker+"=1")
	child.SysProcAttr = detachedAttr()
	if err := child.Start(); err != nil {
		return fmt.Errorf("daemonize: start child: %w", err)
	}
	if err := child.Process.Release(); err != nil {
		return fmt.Errorf("daemonize: release child: %w", err)
	}
	os.Exit(0)
	return nil
}
