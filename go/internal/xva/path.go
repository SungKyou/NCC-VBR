// copied from ncc-xen backend/internal/job/vm_backup_util.go @043c6d5 (CP-VM-09), 2026-11-19.
// 바뀐 부분(docs/vm/08-copied-code-register.md CP-VM-09):
//   - BackupSettings 대신 이 패키지의 Settings 를 받는다.
//   - 실패를 bool 대신 오류 코드가 있는 *Error 로 돌려준다.
//   - export 디렉터리·파일 이름 결합(ResolveOutput)과 확장자 규칙을 이 파일로 모았다(원본은 runExport 안).
//   - xapiBoolField 는 vm 패키지로 옮겼다.

package xva

import (
	"path/filepath"
	"strings"
)

// Settings 는 경로 규칙에 쓰는 설정이다(sysconfig.yaml backup.*).
type Settings struct {
	BasePath             string
	SandboxMode          bool
	AllowedAbsolutePaths []string
}

// ValidFileName 은 파일 이름에 경로 구분자·금지 문자·".." 가 없는지 본다(Windows 금지 문자 기준).
func ValidFileName(name string) bool {
	if name == "" || strings.Contains(name, "..") {
		return false
	}
	return !strings.ContainsAny(name, `\/:*?"<>|`)
}

// containsParentRef 는 경로에 상위 디렉터리 참조("..")가 있는지 본다.
func containsParentRef(cleaned string) bool {
	for _, seg := range strings.FieldsFunc(cleaned, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return true
		}
	}
	return false
}

// Resolve 는 요청의 경로를 실제 경로로 바꾼다. 상대 경로는 BasePath 아래, 절대 경로는 그대로다.
// SandboxMode 면 절대 경로가 BasePath 나 AllowedAbsolutePaths 아래(심볼릭 링크를 푼 실제 경로 기준)여야 한다.
func Resolve(s Settings, p string) (string, error) {
	cleaned := filepath.Clean(p)
	if containsParentRef(cleaned) {
		return "", &Error{Code: CodeInvalidPath, Err: errorf("path contains '..': %s", p)}
	}
	if !filepath.IsAbs(cleaned) {
		return filepath.Join(s.BasePath, cleaned), nil
	}
	if !s.SandboxMode {
		return cleaned, nil
	}
	roots := append([]string{s.BasePath}, s.AllowedAbsolutePaths...)
	if isPathWithinAny(cleaned, roots) {
		return cleaned, nil
	}
	return "", &Error{Code: CodePathNotAllowed, Err: errorf("path is outside the allowed roots: %s", p)}
}

// OutputName 은 내보낼 파일 이름에 확장자를 붙인다. ".xva" 가 없으면 붙이고, zstd 면 ".zst" 를 더 붙인다.
func OutputName(fileName, compression string) string {
	if !strings.HasSuffix(strings.ToLower(fileName), ".xva") {
		fileName += ".xva"
	}
	if compression == CompressionZstd {
		fileName += ".zst"
	}
	return fileName
}

// isPathWithinAny 는 candidate 가 roots 가운데 하나의 실제 경로 아래인지 본다.
func isPathWithinAny(candidate string, roots []string) bool {
	candReal := realpathOrClean(candidate)
	for _, root := range roots {
		if root == "" {
			continue
		}
		if pathHasPrefix(candReal, realpathOrClean(root)) {
			return true
		}
	}
	return false
}

// realpathOrClean 은 심볼릭 링크를 푼 실제 경로다. 아직 없는 파일이면 부모 디렉터리까지만 푼다.
func realpathOrClean(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	dir := filepath.Dir(p)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(real, filepath.Base(p))
	}
	return filepath.Clean(p)
}

// pathHasPrefix 는 candidate 가 root 디렉터리 아래인지 경로 구분자 경계를 지켜 본다.
func pathHasPrefix(candidate, root string) bool {
	candidate = filepath.Clean(candidate)
	root = filepath.Clean(root)
	if candidate == root {
		return true
	}
	return strings.HasPrefix(candidate, root+string(filepath.Separator))
}
