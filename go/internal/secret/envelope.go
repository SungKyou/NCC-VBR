// 출처: ncc-xen stdcrypto/secret/envelope.go (커밋 043c6d5, 2026-11-17 복사)
// 바뀐 부분: docs/agent/08-copied-code-register.md CP-10

package secret

import (
	"encoding/base64"
	"fmt"
	"strings"
)

const envelopePrefix = "ENC["

type EnvelopeMetadata struct {
	Version   string
	Algorithm string
	KeyID     string
}

type parsedEnvelope struct {
	EnvelopeMetadata
	payload []byte
}

func HasEnvelopePrefix(value string) bool {
	return strings.HasPrefix(value, envelopePrefix)
}

func ParseEnvelope(value string) (EnvelopeMetadata, error) {
	parsed, err := parseEnvelope(value)
	if err != nil {
		return EnvelopeMetadata{}, err
	}
	return parsed.EnvelopeMetadata, nil
}

func parseEnvelope(value string) (parsedEnvelope, error) {
	if len(value) > MaximumEnvelopeBytes || !strings.HasPrefix(value, envelopePrefix) || !strings.HasSuffix(value, "]") {
		return parsedEnvelope{}, ErrInvalidEnvelope
	}
	parts := strings.Split(value[len(envelopePrefix):len(value)-1], ":")
	if len(parts) != 4 || parts[0] == "" || parts[1] == "" || parts[2] == "" || parts[3] == "" {
		return parsedEnvelope{}, ErrInvalidEnvelope
	}
	if parts[0] != Version1 {
		return parsedEnvelope{}, ErrUnsupportedVersion
	}
	algorithm, err := lookupAlgorithm(parts[1])
	if err != nil {
		return parsedEnvelope{}, err
	}
	if !validKeyID(parts[2]) {
		return parsedEnvelope{}, ErrInvalidEnvelope
	}
	if strings.ContainsAny(parts[3], "= \t\r\n") {
		return parsedEnvelope{}, ErrInvalidEnvelope
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[3])
	if err != nil {
		return parsedEnvelope{}, ErrInvalidEnvelope
	}
	aead, err := algorithm.NewAEAD(make([]byte, algorithm.KeySize()))
	if err != nil {
		return parsedEnvelope{}, ErrInvalidEnvelope
	}
	if len(payload) < aead.NonceSize()+aead.Overhead() {
		return parsedEnvelope{}, ErrInvalidEnvelope
	}
	return parsedEnvelope{
		EnvelopeMetadata: EnvelopeMetadata{Version: parts[0], Algorithm: parts[1], KeyID: parts[2]},
		payload:          payload,
	}, nil
}

func formatEnvelope(version, algorithm, keyID string, payload []byte) (string, error) {
	if version != Version1 || !validKeyID(keyID) {
		return "", ErrInvalidEnvelope
	}
	if _, err := lookupAlgorithm(algorithm); err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	value := fmt.Sprintf("%s%s:%s:%s:%s]", envelopePrefix, version, algorithm, keyID, encoded)
	if len(value) > MaximumEnvelopeBytes {
		return "", ErrInvalidEnvelope
	}
	return value, nil
}
