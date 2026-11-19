// 출처: ncc-xen backend/pkg/xapi/client.go (커밋 043c6d5, 2026-11-17 복사)
// 바뀐 부분: docs/agent/08-copied-code-register.md CP-02

package xapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Logger는 이 패키지가 남기는 진단 기록을 받는다. ncc-vbr의 logx.Logger가 만족한다.
// nil이면 기록하지 않는다.
type Logger interface {
	Printf(format string, args ...interface{})
}

func (c *Client) logf(format string, args ...interface{}) {
	if c.log != nil {
		c.log.Printf(format+"\n", args...)
	}
}

// Client는 XenServer XAPI XML-RPC 클라이언트입니다.
// PoC(vms 프로젝트)의 검증된 구조를 기반으로 확장합니다.
//
// 설계 원칙:
//   - 요청마다 Login/Logout 하지 않음 → 세션을 재사용
//   - mixin 패턴으로 도메인별 메서드 분리 (vm.go, pool.go, ...)
//   - 모든 XAPI 호출은 call() 을 통해 통일
//
// mu는 baseURL/sessionRef 필드를 보호합니다(2026-09-14, 옵션 C). 하나의
// *Client가 poolSession을 통해 여러 goroutine에 동시에 읽힐 수 있고,
// HOST_IS_SLAVE 리다이렉트(Login) 중에는 baseURL이, 로그인/로그아웃 중에는
// sessionRef가 쓰기 대상이 되므로 직접 필드 접근 대신 반드시 아래 접근자
// (SessionRef/BaseURL/SetBaseURL 등)를 통해서만 읽고 써야 합니다. call()이
// 만드는 실제 HTTP 요청·응답 대기 동안은 락을 잡지 않습니다(baseURL 값만
// 락 안에서 복사해 쓰고, 네트워크 I/O는 락 밖에서 수행).
type Client struct {
	mu               sync.RWMutex
	baseURL          string
	httpClient       *http.Client
	sessionRef       string
	log              Logger
	requestTimeout   time.Duration
	maxResponseBytes int64 // 2026-09-18: call()의 응답 크기 상한, SetMaxResponseBytes 참조
}

// NewClient는 새 클라이언트를 생성합니다.
// connectTimeout: TCP+TLS 연결 타임아웃 (기본 10초 권장)
// requestTimeout: 전체 요청 타임아웃 (로그인·XAPI 호출 포함)
func NewClient(baseURL string, verifySSL bool, connectTimeout, requestTimeout time.Duration, log Logger) *Client {
	dialer := &net.Dialer{Timeout: connectTimeout}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifySSL}, //nolint:gosec
		DialContext:     dialer.DialContext,
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Transport: tr,
			Timeout:   requestTimeout,
		},
		log:              log,
		requestTimeout:   requestTimeout,
		maxResponseBytes: defaultMaxRPCResponseBytes,
	}
}

// defaultMaxRPCResponseBytes는 XAPI XML-RPC 응답 본문을 메모리에 적재할 때
// 허용하는 기본 최대 크기입니다(2026-09-18, 코드 리뷰 지적 대응).
//
// 기존에는 io.ReadAll(resp.Body)에 상한이 없어, 비정상적으로 큰 응답
// (대형 Pool의 get_all_records / INFRA_SYNC full_sync, 또는 masterIp가
// 가리키는 대상이 실제로는 XenServer가 아니어서 예상 밖의 거대한 본문을
// 돌려주는 경우 등)이 순간적으로 메모리를 크게 점유해 GC pause나 OOM으로
// 이어질 수 있었습니다.
//
// 값 산정 근거: 매우 큰 Pool(VM 수천 대)이라도 단일 class의
// get_all_records 응답은 레코드 1개당 대략 수 KB 수준이라 전체로도 수십
// MB 선에서 그칠 것으로 추정됩니다. 256MB는 그보다 몇 배의 여유를 두면서도
// 진짜 비정상적인 응답(수백 MB~GB 단위)은 확실히 차단하는 값입니다. 특정
// Pool에서 정상 응답이 이 한도에 걸리는 사례가 실제로 나오면
// SetMaxResponseBytes()로 그 Client에 대해서만 조정할 수 있습니다.
const defaultMaxRPCResponseBytes int64 = 256 << 20 // 256 MiB

// SetMaxResponseBytes는 이 Client의 XAPI 응답 크기 상한을 재정의합니다.
// 0 이하를 넘기면 defaultMaxRPCResponseBytes로 되돌립니다. NewClient()로
// 생성된 모든 Client는 기본값을 그대로 사용하므로, 특정 Pool에 대해서만
// 한도를 조정하고 싶을 때만 호출하면 됩니다(현재 어떤 호출부도 이 메서드를
// 사용하지 않으므로, 지금 당장은 모든 Client가 기본값 256MiB를 씁니다).
func (c *Client) SetMaxResponseBytes(n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n <= 0 {
		n = defaultMaxRPCResponseBytes
	}
	c.maxResponseBytes = n
}

func (c *Client) getMaxResponseBytes() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.maxResponseBytes
}

// checkResponseSize는 읽어들인 응답 바이트 수 n이 limit을 초과했는지
// 판정합니다. call()에서 분리한 순수 함수라 HTTP 없이 바로 테스트할 수
// 있습니다.
func checkResponseSize(method string, n int, limit int64) error {
	if int64(n) > limit {
		return fmt.Errorf("XAPI response too large (method=%s, limit=%d bytes)", method, limit)
	}
	return nil
}

// Accessors
func (c *Client) SessionRef() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionRef
}
func (c *Client) BaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}
func (c *Client) SetBaseURL(u string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL = u
}
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionRef != ""
}

// Close는 이 Client의 유휴 HTTP 커넥션을 정리합니다.
//
// NewClient()는 호출마다 새 http.Transport를 생성하는데, Transport의
// IdleConnTimeout이 기본값(0=무제한)이라 유휴 커넥션과 그 처리 goroutine이
// 명시적으로 닫지 않으면 영원히 남습니다(Transport 자체가 GC돼도 자동으로
// 정리되지 않음 — net/http의 잘 알려진 동작). 이 Client를 더 이상 쓰지 않고
// 폐기할 때(재연결로 새 Client를 만들어 교체할 때, Pool 등록 해제 시 등)는
// 반드시 이 메서드를 호출해야 합니다.
// (2026-09-15 — Pool 등록/삭제 반복 시 goroutine이 계속 증가하는 문제의
//
//	실제 원인으로 확인됨: connectAndListen()이 재연결마다 새 Client를 만들며
//	이전 Client의 Transport를 그대로 버려 유휴 커넥션이 계속 누적되었음.)
func (c *Client) Close() {
	if tr, ok := c.httpClient.Transport.(*http.Transport); ok {
		tr.CloseIdleConnections()
	}
}

// ─────────────────────────────────────────────
// 세션 관리
// ─────────────────────────────────────────────

// Login은 XenServer에 로그인합니다.
// HOST_IS_SLAVE 오류 시 master IP로 자동 리다이렉트합니다.
func (c *Client) Login(ctx context.Context, username, password string) error {
	result, err := c.call(ctx, "session.login_with_password",
		username, password, "1.0", "ncc-vbr-agent")
	if err != nil {
		// HOST_IS_SLAVE: master IP로 재시도
		if masterIP := extractMasterIP(err.Error()); masterIP != "" {
			c.logf("slave host detected, redirecting to master. master_ip=%s", masterIP)
			c.SetBaseURL("https://" + masterIP + ":443")
			result, err = c.call(ctx, "session.login_with_password",
				username, password, "1.0", "ncc-vbr-agent")
			if err != nil {
				return fmt.Errorf("master login failed: %w", err)
			}
		} else {
			return fmt.Errorf("login failed: %w", err)
		}
	}

	ref := result.Str()
	if !strings.HasPrefix(ref, "OpaqueRef:") {
		return fmt.Errorf("invalid session reference format: %q", ref)
	}

	c.mu.Lock()
	c.sessionRef = ref
	c.mu.Unlock()
	return nil
}

// Logout은 세션을 종료합니다.
func (c *Client) Logout(ctx context.Context) error {
	sessionRef := c.SessionRef()
	if sessionRef == "" {
		return nil
	}
	_, err := c.call(ctx, "session.logout", sessionRef)
	c.mu.Lock()
	c.sessionRef = ""
	c.mu.Unlock()
	return err
}

// ─────────────────────────────────────────────
// 내부 RPC 호출 (모든 mixin에서 사용)
// ─────────────────────────────────────────────

// Call은 외부(mixin)에서 사용하는 공개 RPC 호출 메서드입니다.
func (c *Client) Call(ctx context.Context, method string, args ...interface{}) (Value, error) {
	return c.call(ctx, method, args...)
}

// call은 XML-RPC 메서드를 호출하고 XAPI 래퍼를 벗겨 실제 Value를 반환합니다.
func (c *Client) call(ctx context.Context, method string, args ...interface{}) (Value, error) {
	body, err := encodeCall(method, args)
	if err != nil {
		return nilVal(), fmt.Errorf("request encoding failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL(), bytes.NewReader(body))
	if err != nil {
		return nilVal(), fmt.Errorf("HTTP request creation failed: %w", err)
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("User-Agent", "ncc-vbr-agent/3.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nilVal(), fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// 2026-09-18: io.ReadAll(resp.Body)에 상한이 없으면 비정상적으로 큰
	// 응답이 메모리를 무제한 점유할 수 있어 io.LimitReader로 상한을 둔다.
	// 실제 상한(limit)보다 1바이트 더 읽어(limit+1) 응답이 정확히 상한과
	// 같은 크기인 정상 케이스와 상한을 "초과"해서 잘린 케이스를 구분한다
	// (정확히 limit 크기면 len(respBody)==limit이라 checkResponseSize에
	// 안 걸림).
	limit := c.getMaxResponseBytes()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nilVal(), fmt.Errorf("response read failed: %w", err)
	}
	if sizeErr := checkResponseSize(method, len(respBody), limit); sizeErr != nil {
		return nilVal(), sizeErr
	}

	if resp.StatusCode != 200 {
		preview := respBody
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return nilVal(), fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(preview))
	}

	raw, err := parseMethodResponse(respBody)
	if err != nil {
		return nilVal(), fmt.Errorf("XML-RPC parsing failed: %w", err)
	}

	return c.unwrapXAPIResponse(raw)
}

// redactSessionRefInLog는 디버그 로그용 XML 문자열 s에서 XAPI 호출의 session
// ref가 그대로 노출되지 않도록 치환합니다.
//
// 2026-09-18: migrate 관련 디버그 로그(XML-RPC 요청/응답 원문)에 살아있는
// 세션 토큰(session_ref, OpaqueRef 형식)이 평문으로 남아, 그 로그를 확보한
// 사람이 세션 만료 전까지 해당 세션 권한으로 XAPI를 호출할 수 있는 위험이
// 있었습니다. args[0]은 XAPI 호출 관례상 항상 session ref입니다(이 함수를
// 쓰는 migrate 계열 호출은 모두 인증된 세션으로 호출되며, session ref 없이
// 호출하는 session.login_with_password는 이 분기를 타지 않음). OpaqueRef
// 형식은 VM/host 등 다른 오브젝트 참조에도 쓰이므로 "OpaqueRef:" 전체를
// 마스킹하지 않고, 이 호출에 실제로 쓰인 session ref 값 하나만 정확히
// 문자열 치환해 나머지 디버깅 정보(VM/host/network 참조 등)는 그대로
// 유지합니다.
func redactSessionRefInLog(s string, args []interface{}) string {
	if len(args) == 0 {
		return s
	}
	sref, ok := args[0].(string)
	if !ok || sref == "" {
		return s
	}
	return strings.ReplaceAll(s, sref, "***REDACTED-SESSION-REF***")
}

// XAPIError는 XAPI Failure 응답을 구조화해서 담습니다. Code는 XenAPI
// 프로토콜상 ErrorDescription의 첫 번째 원소(기계 판독용 에러 코드, 예:
// "SESSION_INVALID", "VM_BAD_POWER_STATE")이고 Params는 나머지입니다.
// Error()는 기존과 완전히 동일한 문자열을 반환하므로(로그 출력 등 기존
// 호출부는 영향 없음), Code/Params는 IsSessionError() 같은 분류용으로만
// 추가된 필드입니다.
//
// (2026-09-15 — 기존에는 XAPI 에러를 fmt.Errorf로 문자열에 뭉쳐버려서
// 호출부가 "세션 문제인지 업무 로직 에러인지" 구분할 방법이 없었고, 그
// 결과 CONTROL 계열 25곳이 전부 "에러가 나면 무조건 세션 무효화"로 처리
// 되어 있었음. VM_BAD_POWER_STATE 같은 세션과 무관한 에러도 불필요하게
// 재연결을 유발해, 동일 Pool의 다른 동시 요청이 그 재연결을 기다리게
// 되는 비용이 있었음.)
type XAPIError struct {
	Code    string
	Params  []string
	message string
}

func (e *XAPIError) Error() string { return e.message }

// 세션 자체가 무효화됐다고 볼 수 있는 XAPI 에러 코드입니다. 이 목록에
// 없는 에러(VM_BAD_POWER_STATE, HANDLE_INVALID, SR_FULL, HOST_IN_USE 등
// 업무 로직 에러)는 세션과 무관하므로 재연결을 유발하지 않아야 합니다.
var sessionErrorCodes = map[string]bool{
	"SESSION_INVALID":               true,
	"SESSION_AUTHENTICATION_FAILED": true,
	"SESSION_AUTHORIZATION_FAILED":  true,
	"SESSION_NOT_REGISTERED":        true,
}

// IsSessionError는 err가 세션 자체의 무효화를 나타내는 XAPI 에러인지
// 판단합니다. XAPIError가 아니거나(HTTP 레벨 에러, 타임아웃 등) 알려진
// 세션 에러 코드가 아니면 false를 반환합니다 — 그 경우 호출부는 세션을
// 무효화하지 말고 실패만 보고해야 합니다(다음 요청은 기존 세션을 그대로
// 재사용 — EnsureConnected의 사전 probe가 실제로 죽은 세션은 어차피
// 걸러냅니다).
func IsSessionError(err error) bool {
	var xe *XAPIError
	if errors.As(err, &xe) {
		return sessionErrorCodes[xe.Code]
	}
	return false
}

// unwrapXAPIResponse는 XAPI의 {Status, Value} 래퍼를 벗겨냅니다.
func (c *Client) unwrapXAPIResponse(v Value) (Value, error) {
	if v.kind == kindStruct {
		status := v.Field("Status").Str()
		switch status {
		case "Success":
			return v.Field("Value"), nil
		case "Failure":
			errDesc := v.Field("ErrorDescription")
			if errDesc.kind == kindArray && len(errDesc.Arr()) > 0 {
				parts := make([]string, 0, len(errDesc.Arr()))
				for _, item := range errDesc.Arr() {
					parts = append(parts, item.Str())
				}
				return nilVal(), &XAPIError{
					Code:    parts[0],
					Params:  parts[1:],
					message: fmt.Sprintf("XAPI Error: %s", strings.Join(parts, " | ")),
				}
			}
			return nilVal(), &XAPIError{
				Code:    errDesc.Str(),
				message: fmt.Sprintf("XAPI Failure: %s", errDesc.Str()),
			}
		case "":
			return v, nil
		default:
			return nilVal(), fmt.Errorf("Unknown XAPI status: %s", status)
		}
	}
	return v, nil
}

// extractMasterIP는 HOST_IS_SLAVE 오류에서 master IP를 추출합니다.
//
// XAPI 오류 형태:
//
//	"XAPI Error: HOST_IS_SLAVE 192.168.x.x"  (공백 구분)
//	"XAPI Error: HOST_IS_SLAVE | 192.168.x.x" (파이프+공백 구분)
func extractMasterIP(errMsg string) string {
	if !strings.Contains(errMsg, "HOST_IS_SLAVE") {
		return ""
	}
	// 공백 기준으로 분리한 뒤 HOST_IS_SLAVE 이후 토큰 중 IP를 찾는다.
	// 파이프(|) 구분 시 parts = ["HOST_IS_SLAVE", "|", "192.168.x.x"] 순서이므로
	// "|" 같은 구분자 토큰은 건너뛰고 실제 IP 후보를 탐색한다.
	parts := strings.Fields(errMsg)
	for i, p := range parts {
		if p == "HOST_IS_SLAVE" {
			for j := i + 1; j < len(parts); j++ {
				candidate := strings.Trim(parts[j], "|,")
				if strings.Count(candidate, ".") == 3 {
					return candidate
				}
			}
		}
	}
	return ""
}

// ─────────────────────────────────────────────
// Pool Master 조회 (Sync Worker 용)
// ─────────────────────────────────────────────

// GetPoolMasterAddress는 Pool Master 호스트의 IP를 반환합니다.
// Sync Worker가 연결 시 slave → master 리다이렉트에 사용합니다.
func (c *Client) GetPoolMasterAddress(ctx context.Context) (string, error) {
	poolsVal, err := c.call(ctx, "pool.get_all", c.SessionRef())
	if err != nil {
		return "", fmt.Errorf("pool.get_all failed: %w", err)
	}

	pools := poolsVal.ToStringSlice()
	if len(pools) == 0 {
		return "", fmt.Errorf("pool reference value is missing")
	}

	masterRefVal, err := c.call(ctx, "pool.get_master", c.SessionRef(), pools[0])
	if err != nil {
		return "", fmt.Errorf("pool.get_master failed: %w", err)
	}

	addrVal, err := c.call(ctx, "host.get_address", c.SessionRef(), masterRefVal.Str())
	if err != nil {
		return "", fmt.Errorf("host.get_address failed: %w", err)
	}

	return addrVal.Str(), nil
}

// IsMasterRedirectError는 HOST_IS_SLAVE 오류에서 master IP를 추출합니다.
// PoC의 IsMasterRedirectError와 동일한 로직입니다.
func IsMasterRedirectError(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	ip := extractMasterIP(err.Error())
	return ip, ip != ""
}
