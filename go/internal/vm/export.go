package vm

// export.go 의 순서는 ncc-xen backend/internal/job/vm_backup_export.go runExport(@043c6d5, CP-VM-05)와
// 1단계 go/internal/agent/xen.go Backup 을 합친 것이다.
//   - 전원 상태에 따른 스냅샷 판단(auto)은 ncc-xen, always·디스크 선택·스냅샷 정리(스냅샷 VDI 삭제)는 1단계.
//   - ncc-xen 의 임시 스냅샷 정리는 VM.destroy 만 해서 스냅샷 VDI 가 남을 수 있다. 1단계 UninstallSnapshot 을 쓴다.

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"ncc-vbr/internal/i18n"
	"ncc-vbr/internal/jobstore"
	logger "ncc-vbr/internal/stdlogger"
	"ncc-vbr/internal/vmconfig"
	"ncc-vbr/internal/xapi"
	"ncc-vbr/internal/xva"
)

// PasswordPurpose 는 요청 속 XAPI 암호의 복호화 용도 문자열이다(DEC-VM-27, 1단계 vbr_secret 과 같다).
const PasswordPurpose = "ncc-vbr/xapi-password"

const (
	xapiConnectTimeout = 10 * time.Second
	xapiRequestTimeout = 30 * time.Minute
	cleanupTimeout     = 2 * time.Minute
)

// printfLogger 는 xapi.Logger 를 stdlogger 로 잇는다.
type printfLogger struct{ e *logger.Entry }

func (p printfLogger) Printf(format string, args ...interface{}) {
	p.e.Debug(strings.TrimRight(fmt.Sprintf(format, args...), "\n"))
}

// session 은 작업 하나의 XAPI 세션이다. 작업이 끝나면 로그아웃한다(01 요건 5장 5번).
type session struct {
	client *xapi.Client
	vm     *xapi.VMClient
	http   *http.Client
}

func (s *session) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = s.client.Logout(ctx)
	s.client.Close()
	if tr, ok := s.http.Transport.(*http.Transport); ok {
		tr.CloseIdleConnections()
	}
}

// login 은 암호를 풀고 마스터에 로그인한다. 풀린 암호는 이 함수 밖으로 나가지 않는다.
func login(ctx context.Context, p *Payload, cfg *vmconfig.Loaded, log *logger.Entry) (*session, error) {
	pw, err := cfg.Reveal(p.Credentials.Password, PasswordPurpose)
	if err != nil {
		return nil, &JobError{Code: CodeDecryptFailed, Detail: err.Error()}
	}
	user := p.Credentials.Username
	if user == "" {
		user = "root"
	}
	addr := strings.TrimSpace(p.MasterIP)
	if !strings.HasPrefix(addr, "https://") && !strings.HasPrefix(addr, "http://") {
		addr = "https://" + addr
	}
	c := xapi.NewClient(addr, cfg.XAPI.VerifySSL, xapiConnectTimeout, xapiRequestTimeout, printfLogger{log})
	if err := c.Login(ctx, user, pw); err != nil {
		c.Close()
		return nil, loginError(err)
	}
	log.Info(i18n.T("vm.log.login"), "master", c.BaseURL())
	return &session{
		client: c,
		vm:     xapi.NewVMClient(c),
		http: &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: !cfg.XAPI.VerifySSL}, //nolint:gosec // 설정으로 정한다
		}},
	}, nil
}

func pathSettings(cfg *vmconfig.Loaded) xva.Settings {
	return xva.Settings{BasePath: cfg.Backup.BasePath, SandboxMode: cfg.Backup.SandboxMode,
		AllowedAbsolutePaths: cfg.Backup.AllowedAbsolutePaths}
}

// xapiBool 은 XAPI 레코드의 bool 필드를 읽는다(bool 또는 "0"/"1"/"true"/"false", ncc-xen xapiBoolField).
func xapiBool(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "1" || strings.EqualFold(t, "true")
	}
	return false
}

// tempSnapshot 은 이 작업이 만든 임시 스냅샷이다.
type tempSnapshot struct {
	ref, uuid string
}

func (s *Service) runExport(ctx context.Context, job *Job, cfg *vmconfig.Loaded, p *progress, log *logger.Entry) outcome {
	o := job.Export
	vmUUID := job.Req.Payload.TargetUUID

	// ① 경로(로그인 전에 확인한다. ncc-xen 과 같은 순서)
	outDir := cfg.Backup.BasePath
	if o.ExportPath != "" {
		d, err := xva.Resolve(pathSettings(cfg), o.ExportPath)
		if err != nil {
			return outcome{err: err}
		}
		outDir = d
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return outcome{err: &JobError{Code: CodeFileIO, Detail: err.Error()}}
	}
	outPath := joinPath(outDir, xva.OutputName(o.FileName, o.Compression))
	if _, err := os.Stat(outPath); err == nil && !o.Overwrite {
		return outcome{err: &JobError{Code: CodeFileExists, Detail: "file already exists: " + outPath}}
	}

	// ② 로그인과 VM 상태
	sess, err := login(ctx, job.Req.Payload, cfg, log)
	if err != nil {
		return outcome{err: err}
	}
	defer sess.close()
	vmRef, err := sess.vm.GetByUUID(ctx, vmUUID)
	if err != nil {
		return outcome{err: classify(err)}
	}
	rec, err := sess.vm.GetRecord(ctx, vmRef)
	if err != nil {
		return outcome{err: classify(err)}
	}
	powerState := strings.ToLower(fmt.Sprint(rec["power_state"]))
	isSnapshot := xapiBool(rec["is_a_snapshot"])
	vmName, _ := rec["name_label"].(string)

	needSnap, jerr := decideSnapshot(o, powerState, isSnapshot)
	if jerr != nil {
		return outcome{err: jerr}
	}
	exportUUID := vmUUID
	var snap *tempSnapshot
	result := map[string]interface{}{}
	if needSnap {
		p.set("snapshot", 1, i18n.T("vm.export.snapshot"))
		name := o.SnapshotName
		if name == "" {
			name = fmt.Sprintf("NCC-BACKUP-%s-%s", vmName, s.now().Format("20060102150405"))
		}
		snap, err = s.makeSnapshot(ctx, sess.vm, vmUUID, name, log)
		if err != nil {
			return outcome{err: err}
		}
		exportUUID = snap.uuid
		// 강제 종료되면 정리하지 못한다. 재기동 때 남은 스냅샷을 알 수 있게 바로 기록한다(REQ-VM-17)
		p.note(jobstore.SnapshotLeftKey, snap.uuid)
		defer func() { s.cleanupSnapshot(sess.vm, snap, o.KeepSnapshot, result, log) }()
		if len(o.Disks) > 0 {
			if err := keepOnlyDisks(ctx, sess.vm, snap.ref, o.Disks, log); err != nil {
				return outcome{err: err, result: result}
			}
		}
	}

	// ③ 내보내기
	verifyAfter := cfg.Backup.VerifyAfterExport
	if o.VerifyAfterExport != nil {
		verifyAfter = *o.VerifyAfterExport
	}
	scale := 99
	if verifyAfter {
		scale = 90
	}
	log.Info(i18n.T("vm.log.export_start"), "exportUUID", exportUUID, "snapshotUsed", snap != nil,
		"filePath", outPath, "compression", o.Compression)
	res, err := xva.Export(ctx, xva.ExportParams{
		Client: sess.client, VM: sess.vm, HTTP: sess.http, UUID: exportUUID, OutputPath: outPath,
		Compression: o.Compression, Overwrite: o.Overwrite, TaskLabel: "NCC-VBR VM Export",
		Interval: cfg.ProgressInterval(),
		Progress: func(pct int, _ int64) {
			v := pct * scale / 99
			p.set("exporting", v, pctMsg("vm.export.running", v))
		},
	})
	if err != nil {
		return outcome{err: err, result: result}
	}
	result["file_path"] = outPath
	result["file_size_bytes"] = res.Size
	result["checksum_sha256"] = res.SHA256
	result["format"] = "xva"
	result["compression"] = o.Compression
	result["exported_uuid"] = exportUUID
	result["power_state"] = powerState
	result["snapshot_used"] = snap != nil
	result["duration_seconds"] = seconds(res.Duration)
	if len(o.Disks) > 0 {
		result["disks"] = o.Disks
	}
	if powerState == "suspended" && snap == nil {
		result["memory_included"] = false // ncc-xen 과 같다: 디스크 기준 백업
	}

	// ④ 백업 뒤 검증(선택)
	if verifyAfter {
		vr, verr := xva.Verify(ctx, outPath, cfg.ProgressInterval(), func(pct int, _ int64) {
			v := 90 + pct*9/99
			p.set("verifying", v, pctMsg("vm.verify.running", v))
		})
		if verr != nil {
			return outcome{err: verr, result: result}
		}
		result["verify"] = verifyMap(outPath, vr, s.now())
	}
	log.Info(i18n.T("vm.log.export_done"), "filePath", outPath, "fileSizeBytes", res.Size, "sha256", res.SHA256)
	p.set("cleanup", 99, i18n.T("vm.export.cleanup"))
	result["completedAt"] = completedAt(s.now())
	return outcome{result: result, message: i18n.T("vm.export.done", outPath)}
}

// decideSnapshot 은 임시 스냅샷이 필요한지 정한다(03 설계 3장 표).
func decideSnapshot(o *ExportOptions, powerState string, isSnapshot bool) (bool, *JobError) {
	if isSnapshot {
		if len(o.Disks) > 0 {
			// 스냅샷에서 디스크를 지우면 사용자의 스냅샷이 망가진다.
			return false, &JobError{Code: CodeInvalidField, Detail: "disks cannot be used when targetUUID is a snapshot"}
		}
		return false, nil
	}
	if len(o.Disks) > 0 || o.SnapshotMode == SnapshotAlways {
		return true, nil
	}
	switch powerState {
	case "halted", "suspended":
		return false, nil
	case "running":
		return true, nil
	}
	return false, &JobError{Code: CodeVMState, Detail: "unsupported power state for export: " + powerState}
}

// makeSnapshot 은 임시 스냅샷을 만들고 VM 으로 바꾼다(1단계 Backup ①②).
func (s *Service) makeSnapshot(ctx context.Context, vm *xapi.VMClient, vmUUID, name string, log *logger.Entry) (*tempSnapshot, error) {
	ref, err := vm.Snapshot(ctx, vmUUID, name)
	if err != nil {
		return nil, classify(err)
	}
	uuid, err := vm.GetUUIDByRef(ctx, ref)
	if err != nil {
		s.uninstall(vm, &tempSnapshot{ref: ref}, log)
		return nil, classify(err)
	}
	snap := &tempSnapshot{ref: ref, uuid: uuid}
	if err := vm.SetIsATemplate(ctx, ref, false); err != nil {
		s.uninstall(vm, snap, log)
		return nil, classify(err)
	}
	if err := vm.SetHAAlwaysRun(ctx, ref, false); err != nil {
		log.Debug("VM.set_ha_always_run failed (ignored)", "error", err) // 폐기 예정 필드(1단계 C-AG-07)
	}
	log.Info(i18n.T("vm.log.snapshot"), "snapshotUUID", uuid, "snapshotName", name)
	return snap, nil
}

// keepOnlyDisks 는 스냅샷에서 고르지 않은 디스크를 지운다(1단계 partialRemove). 고른 번호가 없으면 DISK_NOT_FOUND.
func keepOnlyDisks(ctx context.Context, vm *xapi.VMClient, snapRef string, keep []int, log *logger.Entry) error {
	vbds, err := vm.DiskVBDs(ctx, snapRef)
	if err != nil {
		return classify(err)
	}
	have := map[int]bool{}
	for _, d := range vbds {
		have[d.UserDevice] = true
	}
	want := map[int]bool{}
	var missingDisks []int
	for _, n := range keep {
		want[n] = true
		if !have[n] {
			missingDisks = append(missingDisks, n)
		}
	}
	if len(missingDisks) > 0 {
		var all []int
		for n := range have {
			all = append(all, n)
		}
		sort.Ints(all)
		return &JobError{Code: CodeDiskNotFound,
			Detail: fmt.Sprintf("requested disks %v not found on the VM (disks: %v)", missingDisks, all)}
	}
	for _, d := range vbds {
		if want[d.UserDevice] {
			continue
		}
		if err := vm.SetVBDOwnerFalse(ctx, d.Ref); err != nil {
			return classify(err)
		}
		if d.VDIRef == "" || d.VDIRef == xapi.NullRef {
			continue
		}
		if err := vm.DestroyVDI(ctx, d.VDIRef); err != nil {
			return classify(err)
		}
		log.Info(i18n.T("vm.log.disk_removed"), "userdevice", d.UserDevice)
	}
	return nil
}

// cleanupSnapshot 은 성공·실패·취소와 무관하게 임시 스냅샷을 지운다(취소와 무관한 새 ctx).
// 실패하면 결과에 snapshot_cleanup_failed·snapshot_cleanup_error 를 남긴다(ncc-xen 과 같은 필드).
func (s *Service) cleanupSnapshot(vm *xapi.VMClient, snap *tempSnapshot, keep bool, result map[string]interface{}, log *logger.Entry) {
	if snap == nil {
		return
	}
	if keep {
		result["snapshot_uuid"] = snap.uuid
		return
	}
	if err := s.uninstall(vm, snap, log); err != nil {
		result["snapshot_cleanup_failed"] = snap.uuid
		result["snapshot_cleanup_error"] = errText(err)
	}
}

// cleanupRetryInterval 은 스냅샷 디스크가 아직 사용 중일 때 다시 지우기까지 기다리는 시간이다.
var cleanupRetryInterval = 2 * time.Second

func (s *Service) uninstall(vm *xapi.VMClient, snap *tempSnapshot, log *logger.Entry) error {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	for attempt := 1; ; attempt++ {
		err := vm.UninstallSnapshot(ctx, snap.ref)
		if err == nil {
			log.Info(i18n.T("vm.log.snapshot_removed"), "snapshotUUID", snap.uuid, "attempts", attempt)
			return nil
		}
		// 취소 직후에는 XenServer 가 내보내기를 멈추며 디스크를 잠시 더 잡고 있다(2026-11-19 실기 I07).
		// 같은 상태가 풀릴 때까지 cleanupTimeout 안에서 다시 지운다. UninstallSnapshot 은 이미 지운
		// 디스크를 건너뛰므로 다시 불러도 된다.
		if !retryableCleanup(err) || ctx.Err() != nil {
			log.Warn(i18n.T("vm.log.snapshot_cleanup_failed"), "snapshotUUID", snap.uuid, "attempts", attempt, "error", err)
			return err
		}
		select {
		case <-ctx.Done():
			log.Warn(i18n.T("vm.log.snapshot_cleanup_failed"), "snapshotUUID", snap.uuid, "attempts", attempt, "error", err)
			return err
		case <-time.After(cleanupRetryInterval):
		}
	}
}

func retryableCleanup(err error) bool {
	var xe *xapi.XAPIError
	if !errors.As(err, &xe) {
		return false
	}
	return xe.Code == "VDI_IN_USE" || xe.Code == "OTHER_OPERATION_IN_PROGRESS"
}

func verifyMap(path string, vr xva.VerifyResult, now time.Time) map[string]interface{} {
	m := map[string]interface{}{
		"file_path":            path,
		"file_size_bytes":      vr.Size,
		"verified":             vr.Verified,
		"chunk_count":          vr.ChunkCount,
		"checksum_entries":     vr.ChecksumEntries,
		"compression_detected": vr.CompressionDetected,
		"format":               "xva",
		"completedAt":          completedAt(now),
	}
	if vr.SHA256 != "" {
		m["checksum_sha256"] = vr.SHA256
	}
	if !vr.Verified {
		m["failure_reasons"] = vr.Reasons
	}
	return m
}
