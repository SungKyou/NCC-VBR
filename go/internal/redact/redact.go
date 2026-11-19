// copied from ncc-xen backend/internal/job/redact.go @043c6d5 (CP-VM-03), 2026-11-19.
// 바뀐 부분(docs/vm/08-copied-code-register.md CP-VM-03):
//   - 패키지를 job 에서 redact 로 옮기고 함수를 공개했다(Value, IsSensitiveKey).
//   - 가린 값을 "***REDACTED***" 대신 "***" 로 쓴다(05 문서 8.1).
//   - Request 구조체 전용 함수(redactedRequestForLogging)는 vm 패키지 쪽에 새로 썼다.

// Package redact 는 로그·작업 기록·응답에 남기기 전에 비밀 값을 가린다.
//
// 키 이름에 아래 부분 문자열이 들어 있으면(대소문자 무시) 값을 가린다.
// 필드 목록을 나열하는 방식보다 새 필드가 생겨도 대체로 자동으로 덮인다(ncc-xen 2026-09-18 결정).
package redact

import "strings"

// Mask 는 가린 값 대신 쓰는 문자열이다.
const Mask = "***"

var sensitiveKeySubstrings = []string{
	"password", "passwd", "secret", "token", "apikey", "api_key",
	"credential", "sessionref", "session_ref",
}

// IsSensitiveKey 는 key 에 비밀 값을 뜻하는 부분 문자열이 있으면 참이다.
func IsSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, s := range sensitiveKeySubstrings {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// Value 는 JSON 디코딩 결과 트리(map / slice / 스칼라)를 돌며 비밀 키의 값을 가린 새 트리를 돌려준다.
// 원본은 바꾸지 않는다. "credentials" 처럼 키 이름이 비밀이면 그 아래 전체를 가린다.
func Value(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			if IsSensitiveKey(k) {
				out[k] = Mask
				continue
			}
			out[k] = Value(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, val := range t {
			out[i] = Value(val)
		}
		return out
	default:
		return v
	}
}
