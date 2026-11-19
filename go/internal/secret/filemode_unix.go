// 출처: ncc-xen stdcrypto/secret/filemode_unix.go (커밋 043c6d5, 2026-11-17 복사)
// 바뀐 부분: docs/agent/08-copied-code-register.md CP-10

//go:build unix

package secret

import (
	"fmt"
	"os"
)

func validateKeyringFileMode(mode os.FileMode) error {
	if mode.Perm()&0o077 != 0 {
		return fmt.Errorf("%w: keyring permissions are too broad", ErrInvalidKeyring)
	}
	return nil
}
