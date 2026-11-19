package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"ncc-vbr/internal/logx"
)

// Version 은 전환본의 판이다. 원본 1.602 를 기준으로 했다.
const Version = "3.000"

// Title 은 원본 -version 출력의 제품명이다.
const Title = "Xenserver Agent for NCC-VBR"

// 수신 처리 시간. 원본은 길이가 모자라면 1초씩 5회(MAX_MSG_RETRY) 더 읽었다.
const readRetryWindow = 6 * time.Second

// Hypervisor 는 백업·복구가 부르는 XenServer 쪽 동작이다. 시험에서는 가짜로 바꾼다.
type Hypervisor interface {
	Backup(ctx context.Context, req BackupRequest, log *StepLog) (snapshotUUID string, err error)
	Restore(ctx context.Context, req RestoreRequest, log *StepLog) (vmUUID string, err error)
}

// Agent 는 실행 중인 agent 하나다.
type Agent struct {
	cfg  Settings
	log  *logx.Logger
	tran *TranLog
	reg  *Registry
	hv   Hypervisor
	step *StepLog
	dir  string

	wg       sync.WaitGroup
	stop     chan struct{}
	listener net.Listener
}

// New 는 agent 를 만든다. dir 은 작업 디렉터리(로그, 상태 파일)다.
func New(cfg Settings, log *logx.Logger, hv Hypervisor, dir string) *Agent {
	return &Agent{
		cfg:  cfg,
		log:  log,
		tran: NewTranLog(log, dir),
		reg:  NewRegistry(StatePath(dir)),
		hv:   hv,
		step: &StepLog{log: log},
		dir:  dir,
		stop: make(chan struct{}),
	}
}

// Listen 은 수신 포트를 연다. 원본은 포트를 쓰는 프로세스를 죽였지만(fuser -k) 전환본은 오류로 끝낸다.
func (a *Agent) Listen() error {
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(a.cfg.Port))
	if err != nil {
		return fmt.Errorf("port %d listen failed (VBR Agent is already running or the port is in use): %w", a.cfg.Port, err)
	}
	a.listener = ln
	return nil
}

// Addr 는 수신 주소다(시험용).
func (a *Agent) Addr() net.Addr { return a.listener.Addr() }

// Run 은 재기동 복구, 작업자, 수신을 시작하고 ctx 가 끝날 때까지 돈다.
func (a *Agent) Run(ctx context.Context) {
	restarted := 0
	requeued := a.reg.Restore(func(m Message) {
		// 원본 env_initialize 571~576행
		a.log.Printf("[%.17s] Task cancel.   Please check result.\n", m.Seq())
		a.tran.Write('6', "Task cancel(mode 1)", m.Raw)
		restarted++
	})
	if restarted+requeued > 0 {
		a.log.Printf("Restart recovery : canceled [%d], requeued [%d]\n", restarted, requeued)
	}

	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for i := 0; i < a.cfg.AsyncWorkers; i++ {
		a.wg.Add(1)
		go a.asyncWorker(workCtx, i)
		a.log.Printf("[%d]th async thread starting.\n", i)
	}

	go func() {
		<-ctx.Done()
		close(a.stop)
		a.listener.Close()
	}()
	a.log.Printf("VBR agent for Xenserver service start.\n")
	a.log.Printf("NCC-VBR version	: [%s]\n", Version)

	var conns sync.WaitGroup
	for {
		c, err := a.listener.Accept()
		if err != nil {
			select {
			case <-a.stop:
				a.log.Printf("VBR agent for Xenserver service stop.\n")
				cancel()
				a.wg.Wait()
				conns.Wait()
				return
			default:
			}
			a.log.Printf("Network accept internal error.[%v]\n", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if !a.cfg.allowed(c.RemoteAddr()) {
			a.log.Printf("Connection refused (not in %s) [%s]\n", keyAllowClient, c.RemoteAddr())
			c.Close()
			continue
		}
		conns.Add(1)
		go func() {
			defer conns.Done()
			a.serve(c)
		}()
	}
}

// netConn 은 접속 하나다. 같은 접속으로 여러 응답이 나갈 수 있어 쓰기를 잠근다.
type netConn struct {
	c     net.Conn
	mu    sync.Mutex
	alive atomic.Bool
}

func (n *netConn) Send(frame []byte) error {
	if !n.alive.Load() {
		return errors.New("connection closed")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	_ = n.c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := n.c.Write(frame)
	return err
}

func (n *netConn) Alive() bool { return n.alive.Load() }

// serve 는 접속 하나에서 전문을 차례로 읽는다(원본 RealTimeMain 의 접속별 처리).
func (a *Agent) serve(c net.Conn) {
	conn := &netConn{c: c}
	conn.alive.Store(true)
	defer func() {
		conn.alive.Store(false)
		c.Close()
	}()
	go func() {
		<-a.stop
		c.Close()
	}()
	for {
		_ = c.SetReadDeadline(time.Time{})
		head := make([]byte, lengthLen)
		if _, err := io.ReadFull(c, head); err != nil {
			return // 원본 "Close connection request received."
		}
		n, _ := strconv.Atoi(string(head)) // 원본 atoi: 숫자가 아니면 0
		if n <= 0 {
			a.log.Printf("Null stream	data received. skip	this operation\n")
			continue
		}
		if n > HeaderLen+maxBodyLen {
			a.log.Printf("Message length is too large [%d]. Close connection.\n", n)
			return
		}
		body := make([]byte, n)
		_ = c.SetReadDeadline(time.Now().Add(readRetryWindow))
		if got, err := io.ReadFull(c, body); err != nil {
			a.log.Printf("Close connection (message length is not valid). [%d][%d]\n", n, got)
			return
		}
		a.handle(Message{Raw: body}, conn)
	}
}

// handle 은 원본 MessageProcessing + backup_receiver + mgt_processing 이다.
func (a *Agent) handle(msg Message, conn Conn) {
	if msg.at(0) != 'B' {
		// 원본 858~872행
		a.log.Printf("request mesage format error.\n")
		reply := msg.Echo(CodeError)
		a.tran.Write('9', "Format Error", reply)
		_ = conn.Send(Frame(reply))
		return
	}
	i := a.reg.Register(msg, conn)
	if i < 0 {
		// 원본 834~855행
		a.log.Printf("Task queue is full [%d].\n", MaxSlots)
		reply := msg.Echo(CodeQueueFull)
		a.tran.Write('8', "Queue full", reply)
		_ = conn.Send(Frame(reply))
		return
	}
	a.tran.Write('1', "Message Receive", msg.Raw)

	switch msg.Task() {
	case 'A':
		a.tran.Write('2', "Queue saving", msg.Raw)
		// 원본은 큐에 넣은 뒤 접수 응답을 보냈다(1112~1144행). 응답 머리는 이 전문의 것이다
		// (원본은 마지막에 받은 전문 sx_Message 를 복사해 다른 전문 번호가 나갈 수 있었다).
		a.reg.Enqueue(i)
		if err := conn.Send(Frame(msg.Reply(CodeOK, "OK"))); err != nil {
			a.log.Printf("Send error : Backup manager. internal error[%v]\n", err)
		}
	case 'Q', 'C':
		a.reg.SetStatus(i, StatusQueued)
		a.tran.Write('2', "Queue saving", msg.Raw)
		a.management(i, msg, conn)
	case 'S', 'T', 'I':
		// 전환본에서 지원하지 않는 요청(docs/agent/00-rescope-review.md 2장, Q-A11)
		reply := msg.Reply(CodeError, "[ERROR]Not supported.")
		a.tran.Write('9', "Not supported", reply)
		a.sendMgtResult(i, reply, conn)
	default:
		// 원본 1094~1106행. 원본은 상태를 1로 둔 채 1초마다 같은 응답을 되풀이했다.
		a.log.Printf("TASK type format error\n")
		reply := msg.Echo(CodeError)
		a.tran.Write('9', "TASK Format Error", reply)
		_ = conn.Send(Frame(reply))
		a.reg.SetStatus(i, StatusError)
	}
}

// management 는 원본 mgt_processing 의 Q, C 처리다.
func (a *Agent) management(i int, msg Message, conn Conn) {
	a.reg.SetStatus(i, StatusRunning)
	a.tran.Write('3', "Management Queue receive", msg.Raw)
	var code byte
	var text string
	var ok bool
	if msg.Task() == 'Q' {
		code, text, ok = a.query(msg)
	} else {
		code, text, ok = a.cancel(msg)
	}
	if !ok {
		code = CodeError
	}
	a.sendMgtResult(i, msg.Reply(code, text), conn)
}

// sendMgtResult 는 조회·취소 결과를 보내고 기록한다(원본 1476~1544행).
func (a *Agent) sendMgtResult(i int, reply []byte, conn Conn) {
	if err := conn.Send(Frame(reply)); err != nil {
		a.log.Printf("Send to REQUEST Manager internal error[%v]\n", err)
		a.tran.Write('0', "QUERY RESULT send error", reply)
	} else {
		a.tran.Write('0', "QUERY RESULT send success", reply)
	}
	a.reg.SetStatus(i, StatusDone) // 원본은 송신 결과와 관계없이 마지막에 5로 바꿨다(1544행)
}

// query 는 원본 query_task 다.
func (a *Agent) query(msg Message) (byte, string, bool) {
	seq := msg.Body()
	if len(seq) != seqLen {
		return CodeError, fmt.Sprintf("TASK SEQ is not valid.[%s]", seq), false
	}
	code, text, ok := a.tran.Query(seq)
	if !ok {
		return CodeError, seq, false
	}
	// 원본 3464~3481행: 로그로는 대기·처리 중인데 등록부에 없으면 7
	if code == '1' || code == '2' || code == '3' {
		var key [seqLen]byte
		copy(key[:], seq)
		if _, _, found := a.reg.Find(key); !found {
			a.log.Printf("[%.17s] Task already canceled.   Please check result.\n", seq)
			code = '7'
		}
	}
	return code, text, true
}

// cancel 은 원본 cancel_task 다. 일괄 취소는 전문번호 0 17자리다.
func (a *Agent) cancel(msg Message) (byte, string, bool) {
	target := msg.Body()
	if len(target) != seqLen {
		return CodeError, "Cancel request sequence is not valid.", false
	}
	if target != "00000000000000000" {
		var key [seqLen]byte
		copy(key[:], target)
		return a.cancelOne(key, msg)
	}
	total, ok, failed := 0, 0, 0
	for i := 0; i < MaxSlots; i++ {
		total++
		if _, _, success := a.cancelOne(a.reg.SeqAt(i), msg); success {
			ok++
		} else {
			failed++
		}
	}
	a.log.Printf("Multi cancel summary : total [%d], cancel [%d], failed [%d]\n", total, ok, failed)
	// 원본 cancel_task 2940~2967행은 일괄 취소에서 결과 코드를 바꾸지 않아 요청의 작업구분 'C' 가 그대로 나간다
	// (2015 표본도 'C'. 원본 비교 시험에서 확인).
	return 'C', fmt.Sprintf("Multi cancel summary : total [%d], cancel [%d], failed [%d]\n", total, ok, failed), true
}

// cancelOne 은 원본 cancel_task_detail 이다. 처리 중인 작업은 원본처럼 취소하지 않는다.
func (a *Agent) cancelOne(key [seqLen]byte, req Message) (byte, string, bool) {
	i, status, found := a.reg.Find(key)
	if !found {
		return CodeError, "Request message not found.(Invalid or Already finished).", false
	}
	if status > StatusRunning || status == StatusFree {
		return CodeError, "Request message Already finished.", false
	}
	if status == StatusRunning {
		return CodeError, "Request message is processing.", false
	}
	changed, removed, target := a.reg.CancelQueued(i)
	if changed {
		return CodeError, "[NOTICE]Request message is processing or already finished.", false
	}
	if !removed {
		return CodeError, "Queue Internal ERROR. Queue is empty.", false
	}
	a.tran.Write('C', "TASK Canceled", target.Raw)
	// 원본 3092행: 취소 요청 자신의 전문번호를 적는다
	return CodeOK, fmt.Sprintf("Request message is cancel.[%s]", req.Seq()), true
}
