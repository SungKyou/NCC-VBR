// vbr_secret 은 vbr_agent 의 XAPI 암호를 암호화하는 도구다(새로 작성).
// 암호 형식은 ncc-xen stdcrypto 의 ENC[v1:aes-256-gcm:키ID:...] 봉투와 같다.
//
//	vbr_secret keygen  <키링 파일>            키링 파일을 새로 만든다(권한 600, 이미 있으면 거부)
//	vbr_secret encrypt <키링 파일> [용도]     표준 입력의 첫 줄을 암호화해 ENC[...] 를 출력한다
//
// 용도를 빼면 XAPI 암호(ncc-vbr/xapi-password, vbr_agent·vbr_vm 요청 암호)다.
// vbr_vm 설정 토큰은 sysconfig.api_auth.VBR_VM_API_TOKEN, sysconfig.api_auth.GATEWAY_INTERNAL_TOKEN 을 준다
// (2026-11-19 추가, docs/vm/07-operations.md 3장).
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	"ncc-vbr/internal/secret"
)

const purpose = "ncc-vbr/xapi-password" // vbr_agent 의 PasswordPurpose 와 같다

func main() {
	if len(os.Args) < 3 || len(os.Args) > 4 {
		usage()
	}
	var err error
	switch {
	case os.Args[1] == "keygen" && len(os.Args) == 3:
		err = keygen(os.Args[2])
	case os.Args[1] == "encrypt":
		p := purpose
		if len(os.Args) == 4 {
			if !allowedPurpose(os.Args[3]) {
				fmt.Fprintf(os.Stderr, "vbr_secret : unknown purpose %q\n", os.Args[3])
				os.Exit(255)
			}
			p = os.Args[3]
		}
		err = encrypt(os.Args[2], p)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "vbr_secret : %v\n", err)
		os.Exit(1)
	}
}

// purposes 는 encrypt 가 받는 용도다. 오타로 풀 수 없는 값을 만들지 않게 목록으로 막는다.
var purposes = []string{
	purpose,
	"sysconfig.api_auth.VBR_VM_API_TOKEN",
	"sysconfig.api_auth.GATEWAY_INTERNAL_TOKEN",
}

func allowedPurpose(p string) bool {
	for _, x := range purposes {
		if x == p {
			return true
		}
	}
	return false
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage : vbr_secret keygen <keyring-file>\n        vbr_secret encrypt <keyring-file> [purpose]  (secret from stdin)\n        purpose : %s\n", strings.Join(purposes, " | "))
	os.Exit(255)
}

func keygen(path string) error {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	cfg := secret.KeyringConfig{
		Version:     secret.KeyringVersion1,
		ActiveKeyID: "k1",
		Keys: map[string]secret.KeyConfig{
			"k1": {Algorithm: secret.AlgorithmAES256GCM, Material: base64.StdEncoding.EncodeToString(key)},
		},
	}
	data, err := secret.MarshalKeyringConfig(cfg)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("keyring created : %s\n", path)
	return nil
}

func encrypt(path, use string) error {
	p, err := secret.LoadFromFile(path)
	if err != nil {
		return err
	}
	plain, err := readPassword(os.Stdin)
	if err != nil {
		return err
	}
	enc, err := p.Encrypt(use, []byte(plain))
	if err != nil {
		return err
	}
	fmt.Println(enc)
	return nil
}

// utf8BOM 은 PowerShell 의 $OutputEncoding 이 [Text.Encoding]::UTF8 일 때 파이프 입력 앞에 붙는 3바이트다.
const utf8BOM = "\xef\xbb\xbf"

// readPassword 는 표준 입력의 첫 줄을 암호로 읽는다. 줄 끝의 CR·LF 와 앞의 UTF-8 BOM 은 뗀다.
// 2026-11-18 Windows 실기 시험에서 BOM 이 암호에 섞여 XAPI 인증이 실패한 일로 BOM 제거를 더했다.
func readPassword(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("password is empty")
	}
	plain := strings.TrimPrefix(strings.TrimRight(line, "\r\n"), utf8BOM)
	if plain == "" {
		return "", fmt.Errorf("password is empty")
	}
	return plain, nil
}
