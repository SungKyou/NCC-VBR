// Package apiauth는 cmd/server·cmd/analyzer의 인바운드 HTTP API 토큰 인증입니다
// (🆕 2026-10-29, 코드 검토 ①).
//
// Backend → Middleware 방향(internal/gateway)도 같은 X-Internal-Token 헤더를 씁니다.
// 🔄 2026-11-02 Middleware 협의 결과로 토큰을 방향별 2개로 나눔(internal/config APIAuthConfig):
//   - Middleware → Backend: sysconfig.yaml api_auth.NCC_XEN_API_TOKEN — 이 패키지가 검사
//   - Backend → Middleware: api_auth.GATEWAY_INTERNAL_TOKEN — internal/gateway가 실음
//
// 같은 이름의 환경 변수가 있으면 우선합니다(internal/config applyEnvOverrides).
//
//   - api_auth.enabled=false: 인증 없음(기동 시 경고 로그만).
//   - enabled=true: /api/v1/*, /debug/* 요청에 같은 값의 X-Internal-Token 헤더가
//     없으면 401. /health는 모니터링용으로 계속 열어 둡니다.
package apiauth

import (
	"crypto/subtle"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// Header는 토큰을 싣는 HTTP 헤더 이름입니다(internal/gateway와 동일).
const Header = "X-Internal-Token"

// EnvToken은 인바운드 API 토큰을 담는 환경 변수 이름입니다.
const EnvToken = "NCC_XEN_API_TOKEN"

// TokenFromEnv는 환경 변수의 토큰을 반환합니다(없으면 빈 문자열).
func TokenFromEnv() string {
	return os.Getenv(EnvToken)
}

// Middleware는 token이 비어 있으면 아무것도 검사하지 않고, 아니면
// X-Internal-Token 헤더를 상수 시간 비교로 확인합니다.
func Middleware(token string) gin.HandlerFunc {
	if token == "" {
		return func(c *gin.Context) { c.Next() }
	}
	want := []byte(token)
	return func(c *gin.Context) {
		got := []byte(c.GetHeader(Header))
		if len(got) == 0 || subtle.ConstantTimeCompare(got, want) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: missing or invalid " + Header})
			return
		}
		c.Next()
	}
}
