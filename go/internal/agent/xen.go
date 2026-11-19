package agent

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"ncc-vbr/internal/logx"
	"ncc-vbr/internal/xapi"
)

// XAPI 호출 시간. 스냅샷은 디스크 수에 따라 오래 걸릴 수 있어 넉넉히 둔다
// (ncc-xen 주석: 8.4/9.0 에서 동기 호출이 90초를 넘긴 사례가 있다).
const (
	xapiConnectTimeout = 10 * time.Second
	xapiRequestTimeout = 30 * time.Minute
	cleanupTimeout     = 2 * time.Minute
)

// Xen 은 XAPI 로 백업·복구를 하는 Hypervisor 다.
type Xen struct {
	cfg      Settings
	password string
	log      *logx.Logger
	http     *http.Client // /export, /import 용(전체 시간 제한 없음, ctx 로 끝낸다)

	mu     sync.Mutex
	client *xapi.Client
}

// NewXen 은 Xen 을 만든다. password 는 풀어 놓은 암호다.
func NewXen(cfg Settings, password string, log *logx.Logger) *Xen {
	return &Xen{
		cfg:      cfg,
		password: password,
		log:      log,
		http: &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: !cfg.VerifySSL}, //nolint:gosec // 설정으로 정한다(R-AG-24)
		}},
	}
}

// session 은 로그인된 클라이언트를 돌려준다. 세션이 죽었으면 다시 로그인한다.
func (x *Xen) session(ctx context.Context) (*xapi.Client, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.client != nil {
		// 가벼운 호출로 세션을 확인한다.
		_, err := x.client.Call(ctx, "session.get_this_host", x.client.SessionRef(), x.client.SessionRef())
		if err == nil {
			return x.client, nil
		}
		if !xapi.IsSessionError(err) {
			return nil, err
		}
		x.log.Printf("XAPI session is not valid. Login again. [%v]\n", err)
		x.client.Close()
		x.client = nil
	}
	c := xapi.NewClient(x.cfg.XAPIAddress, x.cfg.VerifySSL, xapiConnectTimeout, xapiRequestTimeout, x.log)
	if err := c.Login(ctx, x.cfg.XAPIUser, x.password); err != nil {
		c.Close()
		return nil, err
	}
	x.log.Printf("XAPI login success [%s]\n", c.BaseURL())
	x.client = c
	return c, nil
}

// xapiText 는 오류에서 사용자에게 보일 문장을 꺼낸다. XAPI 오류면 그 문장, 아니면 오류 전체다.
func xapiText(err error) string {
	var xe *xapi.XAPIError
	if errors.As(err, &xe) {
		return xe.Error()
	}
	return err.Error()
}

// Backup 은 원본 backup_vm 의 3851~3964행이다(인자 검사는 agent 쪽에서 마쳤다).
func (x *Xen) Backup(ctx context.Context, req BackupRequest, log *StepLog) (string, error) {
	c, err := x.session(ctx)
	if err != nil {
		log.Backup('S', req.VMUUID, req.FileName, "VM Snapshot failed : [XAPI session][%s]", xapiText(err))
		return "", fmt.Errorf("[ERROR]%s", xapiText(err))
	}
	vm := xapi.NewVMClient(c)

	// ① 스냅샷(원본 3864행). 이름은 받은 그대로.
	snapRef, err := vm.Snapshot(ctx, req.VMUUID, req.SnapshotName)
	var snapUUID string
	if err == nil {
		snapUUID, err = vm.GetUUIDByRef(ctx, snapRef)
	}
	if err != nil {
		log.Backup('S', req.VMUUID, req.FileName, "VM Snapshot failed : [%s]", xapiText(err))
		x.log.Printf("VM Snapshot failed : [%s]\n", xapiText(err))
		if snapRef != "" {
			x.uninstall(vm, snapRef, snapUUID, req.FileName, log)
		}
		return "", fmt.Errorf("[ERROR]%s", xapiText(err))
	}
	log.Backup('S', req.VMUUID, req.FileName, "VM Snapshot success, uuid=%s", snapUUID)

	// ② 스냅샷을 VM 으로(원본 3892행 template-param-set is-a-template=false ha-always-run=false)
	if err := vm.SetIsATemplate(ctx, snapRef, false); err != nil {
		log.Backup('P', snapUUID, req.FileName, "VM Snapshot property change failed : [%s]", xapiText(err))
		x.uninstall(vm, snapRef, snapUUID, req.FileName, log)
		return "", fmt.Errorf("[ERROR]%s", xapiText(err))
	}
	if err := vm.SetHAAlwaysRun(ctx, snapRef, false); err != nil {
		// ha_always_run 은 XenServer 7 이후 폐기 예정 필드라 실패해도 백업을 계속한다(변경 대장 C-AG-07).
		x.log.Printf("VM.set_ha_always_run failed (ignored) : [%s]\n", xapiText(err))
	}
	log.Backup('P', snapUUID, req.FileName, "VM Snapshot property change, uuid=%s", snapUUID)

	// ③ 부분 백업이면 고르지 않은 디스크를 스냅샷에서 지운다(원본 3910~3922, 3983~4115행)
	if req.Partial {
		if err := x.partialRemove(ctx, vm, snapRef, req.DiskSeq); err != nil {
			x.log.Printf("Partial VM BACKUP (Remove VDI/VBD) failed.[%s][%s] %v\n", snapUUID, req.FileName, err)
			log.Backup('R', snapUUID, req.FileName, "Partial VM BACKUP (Remove VDI/VBD) failed.")
			x.uninstall(vm, snapRef, snapUUID, req.FileName, log)
			return "", errors.New("[ERROR]Partial VM BACKUP (Remove VDI/VBD) failed.")
		}
	}

	// ④ 같은 이름의 파일 삭제(원본 rm -f) 와 ⑤ 내보내기(원본 vm-export … compress=true)
	if err := os.Remove(req.FileName); err != nil && !os.IsNotExist(err) {
		x.log.Printf("Backup file remove failed (ignored) [%s] %v\n", req.FileName, err)
	}
	if err := x.export(ctx, c, vm, snapUUID, req.FileName); err != nil {
		log.Backup('E', snapUUID, req.FileName, "Snapshot VM export failed : [%s]", xapiText(err))
		x.log.Printf("Snapshot VM export failed : [%s]\n", xapiText(err))
		x.uninstall(vm, snapRef, snapUUID, req.FileName, log)
		return "", fmt.Errorf("[ERROR]%s", xapiText(err))
	}
	log.Backup('E', snapUUID, req.FileName, "VM export succeeded.")

	// ⑥ 스냅샷 삭제(원본 remove_snapshot_vm)
	if !x.uninstall(vm, snapRef, snapUUID, req.FileName, log) {
		x.log.Printf("Remove Snapshot VM remove internal failed. Snapshot uuid=%s\n", snapUUID)
		return "", fmt.Errorf("[ERROR]Remove Snapshot VM remove internal failed. Snapshot uuid=%s\n", snapUUID)
	}
	return snapUUID, nil
}

// partialRemove 는 원본 partial_backup_remove 다. diskSeq 의 자리 값이 '0' 인 userdevice 디스크를 지운다.
func (x *Xen) partialRemove(ctx context.Context, vm *xapi.VMClient, snapRef, diskSeq string) error {
	vbds, err := vm.DiskVBDs(ctx, snapRef)
	if err != nil {
		return err
	}
	for _, d := range vbds {
		if d.UserDevice < 0 || d.UserDevice >= MaxVBD {
			// 원본은 배열 밖에 썼다(결함). 전환본은 자리가 없는 디스크를 백업에 남긴다.
			x.log.Printf("Partial backup : userdevice [%d] is out of range. kept.\n", d.UserDevice)
			continue
		}
		if diskSeq[d.UserDevice] != '0' {
			continue
		}
		if err := vm.SetVBDOwnerFalse(ctx, d.Ref); err != nil {
			x.log.Printf("VBD param set ERROR. [%s] %v\n", d.UUID, err)
			return err
		}
		if d.VDIRef == "" || d.VDIRef == xapi.NullRef {
			continue
		}
		if err := vm.DestroyVDI(ctx, d.VDIRef); err != nil {
			x.log.Printf("VDI Destroy ERROR. [%s] %v\n", d.VDIRef, err)
			return err
		}
	}
	return nil
}

// uninstall 은 원본 remove_snapshot_vm 이다. 성공하면 참이다.
// 작업이 취소돼도 정리는 해야 하므로 새 ctx 를 쓴다.
func (x *Xen) uninstall(vm *xapi.VMClient, snapRef, snapUUID, fileName string, log *StepLog) bool {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	if err := vm.UninstallSnapshot(ctx, snapRef); err != nil {
		log.Backup('D', snapUUID, fileName, "Snapshot VM remove failed : [%s]", xapiText(err))
		return false
	}
	log.Backup('D', snapUUID, fileName, "Snapshot VM remove, uuid=%s", snapUUID)
	return true
}

// Restore 는 원본 import_vm 4152~4182행이다.
func (x *Xen) Restore(ctx context.Context, req RestoreRequest, log *StepLog) (string, error) {
	fail := func(err error) (string, error) {
		log.Import(req.SRUUID, req.FileName, "VM import failed : [%s]", xapiText(err))
		x.log.Printf("VM import failed : [%s]\n", xapiText(err))
		return "", fmt.Errorf("[ERROR]%s", xapiText(err))
	}
	c, err := x.session(ctx)
	if err != nil {
		return fail(err)
	}
	vm := xapi.NewVMClient(c)
	uuid, err := x.importFile(ctx, c, vm, req)
	if err != nil {
		return fail(err)
	}
	log.Import(req.SRUUID, req.FileName, "VM import success, vm-uuid=%s", uuid)
	return uuid, nil
}
