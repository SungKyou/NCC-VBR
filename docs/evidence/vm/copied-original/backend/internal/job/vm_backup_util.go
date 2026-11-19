package job

// vm_backup_util.go — VM_BACKUP 공용 유틸리티 (경로 검증, XAPI bool 필드 파싱)
// 🆕 2026-09-01: vm_backup.go에서 기계적으로 분리됨 (로직 변경 없음).

import (
	"path/filepath"
	"strings"
)

func isValidBackupFileName(name string) bool {
	if name == "" || strings.Contains(name, "..") {
		return false
	}
	return !strings.ContainsAny(name, `\/:*?"<>|`)
}

// containsParentRef는 경로에 상위 디렉토리 참조("..")가 포함되어 있는지 확인합니다.
func containsParentRef(cleaned string) bool {
	for _, seg := range strings.FieldsFunc(cleaned, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────
// 파일시스템 sandbox 경계 검사 (2026-09-18, 코드 리뷰 지적 대응)
//
// 배경: export_path/file_path가 절대경로면 base_path와 무관하게 그대로
// 허용해왔음(vm_backup.go 상단 "경로 규칙" 주석 참고 — 의도된 설계였음).
// API 인증이 뚫리면 이 절대경로 허용을 통해 Backend OS 권한 범위 내 임의
// 위치에 대용량 백업 파일을 쓰거나, 임의 위치의 파일을 OVF로 읽어 파싱
// 시도하는 것이 가능했음.
//
// resolvePathWithSandbox가 새 진입점입니다. settings.SandboxMode가
// false(기본값 — sysconfig.yaml에 명시하지 않으면 기존 동작 그대로
// 유지)면 이전과 동일하게 절대경로를 그대로 허용합니다. true로 켜면
// base_path 또는 settings.AllowedAbsolutePaths에 명시된 경로 하위로
// symlink까지 해석한 실제 경로(realpath) 기준으로 귀속되는 경우에만
// 허용합니다.
// ─────────────────────────────────────────────

// resolvePathWithSandbox는 cleanedPath(이미 filepath.Clean + containsParentRef
// 검사를 통과한 값)를 최종 파일시스템 경로로 해석합니다. 실패 시 ok=false를
// 반환하며, 호출부는 각자의 i18n 메시지로 실패를 보고해야 합니다(이 함수
// 자체는 로케일 문자열을 만들지 않습니다 — VM_BACKUP/ovf_inspector 등 서로
// 다른 i18n 네임스페이스에서 공용으로 쓰기 위함).
func resolvePathWithSandbox(settings BackupSettings, cleanedPath string) (resolved string, ok bool) {
	if !filepath.IsAbs(cleanedPath) {
		return filepath.Join(settings.BasePath, cleanedPath), true
	}
	if !settings.SandboxMode {
		return cleanedPath, true // 기존 동작(절대경로 그대로 허용) 유지
	}
	roots := make([]string, 0, 1+len(settings.AllowedAbsolutePaths))
	roots = append(roots, settings.BasePath)
	roots = append(roots, settings.AllowedAbsolutePaths...)
	if isPathWithinAny(cleanedPath, roots) {
		return cleanedPath, true
	}
	return "", false
}

// isPathWithinAny는 candidate가 roots 중 하나의 실제 경로(symlink 해석
// 후) 하위에 포함되는지 확인합니다 — symlink로 sandbox를 우회(base_path
// 안에 만든 symlink가 밖을 가리키는 경우 등)하는 것을 막기 위함입니다.
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

// realpathOrClean은 p의 symlink를 해석한 실제 경로를 반환합니다. p가 아직
// 존재하지 않는 파일(예: 이제 막 생성할 export 대상)이면 EvalSymlinks가
// 실패하므로, 존재하는 가장 가까운 상위 디렉토리까지만 실제 경로로 해석해
// 그 위에 나머지 경로를 이어 붙입니다. 그마저도 실패하면(상위 디렉토리
// 전부 없음) 최소한 filepath.Clean 결과로 폴백합니다 — 이 경우 symlink
// 우회까지는 못 막지만, 접두사 검사 자체는 여전히 유효합니다.
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

// pathHasPrefix는 candidate가 root 디렉토리 하위인지, 경로 구분자 경계를
// 지켜서 확인합니다("/mnt/backup2"가 "/mnt/backup"의 접두사로 잘못
// 매칭되는 문자열 접두사 버그를 방지).
func pathHasPrefix(candidate, root string) bool {
	candidate = filepath.Clean(candidate)
	root = filepath.Clean(root)
	if candidate == root {
		return true
	}
	return strings.HasPrefix(candidate, root+string(filepath.Separator))
}

// xapiBoolField는 XAPI 레코드의 boolean 필드를 안전하게 해석합니다.
// (XAPI는 bool 또는 "0"/"1"/"true"/"false" 문자열 두 형태 모두 반환할 수 있음)
func xapiBoolField(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "1" || strings.EqualFold(val, "true")
	}
	return false
}

