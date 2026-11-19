// Package agent 는 NCC-VBR agent(원본 vbr_agent.c 1.602)의 전환본이다.
//
// 원본과 다른 점의 중심(docs/agent/00-rescope-review.md):
//   - dom0가 아니라 별도 Linux 서버에서 실행하고 XAPI로 풀 마스터를 부른다.
//   - 백업(BACKUP), 부분 백업(PARTIAL), 복구(RESTORE), 결과 조회(Q), 취소(C)만 처리한다.
//     동기 명령(S), 작업자 수 조정(T), 문의(I)는 결과 코드 9로 거절한다.
//   - 상태 감시, 이벤트, 메시지 큐, 공유 메모리, dummy2가 없다.
package agent

import (
	"bytes"
	"fmt"
)

// 전문 틀. 원본 BACKUP_INFO 구조체의 msg_type ~ priority 다.
const (
	HeaderLen  = 23 // 'B' + 전문번호 17 + 작업구분 1 + 제한시간 3 + 우선순위 1
	posSeq     = 1
	seqLen     = 17
	posTask    = 18
	posTimeout = 19
	posPrio    = 22
	lengthLen  = 7 // 앞에 붙는 길이 자리
	maxBodyLen = 300000 - HeaderLen
)

// 결과 코드(응답의 작업구분 자리)
const (
	CodeOK         = '0'
	CodeError      = '9'
	CodeQueueFull  = '8'
	CodeProcessing = '3'
)

// Message 는 받은 전문 하나다(앞의 길이 7자리는 뺀 것).
type Message struct {
	Raw []byte
}

// at 은 i 번째 바이트다. 전문이 짧으면 0 을 돌려준다(원본은 0으로 지운 구조체에 복사했다).
func (m Message) at(i int) byte {
	if i < len(m.Raw) {
		return m.Raw[i]
	}
	return 0
}

// Seq 는 전문번호 17자리다. 원본처럼 NUL 앞까지만 쓴다.
func (m Message) Seq() string {
	return cstr(m.field(posSeq, seqLen))
}

// SeqBytes 는 비교용 전문번호 17바이트다(짧으면 0 으로 채운다). 원본은 memcmp 17바이트로 비교했다.
func (m Message) SeqBytes() [seqLen]byte {
	var out [seqLen]byte
	copy(out[:], m.field(posSeq, seqLen))
	return out
}

// Task 는 작업 구분이다.
func (m Message) Task() byte { return m.at(posTask) }

// Priority 는 우선순위 숫자다. 원본은 atol("%c") 였으므로 숫자가 아니면 0 이다.
func (m Message) Priority() int {
	p := m.at(posPrio)
	if p >= '0' && p <= '9' {
		return int(p - '0')
	}
	return 0
}

// Body 는 본문이다. 원본은 C 문자열로 다뤘으므로 첫 NUL 앞까지다.
func (m Message) Body() string {
	if len(m.Raw) <= HeaderLen {
		return ""
	}
	return cstr(m.Raw[HeaderLen:])
}

func (m Message) field(pos, n int) []byte {
	if pos >= len(m.Raw) {
		return nil
	}
	end := pos + n
	if end > len(m.Raw) {
		end = len(m.Raw)
	}
	return m.Raw[pos:end]
}

// header 는 응답에 쓸 머리 23바이트다. 짧은 전문이면 0 으로 채운다.
func (m Message) header() []byte {
	h := make([]byte, HeaderLen)
	copy(h, m.Raw)
	return h
}

// Reply 는 요청의 머리에 결과 코드와 본문을 붙인 응답이다(원본 1+17+1+3+1+strlen(text)).
func (m Message) Reply(code byte, text string) []byte {
	h := m.header()
	h[posTask] = code
	return append(h, text...)
}

// Echo 는 받은 전문을 그대로 돌려주되 작업구분 자리만 code 로 바꾼 것이다(형식 오류, 대기열 가득).
func (m Message) Echo(code byte) []byte {
	out := append([]byte(nil), m.Raw...)
	if len(out) > posTask {
		out[posTask] = code
	}
	return out
}

// Frame 은 앞에 길이 7자리를 붙인다.
func Frame(b []byte) []byte {
	return append([]byte(fmt.Sprintf("%07d", len(b))), b...)
}

func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}
