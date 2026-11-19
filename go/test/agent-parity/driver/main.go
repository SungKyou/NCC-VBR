// driver 는 원본 agent(C)와 전환본(Go)에 같은 요청을 보내 응답과 기록을 비교한다.
//
//	driver -c 127.0.0.1:7101 -cdir /p/c -go 127.0.0.1:7102 -godir /p/go
//
// 판정: PASS(같다), EXPECTED(의도한 차이, 변경 대장 번호), FAIL(다르다). FAIL 이 있으면 종료 코드 1.
package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	vmUUID = "22acc256-b431-f9df-6c44-b1f71e7912fc"
	srUUID = "9ad088e0-d03d-0269-0b7f-e40006aa5014"
)

type side struct {
	name  string
	addr  string
	dir   string
	root  string
	obs   map[string]string
	keys  []string
	seqs  []string
	mu    sync.Mutex
	stray []string
	polls int
}

func (s *side) put(key, val string) {
	if _, ok := s.obs[key]; !ok {
		s.keys = append(s.keys, key)
	}
	val = strings.ReplaceAll(val, s.root, "<ROOT>")
	if strings.HasPrefix(val, "B2026111790") && len(val) >= 18 {
		val = "B<POLL-SEQ>" + val[18:]
	}
	s.obs[key] = val
}

func (s *side) send(msg string) string {
	c, err := net.DialTimeout("tcp", s.addr, 5*time.Second)
	if err != nil {
		return "DIAL-ERROR " + err.Error()
	}
	defer c.Close()
	if _, err := c.Write([]byte(fmt.Sprintf("%07d%s", len(msg), msg))); err != nil {
		return "WRITE-ERROR"
	}
	want := ""
	if len(msg) >= 18 {
		want = msg[1:18]
	}
	// 원본은 비동기 결과를 끊긴 접속의 fd 번호로 쓴다(R-AG-16). 그 번호를 이 접속이 받았으면
	// 남의 결과가 먼저 올 수 있으므로, 전문번호가 맞는 응답까지 읽고 나머지는 stray 로 남긴다.
	for i := 0; i < 5; i++ {
		_ = c.SetReadDeadline(time.Now().Add(15 * time.Second))
		head := make([]byte, 7)
		if _, err := io.ReadFull(c, head); err != nil {
			return "READ-ERROR " + err.Error()
		}
		n, _ := strconv.Atoi(string(head))
		body := make([]byte, n)
		if _, err := io.ReadFull(c, body); err != nil {
			return "READ-ERROR " + err.Error()
		}
		if len(body) < 18 || string(body[1:18]) == want || msg[0] != 'B' {
			return string(body)
		}
		s.mu.Lock()
		s.stray = append(s.stray, fmt.Sprintf("on %s got %s", want, strings.ReplaceAll(string(body), s.root, "<ROOT>")))
		s.mu.Unlock()
	}
	return "TOO-MANY-STRAY"
}

// qseq 는 조회 요청의 전문번호다. 관리 서버처럼 요청마다 새 번호를 쓴다
// (원본은 전문번호로 칸을 찾아 응답하므로 번호를 다시 쓰면 앞의 접속으로 응답이 간다).
func (s *side) qseq(key string) string {
	h := 0
	for _, c := range key {
		h = (h*31 + int(c)) % 10000
	}
	s.mu.Lock()
	s.polls++
	n := s.polls
	s.mu.Unlock()
	return fmt.Sprintf("2026111790%04d%03d", h, n%1000)
}

// final 은 결과 조회 코드가 대기·처리 중(1, 2, 3)이 아닐 때까지 묻는다.
func (s *side) final(key, target string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for {
		r := s.send("B" + s.qseq(key) + "Q0001" + target)
		if len(r) > 18 && !strings.ContainsRune("123", rune(r[18])) {
			return r
		}
		if time.Now().After(deadline) {
			return "TIMEOUT " + r
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func (s *side) backup(seq, prio, name, file string) string {
	s.seqs = append(s.seqs, seq)
	return s.send("B" + seq + "A000" + prio + "BACKUP " + vmUUID + " " + name + " " + s.root + "/" + file)
}

func scenario(s *side) {
	// P00 원본 상태 감시가 Active 가 될 때까지 첫 백업으로 기다린다
	s.put("P00 accept", s.backup("20261117100000000", "5", "17", "17.xva"))
	s.put("P00 result", s.final("P00", "20261117100000000", 90*time.Second))

	// P01 형식 오류
	s.put("P01 format error", s.send("X20261117100000001A0005BACKUP x"))

	// P02 인자 오류, P03 복구 인자 오류(2015 표본과 같은 입력)
	s.seqs = append(s.seqs, "20261117100000002", "20261117100000003")
	s.send("B20261117100000002A0005BACKUP 1234 s " + s.root + "/x.xva")
	s.put("P02 bad uuid result", s.final("P02", "20261117100000002", 20*time.Second))
	s.send("B20261117100000003A0005RESTORE " + s.root + "/17.xva  " + srUUID)
	s.put("P03 restore args result", s.final("P03", "20261117100000003", 20*time.Second))

	// P04 처리 중·대기 중 조회와 취소 (작업자 3개)
	for i, seq := range []string{"20261117100000005", "20261117100000006", "20261117100000007", "20261117100000008", "20261117100000009"} {
		s.put("P04 accept "+seq, s.backup(seq, "5", "slow", fmt.Sprintf("s%d.xva", i)))
	}
	time.Sleep(1500 * time.Millisecond)
	s.put("P04 query running", s.send("B20261117100000010Q0001"+"20261117100000005"))
	s.put("P04 query queued", s.send("B20261117100000011Q0001"+"20261117100000008"))
	s.put("P04 cancel running", s.send("B20261117100000012C0001"+"20261117100000005"))
	s.put("P04 cancel queued", s.send("B20261117100000013C0001"+"20261117100000008"))
	s.put("P04 query canceled", s.send("B20261117100000014Q0001"+"20261117100000008"))
	s.put("P04 query not found", s.send("B20261117100000015Q0001"+"20261117109999999"))
	s.put("P04 query short", s.send("B20261117100000016Q0001"+"123"))
	s.put("P04 cancel short", s.send("B20261117100000017C0001"+"123"))
	s.put("P04 cancel not found", s.send("B20261117100000018C0001"+"20261117109999999"))
	for _, seq := range []string{"20261117100000005", "20261117100000006", "20261117100000007", "20261117100000009"} {
		s.put("P04 result "+seq, s.final("P04"+seq, seq, 60*time.Second))
	}

	// P05 부분 백업, P06 복구
	s.seqs = append(s.seqs, "20261117100000020", "20261117100000021")
	s.put("P05 accept", s.send("B20261117100000020A0005PARTIAL "+vmUUID+" 100000000000000 p "+s.root+"/p.xva"))
	s.put("P05 result", s.final("P05", "20261117100000020", 30*time.Second))
	s.put("P06 accept", s.send("B20261117100000021A0005RESTORE "+s.root+"/17.xva true "+srUUID))
	s.put("P06 result", s.final("P06", "20261117100000021", 30*time.Second))

	// P07 일괄 취소(대기 작업 없음)
	s.put("P07 multi cancel", s.send("B20261117100000030C0001"+"00000000000000000"))

	// 대기열 가득(8)은 원본에서 시간이 오래 걸리고 불안정해 단위 시험 T-AG-U07 로만 확인한다.
}

// tranSteps 는 transaction 로그에서 전문번호별 "단계:문구 | 본문" 목록을 뽑는다.
func tranSteps(s *side) map[string][]string {
	out := map[string][]string{}
	files, _ := filepath.Glob(filepath.Join(s.dir, "transaction-*.log"))
	for _, f := range files {
		data, _ := os.ReadFile(f)
		lines := strings.Split(string(data), "\n")
		for i := 0; i < len(lines); i++ {
			l := lines[i]
			if len(l) > 45 && l[0] == 0 && l[1] == '[' {
				seq := l[26:43]
				body := ""
				if i+1 < len(lines) {
					body = strings.TrimRight(strings.TrimPrefix(lines[i+1], "\x00"), "\x00")
				}
				out[seq] = append(out[seq], l[44:]+" | "+strings.ReplaceAll(body, s.root, "<ROOT>"))
			}
		}
	}
	return out
}

// backupLog 는 backup.log 의 단계 문구를 뽑는다(D 단계는 원본이 초기화하지 않은 파일 이름을 써서 뺀다).
func backupLog(s *side) []string {
	data, _ := os.ReadFile(filepath.Join(s.dir, "backup.log"))
	var out []string
	lines := strings.Split(string(data), "\n")
	for i := 0; i+1 < len(lines); i++ {
		l := lines[i]
		if len(l) < 21 || l[0] != '[' {
			continue
		}
		head := strings.ReplaceAll(l[21:], s.root, "<ROOT>")
		if strings.HasPrefix(head, "SNAPSHOT destroy") {
			continue
		}
		out = append(out, head+" / "+strings.TrimSpace(lines[i+1]))
	}
	sort.Strings(out)
	return out
}

// expected 는 의도한 차이를 판정한다. 같은 뜻의 다른 값이면 변경 대장 번호를 돌려준다.
func expected(key, c, g string) string {
	// C-AG-01: 원본 1.602 는 XVA 검증 성공 문장으로 결과를 덮어쓴다. 전환본은 검증을 따로 하고(Q-A13)
	// 운영 로그와 같은 "VM BACKUP Success" 문장을 낸다.
	if strings.Contains(c, "XVA file check Success : backup filename=") && strings.Contains(g, "VM BACKUP Success : VM uuid=") {
		cf := c[strings.Index(c, "backup filename=")+len("backup filename="):]
		gf := g[strings.Index(g, "backup filename=")+len("backup filename="):]
		// 본문 앞(전문 머리)이 같아야 한다
		if cf == gf && c[:strings.Index(c, "XVA file check")] == g[:strings.Index(g, "VM BACKUP Success")] {
			return "C-AG-01"
		}
	}
	// C-AG-02: 원본은 형식 오류 기록에 끝을 끊지 않은 버퍼를 써서 이전 내용이 뒤에 붙는다(원본 결함 D-AG-05)
	if strings.HasPrefix(c, "9:Format Error | ") && strings.HasPrefix(c, g) && len(c) > len(g) {
		return "C-AG-02"
	}
	// C-AG-03: 원본은 끊긴 접속의 fd 번호로 결과를 써서, 그 번호를 받은 다른 접속에 쓰면 "send success" 가 된다(R-AG-16).
	if strings.HasPrefix(c, "0:ASYNC RESULT send success | ") && strings.HasPrefix(g, "0:ASYNC RESULT send error | ") {
		cb, gb := c[len("0:ASYNC RESULT send success | "):], g[len("0:ASYNC RESULT send error | "):]
		if cb == gb || expected(key, cb, gb) != "" {
			return "C-AG-03"
		}
	}
	return ""
}

func compare(cs, gs *side) (pass, exp, fail int) {
	report := func(key, c, g string) {
		switch {
		case c == g:
			pass++
			fmt.Printf("PASS     %s\n", key)
		case expected(key, c, g) != "":
			exp++
			fmt.Printf("EXPECTED %s (%s)\n    c : %q\n    go: %q\n", key, expected(key, c, g), c, g)
		default:
			fail++
			fmt.Printf("FAIL     %s\n    c : %q\n    go: %q\n", key, c, g)
		}
	}
	for _, k := range cs.keys {
		report("reply "+k, cs.obs[k], gs.obs[k])
	}
	ct, gt := tranSteps(cs), tranSteps(gs)
	var seqs []string
	for seq := range ct {
		if !strings.HasPrefix(seq, "2026111790") {
			seqs = append(seqs, seq)
		}
	}
	for seq := range gt {
		if _, ok := ct[seq]; !ok && !strings.HasPrefix(seq, "2026111790") {
			seqs = append(seqs, seq)
		}
	}
	sort.Strings(seqs)
	for _, seq := range seqs {
		c, g := ct[seq], gt[seq]
		if len(c) != len(g) {
			report("tran "+seq+" count", strings.Join(c, "\n"), strings.Join(g, "\n"))
			continue
		}
		for i := range c {
			report(fmt.Sprintf("tran %s #%d", seq, i+1), c[i], g[i])
		}
	}
	report("backup.log", strings.Join(backupLog(cs), "\n"), strings.Join(backupLog(gs), "\n"))

	// 다른 요청의 결과가 이 접속으로 온 횟수. 원본은 R-AG-16 때문에 생길 수 있고, 전환본은 0 이어야 한다.
	fmt.Printf("\nSTRAY frames : c=%d go=%d\n", len(cs.stray), len(gs.stray))
	for _, l := range cs.stray {
		fmt.Printf("    c : %q\n", l)
	}
	for _, l := range gs.stray {
		fmt.Printf("    go: %q\n", l)
	}
	if len(gs.stray) > 0 {
		fail++
	}
	return
}

func main() {
	c := &side{name: "c", obs: map[string]string{}}
	g := &side{name: "go", obs: map[string]string{}}
	flag.StringVar(&c.addr, "c", "127.0.0.1:7101", "")
	flag.StringVar(&c.dir, "cdir", "/p/c", "")
	flag.StringVar(&g.addr, "go", "127.0.0.1:7102", "")
	flag.StringVar(&g.dir, "godir", "/p/go", "")
	flag.Parse()
	c.root, g.root = c.dir+"/BACKUP", g.dir+"/BACKUP"

	var wg sync.WaitGroup
	for _, s := range []*side{c, g} {
		wg.Add(1)
		go func(s *side) { defer wg.Done(); scenario(s) }(s)
	}
	wg.Wait()
	time.Sleep(time.Second) // 마지막 송신 기록이 남을 때까지
	pass, exp, fail := compare(c, g)
	fmt.Printf("\nSUMMARY: PASS %d, EXPECTED %d, FAIL %d\n", pass, exp, fail)
	if fail > 0 {
		fmt.Println("RESULT: FAIL")
		os.Exit(1)
	}
	fmt.Println("RESULT: ALL PASS (expected differences listed above)")
}
