package agent

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"ncc-vbr/internal/config"
)

// Section 은 node_info.cfg 의 섹션 이름이다(원본과 같다).
const Section = "VBR Agent for Xenserver"

// 설정 키. PORT-NO 와 MAX Log size 는 원본 키, 나머지는 원격 실행으로 새로 생긴 키다
// (docs/agent/09-config-spec.md).
const (
	keyPort        = "PORT-NO"
	keyXAPIAddress = "XAPI Address"
	keyXAPIUser    = "XAPI User"
	keyXAPIPass    = "XAPI Password"
	keyKeyring     = "XAPI Keyring"
	keyVerifySSL   = "XAPI Verify SSL"
	keyBackupRoot  = "Backup Root"
	keyAllowClient = "Allow Client"
	keyAsyncThread = "Async Thread"
)

// 비동기 작업자 수. 원본 DEFAULT/MIN/MAX_ASYNC_THREAD_CNT 다.
const (
	DefaultAsyncWorkers = 3
	MinAsyncWorkers     = 2
	MaxAsyncWorkers     = 10
)

// Settings 는 agent 설정이다.
type Settings struct {
	Port         int
	XAPIAddress  string // 예: https://192.168.102.53
	XAPIUser     string
	XAPIPassword string // ENC[...] 이면 Keyring 으로 푼다
	Keyring      string
	VerifySSL    bool
	BackupRoots  []string
	AllowClients []string // 비어 있으면 모두 허용(경고 기록)
	AsyncWorkers int
}

// LoadSettings 는 섹션에서 설정을 읽는다. 없어서 동작할 수 없는 키는 오류다.
func LoadSettings(sec config.Section) (Settings, []string, error) {
	var s Settings
	var notes []string

	port, found := sec.Int64(keyPort)
	if !found || port <= 0 || port > 65535 {
		return s, notes, fmt.Errorf("Config file [%s] parameter not found or invalid", keyPort)
	}
	s.Port = int(port)

	addr, found := sec.String(keyXAPIAddress)
	if !found || addr == "" {
		return s, notes, fmt.Errorf("Config file [%s] parameter not found", keyXAPIAddress)
	}
	if !strings.Contains(addr, "://") {
		addr = "https://" + addr
	}
	s.XAPIAddress = strings.TrimRight(addr, "/")

	s.XAPIUser, found = sec.String(keyXAPIUser)
	if !found || s.XAPIUser == "" {
		s.XAPIUser = "root"
		notes = append(notes, fmt.Sprintf("Config file [%s] parameter not found.(root default setting)", keyXAPIUser))
	}
	s.XAPIPassword, found = sec.String(keyXAPIPass)
	if !found {
		return s, notes, fmt.Errorf("Config file [%s] parameter not found", keyXAPIPass)
	}
	s.Keyring, _ = sec.String(keyKeyring)

	v, _ := sec.String(keyVerifySSL)
	s.VerifySSL = strings.EqualFold(v, "Y") || strings.EqualFold(v, "YES") || strings.EqualFold(v, "TRUE")

	s.BackupRoots = sec.Fields(keyBackupRoot)
	if len(s.BackupRoots) == 0 {
		return s, notes, fmt.Errorf("Config file [%s] parameter not found", keyBackupRoot)
	}
	for i, root := range s.BackupRoots {
		s.BackupRoots[i] = filepath.Clean(root)
	}

	s.AllowClients = sec.Fields(keyAllowClient)
	for _, c := range s.AllowClients {
		if net.ParseIP(c) == nil {
			return s, notes, fmt.Errorf("Config file [%s] value is not an IP address: %s", keyAllowClient, c)
		}
	}
	if len(s.AllowClients) == 0 {
		notes = append(notes, fmt.Sprintf("WARNING. Config file [%s] parameter not found. All clients are allowed.", keyAllowClient))
	}

	s.AsyncWorkers = DefaultAsyncWorkers
	if n, found := sec.String(keyAsyncThread); found {
		w, err := strconv.Atoi(n)
		if err != nil || w < MinAsyncWorkers || w > MaxAsyncWorkers {
			return s, notes, fmt.Errorf("Config file [%s] must be %d..%d", keyAsyncThread, MinAsyncWorkers, MaxAsyncWorkers)
		}
		s.AsyncWorkers = w
	}
	return s, notes, nil
}

// allowed 는 접속한 주소가 허용 목록에 있는지 본다.
func (s Settings) allowed(remote net.Addr) bool {
	if len(s.AllowClients) == 0 {
		return true
	}
	host, _, err := net.SplitHostPort(remote.String())
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	for _, c := range s.AllowClients {
		if ip != nil && ip.Equal(net.ParseIP(c)) {
			return true
		}
	}
	return false
}

// insideRoots 는 파일 경로가 백업 루트 아래인지 본다(Q-A15).
// 상대 경로, "..", 루트 자체는 거부한다. 심볼릭 링크는 따라가지 않고 이름만 본다.
// ncc-xen vm_backup_util.go 의 경로 검사(isValidBackupFileName, pathHasPrefix)를 참고해 새로 썼다
// (그 파일은 ncc-xen 설정 구조에 묶여 있어 복사하지 않았다. 08 문서 CP-09).
func (s Settings) insideRoots(name string) bool {
	if name == "" || !filepath.IsAbs(name) {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(name), "/") {
		if part == ".." {
			return false
		}
	}
	clean := filepath.Clean(name)
	for _, root := range s.BackupRoots {
		rel, err := filepath.Rel(root, clean)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		return true
	}
	return false
}
