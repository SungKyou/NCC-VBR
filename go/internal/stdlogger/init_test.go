// copied from ncc-xen stdlogger/logger/init_test.go @043c6d5 (CP-VM-11), 2026-11-19. 변경: 이 머리 주석과 gofmt 정렬(공백·줄바꿈)뿐, 코드는 그대로.
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
