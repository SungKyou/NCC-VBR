package xva

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// T-VM-U24, U25: 파일 이름 규칙, 상대·절대 경로, sandbox_mode, 확장자.
func TestPathRules(t *testing.T) {
	dir := t.TempDir()
	baseDir := filepath.Join(dir, "base")
	other := filepath.Join(dir, "other")
	_ = os.MkdirAll(baseDir, 0o755)
	_ = os.MkdirAll(other, 0o755)
	for _, n := range []string{"a", "vm-1.xva", "백업"} {
		if !ValidFileName(n) {
			t.Errorf("%q must be valid", n)
		}
	}
	for _, n := range []string{"", "a/b", `a\b`, "a:b", "..", "a..b", "a*"} {
		if ValidFileName(n) {
			t.Errorf("%q must be invalid", n)
		}
	}
	s := Settings{BasePath: baseDir}
	if p, err := Resolve(s, "sub"); err != nil || p != filepath.Join(baseDir, "sub") {
		t.Fatalf("relative: %s %v", p, err)
	}
	if p, err := Resolve(s, other); err != nil || p != other {
		t.Fatalf("absolute without sandbox: %s %v", p, err)
	}
	var e *Error
	if _, err := Resolve(s, "../x"); !errors.As(err, &e) || e.Code != CodeInvalidPath {
		t.Fatalf("parent ref: %v", err)
	}
	s.SandboxMode = true
	if _, err := Resolve(s, other); !errors.As(err, &e) || e.Code != CodePathNotAllowed {
		t.Fatalf("sandbox: %v", err)
	}
	s.AllowedAbsolutePaths = []string{other}
	if _, err := Resolve(s, filepath.Join(other, "f.xva")); err != nil {
		t.Fatalf("allowed: %v", err)
	}
	if OutputName("a", CompressionNone) != "a.xva" || OutputName("a.XVA", CompressionGzip) != "a.XVA" ||
		OutputName("a", CompressionZstd) != "a.xva.zst" {
		t.Fatal("output names")
	}
}
