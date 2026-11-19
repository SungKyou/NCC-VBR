package vmconfig

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const base = `
server:
  port: 8090
api_auth:
  enabled: false
crypto:
  enabled: false
locale: ko
backup:
  base_path: ./b
concurrency:
  max_jobs: 2
task_store:
  dir: ./t
`

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// T-VM-U71: 다시 읽으면 바뀐 값이 반영되고 구독자가 불린다. 잘못된 파일이면 이전 값을 유지한다.
func TestLoadAndReload(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sysconfig.yaml")
	write(t, p, base+"\nunknown_top: 1\n")
	var logs []string
	s, warns, err := Load(p, func(level, msg string, args ...any) { logs = append(logs, level+" "+msg) })
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "unknown_top") {
		t.Fatalf("warns %v", warns)
	}
	c := s.Current()
	if c.Concurrency.MaxJobs != 2 || c.Concurrency.MaxQueue != 100 || c.Backup.TimeoutHours != 4 || c.XAPI.ProgressIntervalSec != 5 {
		t.Fatalf("defaults: %+v", c.Config)
	}
	if len(TestSwitchWarnings(c)) != 2 {
		t.Fatal("both test switches are off: 2 warnings")
	}
	var called atomic.Int32
	s.Subscribe(func(old, cur *Loaded) { called.Add(1) })

	write(t, p, strings.NewReplacer("max_jobs: 2", "max_jobs: 5", "locale: ko", "locale: ja").Replace(base))
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	if s.Current().Concurrency.MaxJobs != 5 || s.Current().Locale != "ja" || called.Load() != 1 {
		t.Fatalf("reload: %+v called %d", s.Current().Config, called.Load())
	}

	write(t, p, "server: [broken")
	if err := s.Reload(); err == nil {
		t.Fatal("broken yaml must fail")
	}
	write(t, p, strings.Replace(base, "max_jobs: 2", "max_jobs: 99", 1))
	if err := s.Reload(); err == nil {
		t.Fatal("out of range must fail")
	}
	if s.Current().Concurrency.MaxJobs != 5 || called.Load() != 1 {
		t.Fatal("previous settings must be kept")
	}

	write(t, p, strings.Replace(base, "port: 8090", "port: 9000", 1))
	_ = s.Reload()
	found := false
	for _, l := range logs {
		if strings.Contains(l, "needs restart") {
			found = true
		}
	}
	if !found {
		t.Fatalf("port change must warn restart: %v", logs)
	}
}

// 인증이 켜졌는데 토큰이 없으면 기동하지 않는다. crypto.enabled 인데 평문 토큰도 거부한다.
func TestAuthAndCryptoRules(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sysconfig.yaml")
	write(t, p, strings.Replace(base, "api_auth:\n  enabled: false", "api_auth:\n  enabled: true", 1))
	if _, _, err := Load(p, nil); err == nil || !strings.Contains(err.Error(), "VBR_VM_API_TOKEN") {
		t.Fatalf("missing token: %v", err)
	}
	t.Setenv(EnvAPIToken, "from-env")
	s, _, err := Load(p, nil)
	if err != nil || s.Current().APIToken != "from-env" {
		t.Fatalf("env token: %v", err)
	}
	write(t, p, strings.Replace(base, "crypto:\n  enabled: false", "crypto:\n  enabled: true\n  keyring_file: "+filepath.Join(dir, "none.yaml"), 1))
	if _, _, err := Load(p, nil); err == nil || !strings.Contains(err.Error(), "keyring") {
		t.Fatalf("missing keyring: %v", err)
	}
	l := &Loaded{}
	l.Crypto.Enabled = true
	if _, err := l.Reveal("plain", "x"); err != ErrPlaintext {
		t.Fatalf("plaintext: %v", err)
	}
}
