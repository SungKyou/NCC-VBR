package logger

import "testing"

// 🆕 2026-10-28 — InitOption.FileNameSuffix 경로 계산.
func TestSuffixedFileName(t *testing.T) {
	cases := []struct{ in, suffix, want string }{
		{"./logs/ncc-xen.log", "-analyzer", "./logs/ncc-xen-analyzer.log"},
		{"/var/log/ncc/app", "-analyzer", "/var/log/ncc/app-analyzer"}, // 확장자 없음
		{"", "-analyzer", "./logs/app-analyzer.log"},                   // 설정 없음 → 기본값 기준
	}
	for _, c := range cases {
		if got := suffixedFileName(c.in, c.suffix); got != c.want {
			t.Errorf("suffixedFileName(%q, %q) = %q, want %q", c.in, c.suffix, got, c.want)
		}
	}
}
