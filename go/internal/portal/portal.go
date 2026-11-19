// Package portal 은 VBR WEB Portal 로 이벤트 전문을 보낸다(원본 event_register / send_portal).
//
// 전문 형식(원본과 같다. 모두 ASCII 문자)
//
//	길이(7) : 뒤따르는 본문의 byte 수, 10진수 0 채움
//	본문    : msg_type(1)='B' + msg_seq(17)=YYYYMMDDHHMMSS+"001" + task_type(1)='0'
//	          + timeout(3)="000" + priority(1)='1' + JSON
//	JSON    : {"event_info":[{"event_type":"N","host_name":"...","host_uuid":"...",
//	          "event_time":"YYYY-MM-DD HH:MM:SS","event_code":"...","event_msg":"..."}]}
//
// 원본과 다른 점: 접속·송신·수신에 제한 시간을 두고, JSON 문자열 값의 특수 문자를 이스케이프한다.
package portal

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	connectTimeout = 5 * time.Second
	ioTimeout      = 3 * time.Second
	replyBuffer    = 4999
)

// Event 는 포털로 보내는 이벤트 한 건이다.
type Event struct {
	Type     byte // 'C' Critical, 'W' Warning, 'N' Notice
	HostName string
	HostUUID string
	Time     time.Time
	Code     string
	Message  string
}

// Client 는 포털 접속 정보다.
type Client struct {
	Host string
	Port int
}

// Address 는 "host:port" 다.
func (c Client) Address() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

// Encode 는 이벤트를 송신 전문으로 만든다.
func Encode(event Event) ([]byte, error) {
	var body strings.Builder
	body.WriteByte('B')
	body.WriteString(event.Time.Format("20060102150405"))
	body.WriteString("001")
	body.WriteByte('0')
	body.WriteString("000")
	body.WriteByte('1')

	body.WriteString(`{"event_info":[{`)
	fields := []struct{ key, value string }{
		{"event_type", string(event.Type)},
		{"host_name", event.HostName},
		{"host_uuid", event.HostUUID},
		{"event_time", event.Time.Format("2006-01-02 15:04:05")},
		{"event_code", event.Code},
		{"event_msg", event.Message},
	}
	for i, field := range fields {
		quoted, err := quote(field.value)
		if err != nil {
			return nil, err
		}
		if i > 0 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, "%q:%s", field.key, quoted)
	}
	body.WriteString("}]}")

	return []byte(fmt.Sprintf("%07d%s", body.Len(), body.String())), nil
}

// quote 는 JSON 문자열 값을 만든다. 원본 전문과 같도록 '<', '>', '&' 는 그대로 둔다.
func quote(text string) (string, error) {
	var out strings.Builder
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(text); err != nil {
		return "", err
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}

// Send 는 이벤트를 보내고 포털의 응답 본문을 돌려준다.
func (c Client) Send(event Event) (reply string, err error) {
	message, err := Encode(event)
	if err != nil {
		return "", err
	}
	conn, err := net.DialTimeout("tcp", c.Address(), connectTimeout)
	if err != nil {
		return "", fmt.Errorf("portal connect [%s]: %w", c.Address(), err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(ioTimeout)); err != nil {
		return "", err
	}
	if _, err := conn.Write(message); err != nil {
		return "", fmt.Errorf("portal send: %w", err)
	}
	buffer := make([]byte, replyBuffer)
	n, err := conn.Read(buffer)
	if err != nil && n == 0 {
		return "", fmt.Errorf("portal recv: %w", err)
	}
	return string(buffer[:n]), nil
}
