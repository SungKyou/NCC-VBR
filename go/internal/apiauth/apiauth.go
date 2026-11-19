// copied from ncc-xen backend/internal/apiauth/apiauth.go @043c6d5 (CP-VM-02), 2026-11-19.
// 바뀐 부분(docs/vm/08-copied-code-register.md CP-VM-02):
//   - 토큰 이름을 NCC_XEN_API_TOKEN 에서 VBR_VM_API_TOKEN 으로 바꿨다.
//   - 토큰을 고정 값 대신 함수로 받는다. 설정 즉시 반영(DEC-VM-17)으로 토큰·enabled 가 실행 중에 바뀐다.
//   - 401 본문에 code 필드를 더했다(05 문서 9장).

// Package apiauth 는 vbr_vm 이 받는 HTTP API 의 토큰 인증이다.
//
//   - enabled=false: 인증 없음(경고 로그는 vmconfig 쪽에서 남긴다).
//   - enabled=true: /api/v1/* 요청에 같은 값의 X-Internal-Token 헤더가 없으면 401.
//     /health 는 모니터링용으로 열어 둔다(이 미들웨어를 붙이지 않는다).
package apiauth

import (
	"crypto/subtle"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// Header 는 토큰을 싣는 HTTP 헤더 이름이다(ncc-xen 과 같다).
const Header = "X-Internal-Token"

// EnvToken 은 받는 쪽 토큰을 담는 환경 변수 이름이다. 설정 파일보다 우선한다.
const EnvToken = "VBR_VM_API_TOKEN"

// EnvGatewayToken 은 결과 보고에 싣는 토큰의 환경 변수 이름이다(ncc-xen 과 같은 이름).
const EnvGatewayToken = "GATEWAY_INTERNAL_TOKEN"

// UnauthorizedMessage 는 401 본문의 문장이다(ncc-xen 과 같은 문장).
const UnauthorizedMessage = "unauthorized: missing or invalid " + Header

// TokenFromEnv 는 환경 변수의 토큰을 돌려준다(없으면 빈 문자열).
func TokenFromEnv() string {
	return os.Getenv(EnvToken)
}

// Middleware 는 요청마다 current() 로 (켜짐 여부, 토큰)을 읽어 검사한다.
// 켜져 있는데 토큰이 비어 있으면 모든 요청을 거부한다(기동 때는 vmconfig 가 막는다).
func Middleware(current func() (enabled bool, token string)) gin.HandlerFunc {
	return func(c *gin.Context) {
		enabled, token := current()
		if !enabled {
			c.Next()
			return
		}
		got := []byte(c.GetHeader(Header))
		if token == "" || len(got) == 0 || subtle.ConstantTimeCompare(got, []byte(token)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": UnauthorizedMessage, "code": "UNAUTHORIZED"})
			return
		}
		c.Next()
	}
}
