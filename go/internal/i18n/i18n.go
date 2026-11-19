// copied from ncc-xen backend/internal/i18n/i18n.go @043c6d5 (CP-VM-10), 2026-11-19.
// 변경: 머리 주석만 더함. locale 감시(watch.go)는 복사하지 않고 vmconfig 가 SetLocale 을 부른다.
// Package i18n은 sendSuccess/sendFailure의 message 필드와 실행 로그(log.Info 등의
// 첫 인자)에 쓰이는 문구를 locale(ko/en/ja)에 따라 조회하는 기능을 제공합니다.
//
// 설계 원칙 (2026-07-15, stdlogger Entry 버그 수정과 동일한 교훈 반영):
//   - 번역 문자열을 어딘가에 미리 조립해서 캐시해두지 않는다. T()는 호출될 때마다
//     "현재" locale을 다시 읽어 조회한다. executor 생성 시점(main.go, 서비스 기동 시
//     1회)에 결과를 박제해두면, 이후 SetLocale로 언어가 바뀌어도 반영되지 않는
//     문제가 발생한다 — 이는 stdlogger의 Entry가 *slog.Logger를 생성 시점에
//     고정해 hot reload를 놓치던 것과 완전히 동일한 함정이다.
//   - 카탈로그(ko/en/ja)는 컴파일 시점에 정적으로 포함되므로(catalog_ko.go 등),
//     locale 전환은 파일을 다시 읽거나 재파싱할 필요 없이 단순히 "현재 활성
//     locale 코드"만 원자적으로 교체하면 된다 — stdlogger의 Build()+Replace()보다
//     훨씬 가볍다.
package i18n

import (
	"fmt"
	"sync/atomic"
)

// 지원 locale 코드
const (
	KO = "ko"
	EN = "en"
	JA = "ja"
)

// DefaultLocale은 sysconfig.yaml에 locale 항목이 없거나 알 수 없는 값일 때 사용합니다.
const DefaultLocale = KO

// catalogs는 catalog_ko.go / catalog_en.go / catalog_ja.go의 init()에서 채워집니다.
var catalogs = map[string]map[string]string{}

// registerCatalog는 catalog_*.go 파일들이 자신의 locale 맵을 등록할 때 사용합니다.
func registerCatalog(locale string, m map[string]string) {
	catalogs[locale] = m
}

// current는 현재 활성 locale 코드를 담는 원자적 값입니다.
// 기본값은 init()에서 DefaultLocale로 설정됩니다.
var current atomic.Value

func init() {
	current.Store(DefaultLocale)
}

// IsSupported는 해당 locale 코드에 대한 카탈로그가 등록되어 있는지 확인합니다.
func IsSupported(locale string) bool {
	_, ok := catalogs[locale]
	return ok
}

// SetLocale은 현재 활성 locale을 원자적으로 교체합니다.
// 지원하지 않는 locale이면 아무 것도 바꾸지 않고 false를 반환합니다.
//
// 이 함수는 여러 고루틴에서 동시에 호출해도 안전합니다(atomic.Value).
// 이미 진행 중인 요청은 자신이 T()를 호출하는 시점의 locale을 사용하므로,
// 교체 시점에 실행 중이던 job은 시작 시점이 아니라 "각 T() 호출 시점"의
// locale을 따릅니다 — job 도중 언어가 바뀌면 그 job의 message 안에서도
// 앞부분과 뒷부분의 언어가 섞일 수 있다는 뜻입니다. 이를 원치 않으면
// job 시작 시 CurrentLocale()로 스냅샷을 떠서 그 job 전용 T 함수를
// 만들어 쓰는 방식을 검토하세요(WithLocale 참고).
func SetLocale(locale string) bool {
	if !IsSupported(locale) {
		return false
	}
	current.Store(locale)
	return true
}

// CurrentLocale은 현재 활성 locale 코드를 반환합니다.
func CurrentLocale() string {
	if v := current.Load(); v != nil {
		return v.(string)
	}
	return DefaultLocale
}

// RawT는 key에 해당하는 현재 locale의 "원문 템플릿"을 그대로 반환합니다(포맷팅 없음).
//
// fmt.Errorf("...%w", err)처럼 %w(에러 체이닝) 지시자가 포함된 문자열은
// fmt.Sprintf가 이해하지 못하므로(T()가 내부적으로 Sprintf를 쓰면 "%!w(...)"로
// 깨짐), 이런 경우는 T() 대신 RawT()로 번역된 템플릿 문자열만 받아
// fmt.Errorf(i18n.RawT("key"), err) 형태로 fmt.Errorf 자신이 %w를 처리하게 합니다.
func RawT(key string) string {
	tmpl, ok := lookup(CurrentLocale(), key)
	if !ok {
		tmpl, ok = lookup(DefaultLocale, key)
	}
	if !ok {
		tmpl = key
	}
	return tmpl
}

// T는 key에 해당하는 현재 locale의 문구를 찾아 args로 포맷합니다.
// 현재 locale에 키가 없으면 ko(기본) 카탈로그로 폴백하고, 그마저 없으면
// key 문자열 자체를 반환합니다(운영 중 최소한의 정보 손실 방지).
func T(key string, args ...any) string {
	tmpl, ok := lookup(CurrentLocale(), key)
	if !ok {
		tmpl, ok = lookup(DefaultLocale, key)
	}
	if !ok {
		tmpl = key
	}
	if len(args) == 0 {
		return tmpl
	}
	return fmt.Sprintf(tmpl, args...)
}

// TL은 locale을 명시적으로 지정해 조회합니다. job 시작 시점의 locale을
// 끝까지 유지하고 싶은 경우(예: 4시간짜리 cross-pool 마이그레이션 도중
// locale이 바뀌어도 해당 job의 메시지는 처음 언어로 통일하고 싶을 때) 사용합니다.
func TL(locale, key string, args ...any) string {
	tmpl, ok := lookup(locale, key)
	if !ok {
		tmpl, ok = lookup(DefaultLocale, key)
	}
	if !ok {
		tmpl = key
	}
	if len(args) == 0 {
		return tmpl
	}
	return fmt.Sprintf(tmpl, args...)
}

func lookup(locale, key string) (string, bool) {
	m, ok := catalogs[locale]
	if !ok {
		return "", false
	}
	t, ok := m[key]
	return t, ok
}
