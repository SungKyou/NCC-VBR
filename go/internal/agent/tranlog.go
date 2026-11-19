package agent

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ncc-vbr/internal/logx"
)

// TranLog 는 원본 TRAN_LOG2 의 월별 transaction 로그다.
//
// 파일: ./transaction-YYYYMM.log (YYYYMM = 기록하는 전문의 전문번호 앞 6자리)
// 한 건: "\x00[YYYY-MM-DD HH:MM:SS:mmm]<전문번호> <단계>:<문구>\n\x00<전문>\x00\n"
// 크기로 바꾸지 않는다(원본도 주석 처리되어 있다).
//
// 결과 조회(Q)가 이 파일을 읽어 응답한다(원본 query_task). 그래서 형식이 바뀌면 안 된다.
type TranLog struct {
	log *logx.Logger
	dir string
}

// tranFilePrefix 는 원본 TRAN_LOG_FILE("./transaction") 이다.
const tranFilePrefix = "transaction"

func NewTranLog(log *logx.Logger, dir string) *TranLog {
	return &TranLog{log: log, dir: dir}
}

// Write 는 한 건을 쓴다. data 는 'B' 로 시작하는 전문이다.
func (t *TranLog) Write(step byte, title string, data []byte) {
	msg := Message{Raw: data}
	seq := msg.Seq()
	now := t.log.Now()
	var b bytes.Buffer
	b.WriteByte(0)
	b.WriteString(stamp(now))
	b.WriteString(seq)
	b.WriteByte(' ')
	b.WriteByte(step)
	b.WriteByte(':')
	b.WriteString(title)
	b.WriteByte('\n')
	b.WriteByte(0)
	b.WriteString(cstr(data))
	b.WriteByte(0)
	b.WriteByte('\n')
	t.log.Append(tranFileName(seq), b.String(), false, "Transaction Log file open error")
}

func stamp(now time.Time) string {
	return fmt.Sprintf("[%s:%03d]", now.Format("2006-01-02 15:04:05"), now.Nanosecond()/1_000_000)
}

// tranFileName 은 원본 sprintf("%s-%.6s.log", TRAN_LOG_FILE, seq) 다.
func tranFileName(seq string) string {
	if len(seq) > 6 {
		seq = seq[:6]
	}
	return tranFilePrefix + "-" + seq + ".log"
}

// fgetsLimit 는 원본 query_task 의 fgets(buffer, MAX_LENGTH=256, fp) 다. 한 번에 255바이트까지 읽는다.
const fgetsLimit = 255

// Query 는 원본 query_task 다. 전문번호 seq 의 마지막 기록을 찾아 (단계 기호, 본문) 을 돌려준다.
// found 가 거짓이면 code 는 '9', text 는 seq 그대로다(원본은 msg_text 를 바꾸지 않았다).
// 파일을 열지 못하면 ok 가 거짓이다(원본 return FALSE).
func (t *TranLog) Query(seq string) (code byte, text string, ok bool) {
	code, text = CodeError, seq
	path := filepath.Join(t.dir, tranFileName(seq))
	fp, err := os.Open(path)
	if err != nil {
		t.log.Printf("Transaction Log file open error.[./%s]\n", tranFileName(seq))
		return code, text, false
	}
	defer fp.Close()

	r := bufio.NewReader(fp)
	find, matched := false, false
	var acc []byte
	for {
		line, err := fgets(r)
		if len(line) == 0 && err != nil {
			break
		}
		if !(len(line) >= 2 && line[0] == 0 && line[1] == '[') {
			if find {
				acc = append(acc, cstrBytes(line)...)
			}
			continue
		}
		// 로그 머리. 전문번호는 26번째부터 17자리, 단계 기호는 44번째다.
		if len(line) < 45 || string(line[26:43]) != seq {
			find = false
			continue
		}
		find, matched = true, true
		code = line[44]
		acc = acc[:0]
		next, _ := fgets(r)
		switch {
		case len(next) > 1 && next[0] == 0 && next[1] == 'B':
			if len(next) > 24 {
				acc = append(acc, cstrBytes(next[24:])...)
			}
		case len(next) > 0 && next[0] == 0:
			if len(next) > 8 {
				acc = append(acc, cstrBytes(next[8:])...)
			}
		default:
			acc = append(acc, cstrBytes(next)...)
		}
	}
	if matched {
		text = string(acc)
	}
	return code, text, true
}

// fgets 는 줄바꿈까지 또는 255바이트까지 읽는다(줄바꿈 포함).
func fgets(r *bufio.Reader) ([]byte, error) {
	var out []byte
	for len(out) < fgetsLimit {
		c, err := r.ReadByte()
		if err != nil {
			return out, err
		}
		out = append(out, c)
		if c == '\n' {
			break
		}
	}
	return out, nil
}

func cstrBytes(b []byte) []byte {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return b[:i]
	}
	return b
}
