// 출처: ncc-xen stdcrypto/secret/errors.go (커밋 043c6d5, 2026-11-17 복사)
// 바뀐 부분: docs/agent/08-copied-code-register.md CP-10

package secret

import "errors"

var (
	ErrInvalidEnvelope      = errors.New("invalid credential envelope")
	ErrUnsupportedVersion   = errors.New("unsupported credential envelope version")
	ErrUnsupportedAlgorithm = errors.New("unsupported credential algorithm")
	ErrInvalidKeyring       = errors.New("invalid credential keyring")
	ErrInvalidKey           = errors.New("invalid credential key")
	ErrKeyNotFound          = errors.New("credential key not found")
	ErrAuthenticationFailed = errors.New("credential authentication failed")
)
