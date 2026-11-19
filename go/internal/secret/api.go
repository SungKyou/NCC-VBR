// 출처: ncc-xen stdcrypto/secret/api.go (커밋 043c6d5, 2026-11-17 복사)
// 바뀐 부분: docs/agent/08-copied-code-register.md CP-10

package secret

import (
	"crypto/rand"
	"fmt"
)

type Protector struct {
	keyring *keyring
}

func Build(cfg KeyringConfig) (*Protector, error) {
	keyring, err := buildKeyring(cfg)
	if err != nil {
		return nil, err
	}
	return &Protector{keyring: keyring}, nil
}

func (p *Protector) Encrypt(purpose string, plaintext []byte) (string, error) {
	if p == nil || p.keyring == nil {
		return "", ErrInvalidKeyring
	}
	if purpose == "" {
		return "", fmt.Errorf("%w: purpose is required", ErrAuthenticationFailed)
	}
	if len(plaintext) == 0 {
		return "", nil
	}
	if len(plaintext) > MaximumEnvelopeBytes {
		return "", ErrInvalidEnvelope
	}

	keyID, entry := p.keyring.active()
	algorithm, err := lookupAlgorithm(entry.algorithm)
	if err != nil {
		return "", err
	}
	aead, err := algorithm.NewAEAD(entry.material)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("credential nonce generation failed")
	}
	payload := make([]byte, 0, len(nonce)+len(plaintext)+aead.Overhead())
	payload = append(payload, nonce...)
	payload = aead.Seal(payload, nonce, plaintext, []byte(purpose))
	return formatEnvelope(Version1, algorithm.ID(), keyID, payload)
}

func (p *Protector) Decrypt(purpose, envelope string) ([]byte, error) {
	if p == nil || p.keyring == nil {
		return nil, ErrInvalidKeyring
	}
	if purpose == "" {
		return nil, fmt.Errorf("%w: purpose is required", ErrAuthenticationFailed)
	}
	if envelope == "" {
		return []byte{}, nil
	}
	parsed, err := parseEnvelope(envelope)
	if err != nil {
		return nil, err
	}
	entry, ok := p.keyring.find(parsed.KeyID)
	if !ok {
		return nil, ErrKeyNotFound
	}
	if entry.algorithm != parsed.Algorithm {
		return nil, ErrUnsupportedAlgorithm
	}
	algorithm, err := lookupAlgorithm(parsed.Algorithm)
	if err != nil {
		return nil, err
	}
	aead, err := algorithm.NewAEAD(entry.material)
	if err != nil {
		return nil, err
	}
	nonceSize := aead.NonceSize()
	plaintext, err := aead.Open(nil, parsed.payload[:nonceSize], parsed.payload[nonceSize:], []byte(purpose))
	if err != nil {
		return nil, ErrAuthenticationFailed
	}
	return plaintext, nil
}
