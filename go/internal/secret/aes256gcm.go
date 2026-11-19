// 출처: ncc-xen stdcrypto/secret/aes256gcm.go (커밋 043c6d5, 2026-11-17 복사)
// 바뀐 부분: docs/agent/08-copied-code-register.md CP-10

package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
)

type aes256GCMAlgorithm struct{}

func (aes256GCMAlgorithm) ID() string { return AlgorithmAES256GCM }

func (aes256GCMAlgorithm) KeySize() int { return 32 }

func (a aes256GCMAlgorithm) NewAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != a.KeySize() {
		return nil, fmt.Errorf("%w: unexpected key length", ErrInvalidKey)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: cipher initialization failed", ErrInvalidKey)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: AEAD initialization failed", ErrInvalidKey)
	}
	return aead, nil
}
