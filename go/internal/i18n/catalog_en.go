package i18n

// catalog_en.go 는 vbr_vm 문구(영어)다. 키는 catalog_ko.go 와 같다.

func init() {
	registerCatalog(EN, map[string]string{
		"vm.job.queued":      "Queued",
		"vm.job.running":     "Started",
		"vm.job.canceling":   "Canceling",
		"vm.job.canceled":    "Task canceled",
		"vm.job.interrupted": "Task interrupted because the service stopped",
		"vm.job.timeout":     "Task exceeded the time limit",
		"vm.job.failed":      "Task failed (%s)",

		"vm.export.snapshot": "Creating a temporary snapshot",
		"vm.export.running":  "Exporting VM (%d%%)",
		"vm.export.cleanup":  "Removing the temporary snapshot",
		"vm.export.done":     "VM backup completed: %s",

		"vm.verify.running": "Verifying XVA (%d%%)",
		"vm.verify.ok":      "XVA verification passed: %s",
		"vm.verify.bad":     "XVA verification mismatch: %s",

		"vm.restore.decompressing": "Decompressing zstd (%d%%)",
		"vm.restore.running":       "Importing VM (%d%%)",
		"vm.restore.postprocess":   "Post-processing the restored VM",
		"vm.restore.done":          "VM restore completed: %s",

		"vm.log.service_start":           "VBR VM backup service started",
		"vm.log.service_stop":            "VBR VM backup service stopped",
		"vm.log.listen":                  "HTTP listening",
		"vm.log.test_switch":             "A test-only setting is enabled",
		"vm.log.config_warn":             "Config warning",
		"vm.log.restart_recovery":        "Restart recovery: unfinished tasks marked interrupted",
		"vm.log.accepted":                "Task accepted",
		"vm.log.duplicate":               "Duplicate taskId already accepted (not re-executed)",
		"vm.log.rejected":                "Request rejected",
		"vm.log.started":                 "Task started",
		"vm.log.finished":                "Task finished",
		"vm.log.finished_error":          "Task finished with an error",
		"vm.log.store_failed":            "Task record write failed",
		"vm.log.outbox_failed":           "Result report enqueue failed",
		"vm.log.shutdown_timeout":        "Shutdown wait exceeded; running task cleanup may be incomplete",
		"vm.log.login":                   "XAPI login success",
		"vm.log.snapshot":                "Temporary snapshot created",
		"vm.log.disk_removed":            "Unselected disk removed from the temporary snapshot",
		"vm.log.snapshot_removed":        "Temporary snapshot removed",
		"vm.log.snapshot_cleanup_failed": "Temporary snapshot removal failed",
		"vm.log.export_start":            "VM export started",
		"vm.log.export_done":             "VM export completed",
		"vm.log.import_start":            "VM import started",
		"vm.log.import_done":             "VM import completed",
		"vm.log.verify_done":             "XVA verification finished",
		"vm.log.compact_failed":          "Task record compaction failed",
	})
}
