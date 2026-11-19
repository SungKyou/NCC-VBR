// 출처: ncc-xen stdcrypto/secret/algorithm.go (커밋 043c6d5, 2026-11-17 복사)
// 바뀐 부분: docs/agent/08-copied-code-register.md CP-10

package secret

import (
	"crypto/cipher"
	"fmt"
)

const (
	AlgorithmAES256GCM = "aes-256-gcm"
	Version1           = "v1"
)

type algorithm interface {
	ID() string
	KeySize() int
	NewAEAD(key []byte) (cipher.AEAD, error)
}

var allowedAlgorithms = map[string]algorithm{
	AlgorithmAES256GCM: aes256GCMAlgorithm{},
}

func lookupAlgorithm(id string) (algorithm, error) {
	algorithm, ok := allowedAlgorithms[id]
	if !ok {
		return nil, fmt.Errorf("%w: algorithm is not allowed", ErrUnsupportedAlgorithm)
	}
	return algorithm, nil
}
