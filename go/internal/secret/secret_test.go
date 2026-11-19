package secret

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testKeyring(t *testing.T) []byte {
	t.Helper()
	cfg := KeyringConfig{Version: KeyringVersion1, ActiveKeyID: "k1", Keys: map[string]KeyConfig{
		"k1": {Algorithm: AlgorithmAES256GCM, Material: base64.StdEncoding.EncodeToString(make([]byte, 32))},
	}}
	data, err := MarshalKeyringConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// T-SEC-U01 암호화와 복호화, 용도 문자열이 다르면 실패
func TestRoundTrip(t *testing.T) {
	p, err := Load(testKeyring(t))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := p.Encrypt("ncc-vbr/xapi-password", []byte("s3cret"))
	if err != nil || !strings.HasPrefix(enc, "ENC[v1:aes-256-gcm:k1:") || !HasEnvelopePrefix(enc) {
		t.Fatalf("encrypt %q %v", enc, err)
	}
	plain, err := p.Decrypt("ncc-vbr/xapi-password", enc)
	if err != nil || string(plain) != "s3cret" {
		t.Fatalf("decrypt %q %v", plain, err)
	}
	if _, err := p.Decrypt("other", enc); err == nil {
		t.Fatal("wrong purpose must fail")
	}
}

// T-SEC-U02 키링 JSON 은 모르는 필드를 거부한다(원본 safeyaml.DecodeKnownFields 대응)
func TestUnknownField(t *testing.T) {
	data := strings.Replace(string(testKeyring(t)), `"version"`, `"extra": 1, "version"`, 1)
	if _, err := Load([]byte(data)); err == nil {
		t.Fatal("unknown field must fail")
	}
}

// T-SEC-U03 키링 파일 권한이 넓으면 거부(Linux)
func TestFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("권한 검사는 unix 에서만 한다(원본과 같다)")
	}
	path := filepath.Join(t.TempDir(), "k.json")
	if err := os.WriteFile(path, testKeyring(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFromFile(path); err == nil {
		t.Fatal("0644 must fail")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFromFile(path); err != nil {
		t.Fatal(err)
	}
}
