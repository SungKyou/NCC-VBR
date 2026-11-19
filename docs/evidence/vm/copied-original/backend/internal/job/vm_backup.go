package job

// vm_backup.go — VM_BACKUP jobType (신규, 2026-07-16)
//
// 🆕 2026-09-01: 파일 분리 리팩터링 — 기존 vm_backup.go(3,429줄) 단일 파일을
// 같은 패키지(job) 내에서 기능별로 여러 파일로 나눔 (Go는 패키지 내 파일
// 경계가 컴파일/런타임에 영향을 주지 않으므로 순수 기계적 분리이며 로직
// 변경 없음). 이 파일은 구조체 정의 + 액션 디스패처(Execute)만 담당하고,
// 실제 구현은 다음 파일들에 있습니다:
//   vm_backup_export.go      — export (XVA)
//   vm_backup_ovf_export.go  — OVF/OVA export
//   vm_backup_verify.go      — verify (XVA)
//   vm_backup_restore.go     — restore (XVA import)
//   vm_backup_ovf_restore.go — OVF/OVA restore (import)
//   vm_backup_compress.go    — zstd 압축/해제 헬퍼
//   vm_backup_util.go        — 공용 유틸리티
//
// 원본 파일 상단에 있던 전체 설계 주석(export/verify/restore/OVF 동작 설명)은
// 아래에 그대로 보존합니다 (문서 삭제 금지 원칙).
//
// 지원 action:
//   export : VM을 XVA 파일로 내보내기 (XenCenter "Export to File" 대응)
//   verify : export된 XVA 파일의 무결성 검증 (XenCenter "Verify export on completion" 대응, 별도 action)
//
// ── export 동작 ──────────────────────────────────────────────
//   XVA export는 XML-RPC가 아니라 XAPI HTTP 핸들러입니다:
//     GET https://<master>/export?session_id=...&uuid=...&task_id=...
//   Backend가 스트림을 받아 파일로 저장합니다. 따라서 저장 경로는
//   Backend 프로세스가 접근 가능한 경로여야 합니다 (NFS 마운트 등).
//
//   VM 상태별 처리 (/export는 Halted/Suspended/Snapshot만 허용):
//     Halted    : 그대로 export
//     Running   : 임시 스냅샷(disk-only, crash-consistent) 생성 → 스냅샷 export → 스냅샷 삭제
//     Suspended : 1단계 미지원 — memory image 포함 옵션 실기검증 후 지원 예정
//     스냅샷 UUID를 직접 지정한 경우: 상태 무관 그대로 export
//
//   진행률: task.create로 XAPI Task를 만들어 export URL의 task_id로 전달,
//   TaskGetProgress 폴링 → SendProcessing (vm_control copy와 동일 패턴, 1~99%).
//
// ── 경로 규칙 ────────────────────────────────────────────────
//   options.export_path가 절대경로(Ubuntu "/...", Windows "C:\...")면
//   settings.SandboxMode=false(기본값, 기존 동작)일 때 그대로 사용, true일
//   때는 BasePath/AllowedAbsolutePaths 하위로만 제한(2026-09-18, 코드
//   리뷰 지적 대응 — vm_backup_util.go의 resolvePathWithSandbox 참고).
//   상대경로면 sysconfig.yaml backup.base_path 하위로 결합.
//   상위 디렉토리 참조("..")와 file_name 내 경로 구분자/특수문자는 거부.
//
// ── verify 동작 ──────────────────────────────────────────────
//   XVA(tar) 파일을 다시 읽어 검증 (Pool 세션 불필요 — 파일만 검증):
//     1) gzip 매직바이트 감지 시 압축 해제 (compression=gzip으로 export한 파일)
//     2) tar 구조 무결성 확인 + ova.xml 존재 확인
//     3) 데이터 청크별 SHA1 계산 → 내부 .checksum 항목과 대조
//        (.checksum 항목이 하나도 없으면 tar 구조 검증만 수행 — 설계상 폴백)
//     4) 파일 전체 SHA256 산출 → export 시 응답의 checksum_sha256과 교차 검증 가능
//   파일 열기 실패만 failure로 보고하고, 검증 불일치/손상은 verify 작업 자체는
//   success로 보고하되 options.verified=false + 사유를 포함합니다.
//
// ── restore 동작 (2단계, 2026-07-27 신규) ─────────────────────
//   restore/import 관련 상세 설계 주석은 vm_backup_restore.go,
//   vm_backup_ovf_restore.go 파일 상단을 참고하세요 (분리 시 원본 그대로 이동).
//
// ── OVF/OVA export (3단계) ───────────────────────────────────
//   OVF/OVA export 관련 상세 설계 주석은 vm_backup_ovf_export.go 파일
//   상단을 참고하세요 (분리 시 원본 그대로 이동).

import (
	"context"
	"strings"
	"time"

	"github.com/ncc/stdlogger/logger"
	"github.com/ncc/xen-backend/internal/gateway"
	"github.com/ncc/xen-backend/internal/i18n"
)

type BackupSettings struct {
	BasePath     string // export_path가 상대경로일 때의 기준 디렉토리
	TimeoutHours int    // export/verify job 타임아웃 (시간)
	// SandboxMode: 2026-09-18 코드 리뷰 지적 대응. false(기본값 — 명시적으로
	// 켜지 않으면 기존 배포와 100% 동일하게 동작)면 export_path/file_path의
	// 절대경로를 이전처럼 그대로 허용합니다. true면 BasePath 또는
	// AllowedAbsolutePaths 하위(symlink까지 해석한 실제 경로 기준)로만
	// 절대경로를 허용하고, 그 외는 거부합니다. API 인증이 뚫렸을 때
	// Backend OS 권한 범위 내 임의 경로 쓰기/읽기를 막기 위한 운영 강화
	// 옵션이라, 새로 도입하는 옵션이므로 하위 호환을 위해 기본값은 꺼둔
	// 상태입니다 — 운영 환경에서는 sysconfig.yaml에서 켜는 것을 권장합니다.
	SandboxMode bool
	// AllowedAbsolutePaths: SandboxMode=true일 때 BasePath 외에 추가로
	// 허용할 절대경로 루트 목록(예: 별도 NAS 마운트). 비어 있으면
	// BasePath 하위만 허용됩니다.
	AllowedAbsolutePaths []string
}

// VMBackupExecutor는 VM_BACKUP jobType을 처리합니다.
type VMBackupExecutor struct {
	queryBase
	settings BackupSettings
}

func NewVMBackupExecutor(sessions *PoolSessionRegistry, gw *gateway.Client, settings BackupSettings, log *logger.Entry) *VMBackupExecutor {
	if settings.BasePath == "" {
		settings.BasePath = "./backups"
	}
	if settings.TimeoutHours <= 0 {
		settings.TimeoutHours = 4
	}
	return &VMBackupExecutor{
		queryBase: queryBase{sessions: sessions, gw: gw, log: log, jobType: "VM_BACKUP"},
		settings:  settings,
	}
}

func (e *VMBackupExecutor) JobType() string { return "VM_BACKUP" }

func (e *VMBackupExecutor) Execute(req Request) {
	action := strings.ToLower(req.Action)

	e.log.Info(i18n.T("vm_backup.l001"),
		"taskId", req.TaskID,
		"action", action,
		"targetUUID", req.Payload.TargetUUID,
		"masterIP", req.Payload.MasterIP,
	)

	// export는 장시간 작업 — cross-pool migration과 동일하게 시간 단위 타임아웃 사용
	timeout := time.Duration(e.settings.TimeoutHours) * time.Hour
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	switch action {
	case "export":
		e.runExport(ctx, req)
	case "verify":
		e.runVerify(ctx, req)
	case "restore":
		// ── format 분기 (🆕 OVF/OVA import, 2026-08-12; ovf 낱개 파일 지원 추가, 2026-08-13) ──
		// format=ova/ovf는 XVA restore(runImport)와 완전히 다른 파이프라인
		// (OVF 서술자 파싱 + template clone + 디스크별 VDI.create/import_raw_vdi)이라
		// export의 format 분기와 동일한 패턴으로 별도 함수로 분리한다.
		// 기존 XVA restore 경로(runImport)는 무변경.
		// ova/ovf 세부 판정(tar 묶음 vs 디렉토리 낱개 파일)은 runImportOVA 내부에서
		// file_path 확장자로 자동 판단한다(2026-08-13 결정).
		format, _ := strOpt(req.Payload.Options, "format")
		format = strings.ToLower(strings.TrimSpace(format))
		if format == "ova" || format == "ovf" {
			e.runImportOVA(ctx, req)
			return
		}
		e.runImport(ctx, req)
	default:
		e.sendFailure(ctx, req, i18n.T("vm_backup.m001", req.Action))
	}
}

// ─────────────────────────────────────────────────────────────────
// export
// ─────────────────────────────────────────────────────────────────

