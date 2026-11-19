// 출처: ncc-xen stdcrypto/secret/config.go (커밋 043c6d5, 2026-11-17 복사)
// 바뀐 부분: docs/agent/08-copied-code-register.md CP-10

package secret

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
)

const (
	KeyringVersion1      = 1
	MaximumEnvelopeBytes = 64 * 1024
	MaximumKeyringBytes  = 64 * 1024
)

var keyIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

type KeyringConfig struct {
	Version     int                  `json:"version"`
	ActiveKeyID string               `json:"active_key_id"`
	Keys        map[string]KeyConfig `json:"keys"`
}

type KeyConfig struct {
	Algorithm string `json:"algorithm"`
	Material  string `json:"material"`
}

func ValidateKeyringConfig(cfg KeyringConfig) error {
	_, err := buildKeyring(cfg)
	return err
}

func buildKeyring(cfg KeyringConfig) (*keyring, error) {
	if cfg.Version != KeyringVersion1 {
		return nil, fmt.Errorf("%w: unsupported keyring version", ErrInvalidKeyring)
	}
	if !validKeyID(cfg.ActiveKeyID) {
		return nil, fmt.Errorf("%w: invalid active key ID", ErrInvalidKeyring)
	}
	if len(cfg.Keys) == 0 {
		return nil, fmt.Errorf("%w: no keys configured", ErrInvalidKeyring)
	}

	entries := make(map[string]keyEntry, len(cfg.Keys))
	for id, entry := range cfg.Keys {
		if !validKeyID(id) {
			return nil, fmt.Errorf("%w: invalid key ID", ErrInvalidKeyring)
		}
		algorithm, err := lookupAlgorithm(strings.TrimSpace(entry.Algorithm))
		if err != nil {
			return nil, fmt.Errorf("%w: key algorithm is not allowed", ErrInvalidKeyring)
		}
		material, err := decodeKeyMaterial(entry.Material, algorithm.KeySize())
		if err != nil {
			return nil, err
		}
		entries[id] = keyEntry{algorithm: algorithm.ID(), material: material}
	}
	if _, ok := entries[cfg.ActiveKeyID]; !ok {
		return nil, fmt.Errorf("%w: active key does not exist", ErrInvalidKeyring)
	}
	return &keyring{activeKeyID: cfg.ActiveKeyID, entries: entries}, nil
}

func decodeKeyMaterial(encoded string, expectedSize int) ([]byte, error) {
	if encoded == "" {
		return nil, fmt.Errorf("%w: key material is empty", ErrInvalidKey)
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || base64.StdEncoding.EncodeToString(decoded) != encoded {
		return nil, fmt.Errorf("%w: key material encoding is invalid", ErrInvalidKey)
	}
	if len(decoded) != expectedSize {
		return nil, fmt.Errorf("%w: unexpected key length", ErrInvalidKey)
	}
	return append([]byte(nil), decoded...), nil
}

func validKeyID(id string) bool {
	return keyIDPattern.MatchString(id)
}
