package i18n

// catalog_ko.go 는 vbr_vm 문구(한국어, 기본)다. 키 이름은 vm.<영역>.<이름>(docs/vm/03-tobe-design.md 13장).
// ncc-xen 카탈로그는 복사하지 않았다(CP-VM-10). 키를 더하면 en, ja 에도 같은 키를 더한다(시험이 확인한다).

func init() {
	registerCatalog(KO, map[string]string{
		"vm.job.queued":      "대기 중",
		"vm.job.running":     "처리 시작",
		"vm.job.canceling":   "취소하는 중",
		"vm.job.canceled":    "작업이 취소됨",
		"vm.job.interrupted": "서비스가 멈춰 작업이 중단됨",
		"vm.job.timeout":     "작업 시간 제한을 넘음",
		"vm.job.failed":      "작업 실패 (%s)",

		"vm.export.snapshot": "임시 스냅샷 만드는 중",
		"vm.export.running":  "VM 내보내는 중 (%d%%)",
		"vm.export.cleanup":  "임시 스냅샷 정리 중",
		"vm.export.done":     "VM 백업 완료: %s",

		"vm.verify.running": "XVA 검증 중 (%d%%)",
		"vm.verify.ok":      "XVA 검증 통과: %s",
		"vm.verify.bad":     "XVA 검증 불일치: %s",

		"vm.restore.decompressing": "zstd 압축 푸는 중 (%d%%)",
		"vm.restore.running":       "VM 가져오는 중 (%d%%)",
		"vm.restore.postprocess":   "복구 후처리 중",
		"vm.restore.done":          "VM 복구 완료: %s",

		"vm.log.service_start":           "VBR VM 백업 서비스 시작",
		"vm.log.service_stop":            "VBR VM 백업 서비스 종료",
		"vm.log.listen":                  "HTTP 수신 시작",
		"vm.log.test_switch":             "시험용 설정이 켜져 있음",
		"vm.log.config_warn":             "설정 경고",
		"vm.log.restart_recovery":        "재기동 복구: 끝나지 않은 작업을 중단 처리함",
		"vm.log.accepted":                "작업 접수",
		"vm.log.duplicate":               "같은 taskId 가 이미 접수됨(다시 실행하지 않음)",
		"vm.log.rejected":                "요청 거부",
		"vm.log.started":                 "작업 시작",
		"vm.log.finished":                "작업 끝",
		"vm.log.finished_error":          "작업이 오류로 끝남",
		"vm.log.store_failed":            "작업 기록 쓰기 실패",
		"vm.log.outbox_failed":           "결과 보고 대기열 쓰기 실패",
		"vm.log.shutdown_timeout":        "종료 대기 시간을 넘음. 처리 중 작업의 정리가 끝나지 않았을 수 있음",
		"vm.log.login":                   "XAPI 로그인 성공",
		"vm.log.snapshot":                "임시 스냅샷 생성",
		"vm.log.disk_removed":            "고르지 않은 디스크를 임시 스냅샷에서 뺌",
		"vm.log.snapshot_removed":        "임시 스냅샷 삭제",
		"vm.log.snapshot_cleanup_failed": "임시 스냅샷 삭제 실패",
		"vm.log.export_start":            "VM 내보내기 시작",
		"vm.log.export_done":             "VM 내보내기 완료",
		"vm.log.import_start":            "VM 가져오기 시작",
		"vm.log.import_done":             "VM 가져오기 완료",
		"vm.log.verify_done":             "XVA 검증 끝",
		"vm.log.compact_failed":          "작업 기록 정리 실패",
	})
}
