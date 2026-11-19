package vm

// restore.go 의 순서는 ncc-xen backend/internal/job/vm_backup_restore.go runImport(@043c6d5, CP-VM-06)를 따른다.
// 바뀐 점: SR 확인을 zstd 해제보다 먼저 한다(1단계 실기 E-05: 없는 SR 은 파일을 보내기 전에 바로 실패해야 한다.
// ncc-xen 은 큰 zstd 파일을 다 푼 뒤에야 SR 오류를 알린다).

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"ncc-vbr/internal/i18n"
	logger "ncc-vbr/internal/stdlogger"
	"ncc-vbr/internal/vmconfig"
	"ncc-vbr/internal/xva"
)

func (s *Service) runRestore(ctx context.Context, job *Job, cfg *vmconfig.Loaded, p *progress, log *logger.Entry) outcome {
	o := job.Restore
	full, err := xva.Resolve(pathSettings(cfg), o.FilePath)
	if err != nil {
		return outcome{err: err}
	}
	info, err := os.Stat(full)
	if err != nil {
		return outcome{err: &JobError{Code: CodeFileIO, Detail: err.Error()}}
	}

	sess, err := login(ctx, job.Req.Payload, cfg, log)
	if err != nil {
		return outcome{err: err}
	}
	defer sess.close()
	srRef, err := sess.vm.SRByUUID(ctx, o.SRUUID)
	if err != nil {
		return outcome{err: classify(err)}
	}

	// zstd 면 임시 파일로 푼다(XAPI 는 zstd 를 모른다). 진행률 0~30.
	upload, uploadSize, isZstd, err := s.decompress(ctx, full, info.Size(), cfg, p)
	if err != nil {
		return outcome{err: err}
	}
	if isZstd {
		defer os.Remove(upload)
	}
	base := 0
	if isZstd {
		base = 30
	}
	start := s.now()
	log.Info(i18n.T("vm.log.import_start"), "filePath", full, "srUUID", o.SRUUID, "restoreOriginalUUID", o.RestoreOriginalUUID)
	p.set("uploading", base+1, pctMsg("vm.restore.running", base+1))
	refs, err := xva.Import(ctx, xva.ImportParams{
		Client: sess.client, VM: sess.vm, HTTP: sess.http, Path: upload, SRRef: srRef,
		Restore: o.RestoreOriginalUUID, Force: o.Force, TaskLabel: "NCC-VBR VM Import",
		Interval: cfg.ProgressInterval(),
		Progress: func(pct int, _ int64) {
			v := base + pct*(99-base)/99
			p.set("importing", v, pctMsg("vm.restore.running", v))
		},
	})
	if err != nil {
		return outcome{err: err}
	}
	p.set("postprocess", 99, i18n.T("vm.restore.postprocess"))
	newRef := refs[0]
	rec, err := sess.vm.GetRecord(ctx, newRef)
	if err != nil {
		return outcome{err: classify(err)}
	}
	newUUID, _ := rec["uuid"].(string)
	result := map[string]interface{}{
		"vm_uuid":               newUUID,
		"sr_uuid":               o.SRUUID,
		"file_path":             full,
		"file_size_bytes":       info.Size(),
		"restore_original_uuid": o.RestoreOriginalUUID,
		"is_a_template_fixed":   false,
	}
	// 스냅샷 기반 XVA 는 템플릿으로 생기는 XAPI 동작 → 템플릿 플래그 끄기 + allowed_operations 재계산(ncc-xen 17.8 ⑤)
	if xapiBool(rec["is_a_template"]) {
		if err := sess.vm.SetIsATemplate(ctx, newRef, false); err != nil {
			result["is_a_template_fix_error"] = errText(err)
		} else {
			result["is_a_template_fixed"] = true
			if err := sess.vm.UpdateAllowedOperations(ctx, newRef); err != nil {
				result["allowed_operations_fix_error"] = errText(err)
			}
		}
	}
	if o.NameLabel != "" {
		result["name_label"] = o.NameLabel
		if err := sess.vm.SetNameLabelByRef(ctx, newRef, o.NameLabel); err != nil {
			result["name_label_set_error"] = errText(err)
		}
	}
	if o.HomeServerUUID != "" {
		result["home_server_uuid"] = o.HomeServerUUID
		if err := sess.vm.SetAffinityByRef(ctx, newRef, o.HomeServerUUID); err != nil {
			result["home_server_applied"] = false
			result["home_server_set_error"] = errText(err)
		} else {
			result["home_server_applied"] = true
		}
	}
	if len(refs) > 1 {
		result["additional_vm_refs_ignored"] = len(refs) - 1
	}
	if isZstd {
		result["compression_detected"] = xva.CompressionZstd
		result["decompressed_size_bytes"] = uploadSize
	}
	result["duration_seconds"] = seconds(s.now().Sub(start))
	result["completedAt"] = completedAt(s.now())
	log.Info(i18n.T("vm.log.import_done"), "vmUUID", newUUID)
	return outcome{result: result, message: i18n.T("vm.restore.done", newUUID)}
}

// decompress 는 zstd 면 풀고 진행률을 알린다.
func (s *Service) decompress(ctx context.Context, full string, size int64, cfg *vmconfig.Loaded, p *progress) (string, int64, bool, error) {
	var read int64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(cfg.ProgressInterval())
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if size > 0 {
					v := int(float64(atomic.LoadInt64(&read))/float64(size)*29) + 1
					p.set("decompressing", v, pctMsg("vm.restore.decompressing", v))
				}
			}
		}
	}()
	path, n, isZstd, err := xva.DecompressIfZstd(ctx, full, &read)
	close(stop)
	<-done
	if err != nil {
		return "", 0, false, err
	}
	if !isZstd {
		return full, size, false, nil
	}
	return path, n, true, nil
}

func joinPath(dir, name string) string { return filepath.Join(dir, name) }
