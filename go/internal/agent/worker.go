package agent

import (
	"context"
	"fmt"
	"strings"
)

// MaxVBD 는 원본 MAX_VBD_CNT(부분 백업 디스크 자리 수) 다.
const MaxVBD = 15

// BackupRequest 는 BACKUP 과 PARTIAL 의 인자다.
type BackupRequest struct {
	VMUUID       string
	SnapshotName string
	FileName     string
	Partial      bool
	DiskSeq      string // PARTIAL 만. 자리마다 userdevice 하나, '0' 이면 백업에서 뺀다
}

// RestoreRequest 는 RESTORE 의 인자다.
type RestoreRequest struct {
	FileName string
	Preserve bool
	SRUUID   string
}

// asyncWorker 는 원본 async_processing + backup_async_sender 다.
func (a *Agent) asyncWorker(ctx context.Context, seq int) {
	defer a.wg.Done()
	for {
		i, msg, ok := a.reg.Dequeue(ctx.Done())
		if !ok {
			return
		}
		a.tran.Write('3', "ASYNC Queue receive", msg.Raw)
		code, text := a.runAsync(ctx, msg)
		if ctx.Err() != nil {
			// 종료 중에 끊긴 작업은 상태 파일에 처리 중(3)으로 남아 다음 기동 때 6 으로 기록된다(원본과 같다).
			a.log.Printf("ASYNC [%d]th thread : task [%s] interrupted by shutdown.\n", seq, msg.Seq())
			return
		}
		result := msg.Reply(code, text)
		a.tran.Write('5', "ASYNC Queue result send finished", result)
		a.sendAsyncResult(i, result)
	}
}

// sendAsyncResult 는 원본 backup_async_sender 다. 관리 서버는 접수 응답 뒤 접속을 끊으므로
// 보통은 송신 오류로 기록되고, 결과는 Q 조회로 가져간다(운영 로그와 같은 흐름).
func (a *Agent) sendAsyncResult(i int, result []byte) {
	conn := a.reg.Conn(i)
	if conn != nil && conn.Alive() {
		if err := conn.Send(Frame(result)); err == nil {
			a.tran.Write('0', "ASYNC RESULT send success", result)
			a.reg.Free(i)
			return
		}
	}
	a.log.Printf("Send to REQUEST Manager internal error[%s]\n", Message{Raw: result}.Seq())
	a.tran.Write('0', "ASYNC RESULT send error", result)
	a.reg.SetStatus(i, StatusError)
}

// runAsync 는 본문 첫 낱말로 나눈다(원본 3710~3736행). 결과 코드와 본문을 돌려준다.
func (a *Agent) runAsync(ctx context.Context, msg Message) (byte, string) {
	body := msg.Body()
	switch {
	case strings.HasPrefix(body, "BACKUP"), strings.HasPrefix(body, "PARTIAL"):
		return a.backup(ctx, body)
	case strings.HasPrefix(body, "RESTORE"):
		return a.restore(ctx, body)
	default:
		// 원본은 그 밖의 비동기 본문을 dom0 셸 명령으로 실행했다. 전환본은 지원하지 않는다(DEC-AG-04).
		return CodeError, "[ERROR]Not supported."
	}
}

// fields 는 원본 sscanf("%s %s ...") 다. 모자란 자리는 빈 문자열이다.
func fields(s string, n int) []string {
	f := strings.Fields(s)
	out := make([]string, n)
	copy(out, f)
	return out
}

// parseBackup 은 원본 backup_vm 3806~3849행의 인자 검사다. 오류 문장은 원본 그대로다.
func parseBackup(body string) (BackupRequest, error) {
	var r BackupRequest
	switch {
	case strings.HasPrefix(body, "BACKUP"):
		f := fields(body, 4)
		r = BackupRequest{VMUUID: f[1], SnapshotName: f[2], FileName: f[3]}
	case strings.HasPrefix(body, "PARTIAL"):
		f := fields(body, 5)
		r = BackupRequest{VMUUID: f[1], DiskSeq: f[2], SnapshotName: f[3], FileName: f[4], Partial: true}
	default:
		return r, fmt.Errorf("[ERROR]VBR COMMAND is not valid.")
	}
	if len(r.VMUUID) != 36 {
		return r, fmt.Errorf("[ERROR]VM UUID is not valid.")
	}
	if r.SnapshotName == "" {
		return r, fmt.Errorf("[ERROR]Snapshot Name is not valid.")
	}
	if r.FileName == "" {
		return r, fmt.Errorf("[ERROR]Export File Name is not valid.")
	}
	if r.Partial && len(r.DiskSeq) != MaxVBD {
		return r, fmt.Errorf("[ERROR]Partial Backup Disk sequence information is not valid.")
	}
	return r, nil
}

// parseRestore 는 원본 import_vm 4132~4150행이다.
func parseRestore(body string) (RestoreRequest, error) {
	f := fields(body, 4)
	r := RestoreRequest{FileName: f[1], SRUUID: f[3]}
	if r.FileName == "" {
		return r, fmt.Errorf("[ERROR]File Name is not valid.")
	}
	if len(r.SRUUID) != 36 {
		return r, fmt.Errorf("[ERROR]SR UUID is not valid.")
	}
	switch {
	case strings.HasPrefix(f[2], "true"):
		r.Preserve = true
	case strings.HasPrefix(f[2], "false"):
	default:
		return r, fmt.Errorf("[ERROR]Preserve option(true/false) is not valid.")
	}
	return r, nil
}

// backup 은 원본 backup_vm 이다. 단계별 기록은 Hypervisor 가 남긴다.
func (a *Agent) backup(ctx context.Context, body string) (byte, string) {
	req, err := parseBackup(body)
	if err != nil {
		a.log.Printf("%v\n", err)
		return CodeError, err.Error()
	}
	if !a.cfg.insideRoots(req.FileName) {
		// Q-A15: 백업 루트 밖의 파일 이름은 받지 않는다(원본은 받은 이름으로 rm -f 를 실행했다).
		a.log.Printf("Export File Name is outside of %s [%s]\n", keyBackupRoot, req.FileName)
		return CodeError, "[ERROR]Export File Name is not allowed."
	}
	snap, err := a.hv.Backup(ctx, req, a.step)
	if err != nil {
		return CodeError, err.Error()
	}
	// 원본 3963행. XVA 검증(3966~3975행)은 사용자 결정으로 따로 진행한다(Q-A13).
	return CodeOK, fmt.Sprintf("VM BACKUP Success : VM uuid=%s, Snapshot VM uuid=%s, backup filename=%s\n",
		req.VMUUID, snap, req.FileName)
}

// restore 는 원본 import_vm 이다.
func (a *Agent) restore(ctx context.Context, body string) (byte, string) {
	req, err := parseRestore(body)
	if err != nil {
		return CodeError, err.Error()
	}
	if !a.cfg.insideRoots(req.FileName) {
		a.log.Printf("Restore File Name is outside of %s [%s]\n", keyBackupRoot, req.FileName)
		return CodeError, "[ERROR]File Name is not allowed."
	}
	vm, err := a.hv.Restore(ctx, req, a.step)
	if err != nil {
		return CodeError, err.Error()
	}
	return CodeOK, vm
}
