// vbr_agent 는 NCC-VBR agent 의 전환본이다(원본 agent/vbr_agent.c 1.602).
// dom0 가 아니라 별도 Linux 서버에서 실행하고 XAPI 로 풀을 부른다(agentless).
package main

import (
	"fmt"
	"os"

	"ncc-vbr/internal/agent"
	"ncc-vbr/internal/boot"
	"ncc-vbr/internal/secret"
)

// PasswordPurpose 는 암호 봉투의 용도 문자열이다. vbr_secret 과 같아야 한다.
const PasswordPurpose = "ncc-vbr/xapi-password"

// defaultKeyring 은 XAPI Keyring 키가 없을 때의 키링 파일이다(작업 디렉터리 기준).
const defaultKeyring = "vbr_agent.keyring"

func main() {
	app := boot.Start(boot.Spec{
		Title:    agent.Title,
		Version:  agent.Version,
		Usage:    "",
		ArgCount: 0,
		Section:  agent.Section,
	})
	defer app.Close()
	log := app.Log

	cfg, notes, err := agent.LoadSettings(app.Config)
	for _, n := range notes {
		log.Printf("%s\n", n)
	}
	if err != nil {
		fatal(app, "%v", err)
	}
	password, err := openPassword(cfg)
	if err != nil {
		fatal(app, "XAPI password : %v", err)
	}

	hv := agent.NewXen(cfg, password, log)
	a := agent.New(cfg, log, hv, ".")
	if err := a.Listen(); err != nil {
		fatal(app, "%v", err)
	}
	log.Printf("XAPI address [%s] user [%s] backup root %v async thread [%d]\n",
		cfg.XAPIAddress, cfg.XAPIUser, cfg.BackupRoots, cfg.AsyncWorkers)
	a.Run(app.Context)
}

// openPassword 는 ENC[...] 암호를 키링으로 푼다(Q-A14: ncc-xen stdcrypto 와 같은 방식).
// 평문 암호는 받지 않는다.
func openPassword(cfg agent.Settings) (string, error) {
	if !secret.HasEnvelopePrefix(cfg.XAPIPassword) {
		return "", fmt.Errorf("must be encrypted (ENC[...]). use vbr_secret encrypt")
	}
	path := cfg.Keyring
	if path == "" {
		path = defaultKeyring
	}
	p, err := secret.LoadFromFile(path)
	if err != nil {
		return "", fmt.Errorf("keyring [%s] : %w", path, err)
	}
	plain, err := p.Decrypt(PasswordPurpose, cfg.XAPIPassword)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func fatal(app *boot.App, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	app.Log.Printf("FATAL Error	[VBR Agent] STOP RUN. %s\n", msg)
	fmt.Printf("FATAL Error	[VBR Agent] STOP RUN. %s\n", msg)
	app.Close()
	os.Exit(boot.ExitFatal)
}
