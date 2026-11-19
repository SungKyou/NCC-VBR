package vm

// verify.go 는 ncc-xen backend/internal/job/vm_backup_verify.go runVerify·sendVerifyResult(@043c6d5, CP-VM-07)의
// 결과 판정을 따른다: 파일을 열 수 없을 때만 failed, 손상은 succeeded + verified=false.

import (
	"context"

	"ncc-vbr/internal/i18n"
	logger "ncc-vbr/internal/stdlogger"
	"ncc-vbr/internal/vmconfig"
	"ncc-vbr/internal/xva"
)

func (s *Service) runVerify(ctx context.Context, job *Job, cfg *vmconfig.Loaded, p *progress, log *logger.Entry) outcome {
	full, err := xva.Resolve(pathSettings(cfg), job.Verify.FilePath)
	if err != nil {
		return outcome{err: err}
	}
	p.set("verifying", 1, pctMsg("vm.verify.running", 1))
	vr, err := xva.Verify(ctx, full, cfg.ProgressInterval(), func(pct int, _ int64) {
		p.set("verifying", pct, pctMsg("vm.verify.running", pct))
	})
	if err != nil {
		return outcome{err: err}
	}
	result := verifyMap(full, vr, s.now())
	log.Info(i18n.T("vm.log.verify_done"), "filePath", full, "verified", vr.Verified, "chunkCount", vr.ChunkCount)
	if !vr.Verified {
		return outcome{result: result, message: i18n.T("vm.verify.bad", vr.Reasons[0])}
	}
	return outcome{result: result, message: i18n.T("vm.verify.ok", full)}
}
