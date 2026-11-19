// 출처: ncc-xen stdcrypto/secret/filemode_other.go (커밋 043c6d5, 2026-11-17 복사)
// 바뀐 부분: docs/agent/08-copied-code-register.md CP-10

//go:build !unix

package secret

import "os"

func validateKeyringFileMode(os.FileMode) error { return nil }
