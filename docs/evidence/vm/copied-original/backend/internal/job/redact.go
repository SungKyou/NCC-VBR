package job

import "strings"

// sensitiveKeySubstrings — payload.options 내 값을 로그에 남기기 전에
// 마스킹해야 하는 키 이름의 (대소문자 무시) 부분 문자열 목록입니다.
//
// 2026-09-18: handler.go의 규약 1 수신 전문 로그(pretty print, Info 레벨)가
// req 전체를 마스킹 없이 그대로 기록하고 있어, jobType에 따라
// payload.options에 담기는 POOL_QUERY/pool_join의 password/master_password,
// cross_pool_copy·cross_pool_move의 dst_password, SR_CONTROL iSCSI의
// chap_password 등 인프라 자격증명이 로그 파일(및 그 백업, 지원 번들)에
// 평문으로 남는 문제가 있었습니다.
//
// 키 이름 부분 문자열 매칭 방식을 택한 이유: jobType별 옵션 필드를 일일이
// 나열(allowlist/denylist)하면 새 jobType이나 옵션이 추가될 때마다 이 목록도
// 함께 갱신해야 하는데, 누락되면 그 즉시 조용히 로그에 새 다시 노출됩니다.
// 키 이름에 아래 부분 문자열이 포함되는지만 보는 방식은 필드가 늘어나도
// 대체로 자동으로 커버됩니다(다만 완전히 새로운 명명 패턴이 생기면 이
// 목록에 추가해야 함 — 만능은 아닙니다).
var sensitiveKeySubstrings = []string{
	"password", "passwd", "secret", "token", "apikey", "api_key",
	"credential", "sessionref", "session_ref",
}

// isSensitiveKey는 key(대소문자 무시)에 sensitiveKeySubstrings 중 하나라도
// 포함되면 true를 반환합니다.
func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, s := range sensitiveKeySubstrings {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// redactSensitive는 JSON 디코딩 결과로 나올 수 있는 임의의 트리
// (map[string]interface{} / []interface{} / 스칼라)를 재귀적으로 순회하며,
// 키 이름이 isSensitiveKey에 매칭되는 값을 "***REDACTED***" 문자열로 치환한
// 새 트리를 반환합니다.
//
// 원본은 절대 변경하지 않습니다 — map/slice는 참조 타입이라 얕은 복사만
// 하면 안에서 값을 바꿀 때 원본까지 같이 바뀌므로, 매칭되지 않는 경우에도
// 항상 새 map/slice를 만들어 채웁니다. template_create의 vbd_configs[] /
// networks[] 처럼 배열 안에 다시 map이 들어있는 중첩 구조도 재귀로 처리되어
// 깊이에 상관없이 동작합니다.
func redactSensitive(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			if isSensitiveKey(k) {
				out[k] = "***REDACTED***"
				continue
			}
			out[k] = redactSensitive(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, val := range t {
			out[i] = redactSensitive(val)
		}
		return out
	default:
		// string/float64/bool/nil 등 스칼라는 그대로 반환
		return v
	}
}

// redactedRequestForLogging는 req를 로그 전용으로만 사용할 사본으로
// 반환합니다 — payload.options만 redactSensitive()를 거친 새 map으로
// 교체하고 나머지 필드(taskId/jobType/action 등, 원래도 비밀정보 없음)는
// 그대로 둡니다.
//
// 반환값은 로깅에만 사용해야 합니다. 실제 실행(Executor.Execute)에는 이
// 함수를 거치지 않은 원본 req를 그대로 넘겨야 합니다 — 마스킹된 값이 실제
// XAPI 호출에 들어가면 인증 실패 등으로 기능이 깨집니다.
func redactedRequestForLogging(req Request) Request {
	redacted := req
	if req.Payload.Options != nil {
		if m, ok := redactSensitive(req.Payload.Options).(map[string]interface{}); ok {
			redacted.Payload.Options = m
		}
	}
	return redacted
}
