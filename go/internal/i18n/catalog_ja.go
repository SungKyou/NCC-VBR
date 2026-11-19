package i18n

// catalog_ja.go 는 vbr_vm 문구(일본어)다. 키는 catalog_ko.go 와 같다.
// ncc-xen 과 같이 네이티브 검수 전이다(docs/vm/03-tobe-design.md 13장).

func init() {
	registerCatalog(JA, map[string]string{
		"vm.job.queued":      "待機中",
		"vm.job.running":     "処理開始",
		"vm.job.canceling":   "キャンセル中",
		"vm.job.canceled":    "タスクがキャンセルされました",
		"vm.job.interrupted": "サービス停止によりタスクが中断されました",
		"vm.job.timeout":     "タスクの制限時間を超えました",
		"vm.job.failed":      "タスク失敗 (%s)",

		"vm.export.snapshot": "一時スナップショットを作成中",
		"vm.export.running":  "VM をエクスポート中 (%d%%)",
		"vm.export.cleanup":  "一時スナップショットを削除中",
		"vm.export.done":     "VM バックアップ完了: %s",

		"vm.verify.running": "XVA を検証中 (%d%%)",
		"vm.verify.ok":      "XVA 検証成功: %s",
		"vm.verify.bad":     "XVA 検証不一致: %s",

		"vm.restore.decompressing": "zstd を展開中 (%d%%)",
		"vm.restore.running":       "VM をインポート中 (%d%%)",
		"vm.restore.postprocess":   "復元後の処理中",
		"vm.restore.done":          "VM 復元完了: %s",

		"vm.log.service_start":           "VBR VM バックアップサービス開始",
		"vm.log.service_stop":            "VBR VM バックアップサービス終了",
		"vm.log.listen":                  "HTTP 受信開始",
		"vm.log.test_switch":             "テスト用の設定が有効です",
		"vm.log.config_warn":             "設定の警告",
		"vm.log.restart_recovery":        "再起動復旧: 未完了のタスクを中断扱いにしました",
		"vm.log.accepted":                "タスク受付",
		"vm.log.duplicate":               "同じ taskId は受付済み(再実行しません)",
		"vm.log.rejected":                "リクエスト拒否",
		"vm.log.started":                 "タスク開始",
		"vm.log.finished":                "タスク終了",
		"vm.log.finished_error":          "タスクがエラーで終了しました",
		"vm.log.store_failed":            "タスク記録の書き込みに失敗",
		"vm.log.outbox_failed":           "結果報告キューへの書き込みに失敗",
		"vm.log.shutdown_timeout":        "終了待ち時間を超えました。処理中タスクの後片付けが終わっていない可能性があります",
		"vm.log.login":                   "XAPI ログイン成功",
		"vm.log.snapshot":                "一時スナップショット作成",
		"vm.log.disk_removed":            "選択されていないディスクを一時スナップショットから除外",
		"vm.log.snapshot_removed":        "一時スナップショット削除",
		"vm.log.snapshot_cleanup_failed": "一時スナップショットの削除に失敗",
		"vm.log.export_start":            "VM エクスポート開始",
		"vm.log.export_done":             "VM エクスポート完了",
		"vm.log.import_start":            "VM インポート開始",
		"vm.log.import_done":             "VM インポート完了",
		"vm.log.verify_done":             "XVA 検証終了",
		"vm.log.compact_failed":          "タスク記録の整理に失敗",
	})
}
