// 출처: ncc-xen stdcrypto/secret/load.go (커밋 043c6d5, 2026-11-17 복사)
// 바뀐 부분: docs/agent/08-copied-code-register.md CP-10
// (키링 파일 형식을 YAML에서 JSON으로 바꿨다. YAML 라이브러리와 safeyaml을 들여오지 않기 위해서다.)

package secret

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

func LoadFromFile(path string) (*Protector, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("%w: keyring file cannot be inspected", ErrInvalidKeyring)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: keyring path must be a regular file", ErrInvalidKeyring)
	}
	if info.Size() > MaximumKeyringBytes {
		return nil, fmt.Errorf("%w: keyring file is too large", ErrInvalidKeyring)
	}
	if err := validateKeyringFileMode(info.Mode()); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: keyring file cannot be read", ErrInvalidKeyring)
	}
	return Load(data)
}

func Load(data []byte) (*Protector, error) {
	cfg, err := ParseKeyringConfig(data)
	if err != nil {
		return nil, err
	}
	return Build(cfg)
}

func ParseKeyringConfig(data []byte) (KeyringConfig, error) {
	if len(data) == 0 || len(data) > MaximumKeyringBytes {
		return KeyringConfig{}, fmt.Errorf("%w: keyring size is invalid", ErrInvalidKeyring)
	}
	// 원본의 safeyaml.DecodeKnownFields 대신 표준 JSON 해석기에서 모르는 필드를 거부한다.
	var cfg KeyringConfig
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return KeyringConfig{}, fmt.Errorf("%w: keyring fields are invalid", ErrInvalidKeyring)
	}
	if dec.More() {
		return KeyringConfig{}, fmt.Errorf("%w: keyring JSON has trailing data", ErrInvalidKeyring)
	}
	if err := ValidateKeyringConfig(cfg); err != nil {
		return KeyringConfig{}, err
	}
	return cfg, nil
}

func MarshalKeyringConfig(cfg KeyringConfig) ([]byte, error) {
	if err := ValidateKeyringConfig(cfg); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("%w: keyring encoding failed", ErrInvalidKeyring)
	}
	if len(data) > MaximumKeyringBytes {
		return nil, fmt.Errorf("%w: keyring file is too large", ErrInvalidKeyring)
	}
	return data, nil
}
