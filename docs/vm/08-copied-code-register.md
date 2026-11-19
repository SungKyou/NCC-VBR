# vbr_vm 복사 코드 대장

기준일: 2026-11-19 / 양식: [T11](../templates/T11-copied-code-register.md) / 상태: **복사·수정 완료, 단위 시험 통과**

## 1. 기준

| 항목 | 내용 |
|---|---|
| 원본 프로젝트 | ncc-xen, `c:\dev\ncc-xen`(backend, stdlogger). 읽기만 했다 |
| 기준 커밋 | `043c6d5`(2026-11-12). 복사 때(2026-11-19) 다시 확인: 복사한 파일에 커밋 뒤 변경 없음(`git status`) |
| 복사 방식 | import 금지. `go/internal/…`로 복사, import 경로를 `ncc-vbr/internal/…`로 바꿨다 |
| 원본 사본 | `docs/evidence/vm/copied-original/`(복사 시점 원본 그대로, `SOURCE.txt`에 커밋) |
| 실제 차이 | `docs/evidence/vm/copied-code.diff`(`git diff --no-index --ignore-cr-at-eol`) |
| 들여온 외부 라이브러리 | gin v1.12.0, viper v1.21.0, fsnotify v1.10.1, lumberjack v2.2.1, klauspost/compress v1.20.3. 간접 의존 x/net v0.61.0, quic-go v0.64.1은 보안 권고로 올렸다(04 DEC-VM-28) |
| 들여오지 않은 것 | ncc-xen 내부 패키지(`gateway`, `pool`, `redissource`, `xapisource`, `config`), Redis, PostgreSQL, OVF/OVA 코드, i18n 카탈로그(1,705줄) |
| 머리 주석 | 복사한 파일마다 첫머리에 `copied from ncc-xen <경로> @043c6d5 (CP-VM-nn), 2026-11-19`와 바뀐 부분 요약 |
| 1단계 대장과의 관계 | 1단계 CP-01~04(`xapi`), CP-10(`secret`)은 4개 서비스가 같이 쓴다. `xapi`에 더한 함수는 CP-VM-12. 1단계 [agent/08](../agent/08-copied-code-register.md)에도 한 줄 남겼다 |

## 2. 복사 목록

| 번호 | 원본 경로 | 대상 경로 | 가져온 범위 | 상태 |
|---|---|---|---|---|
| CP-VM-01 | `backend/internal/job/handler.go` | `go/internal/vm/request.go`, `go/internal/vm/api/api.go` | 요청·응답 구조체 모양, 202·중복 응답 문장 | 옮겨 고쳐 씀 |
| CP-VM-02 | `backend/internal/apiauth/apiauth.go` | `go/internal/apiauth/apiauth.go` | 파일 전체 | 복사·수정 완료 |
| CP-VM-03 | `backend/internal/job/redact.go` | `go/internal/redact/redact.go` | 키 이름 규칙, 재귀 가림 | 복사·수정 완료 |
| CP-VM-04 | `backend/internal/job/task_durability.go` | `go/internal/jobstore/store.go`, `outbox.go` | 설계(JSON Lines, 원자적 압축, 재기동 interrupted, outbox 재시도·Dead Letter·보관 기간) | **설계만 따르고 새로 씀**(아래 3장) |
| CP-VM-05 | `backend/internal/job/vm_backup_export.go` `runExport` | `go/internal/xva/export.go`, `go/internal/vm/export.go` | 스트리밍 내보내기, 동시 zstd, SHA256, task 최종 확인, 스냅샷 판단 | 옮겨 고쳐 씀 |
| CP-VM-06 | `backend/internal/job/vm_backup_restore.go` `runImport` | `go/internal/xva/import.go`, `go/internal/vm/restore.go` | 업로드, task 최종 대기, 후처리 | 옮겨 고쳐 씀 |
| CP-VM-07 | `backend/internal/job/vm_backup_verify.go` | `go/internal/xva/verify.go`, `go/internal/vm/verify.go` | 검증 전체, 결과 판정 | 옮겨 고쳐 씀 |
| CP-VM-08 | `backend/internal/job/vm_backup_compress.go` `decompressZstdIfNeeded` | `go/internal/xva/zstd.go` | zstd 해제 | 복사·수정 완료 |
| CP-VM-09 | `backend/internal/job/vm_backup_util.go` | `go/internal/xva/path.go` | 경로 규칙, sandbox | 복사·수정 완료 |
| CP-VM-10 | `backend/internal/i18n/i18n.go` | `go/internal/i18n/i18n.go` | 파일 전체(`watch.go`는 복사하지 않음) | 복사(머리 주석만) |
| CP-VM-11 | `stdlogger/logger/*.go` 9개 | `go/internal/stdlogger/` | 시험 파일 포함 전체 | 복사(머리 주석, gofmt 정렬만) |
| CP-VM-12 | `backend/pkg/xapi/mixins/vm.go` `SetNameLabelByRef`, `SetAffinity` | `go/internal/xapi/restore_ops.go` | 두 함수 | 복사·수정 완료. 나머지(`SR.get_by_uuid`, `task.cancel`, VBD·VDI, allowed_operations)는 1단계에 이미 있었다 |
| CP-VM-13 | `backend/demo.ps1`, `demo_modules/demo_common.ps1`, `demo_vm_backup.ps1` | `go/test/vm/demo.ps1`, `demo_modules/` | 진입점·모듈 구조, `Get-Input`(입력 버퍼 비우기), `Confirm-Run`, `Write-MenuHeader` | 구조를 따라 새로 씀 |

## 3. 바뀐 부분

### CP-VM-01 `handler.go`

| 구분 | 대상 | 원본 | 바꾼 뒤 | 이유 |
|---|---|---|---|---|
| 수정 | 요청 구조체 | gin `binding:"required"` | `vm.Validate`가 필수 값·형식을 보고 코드·필드를 돌려준다 | 05 9장 오류 코드 |
| 추가 | `Payload.Credentials` | 없음 | `{username, password}` | DEC-VM-19 |
| 수정 | 작업 실행 | 고루틴을 바로 띄움 | 작업 기록 → 대기열 → 작업자 | 03 6장, 처리 중 조회·취소 |
| 그대로 | 202 응답, 중복 문장 | `{taskId, status:"ACCEPTED", sentAt, message:""}`, `duplicate taskId — already accepted, not re-executed` | 같음 | 호환 |

### CP-VM-02 `apiauth.go`

| 구분 | 대상 | 원본 | 바꾼 뒤 | 이유 |
|---|---|---|---|---|
| 수정 | `EnvToken` | `NCC_XEN_API_TOKEN` | `VBR_VM_API_TOKEN` | 서비스마다 토큰 |
| 수정 | `Middleware` | 고정 토큰 인자, 빈 토큰이면 검사 안 함 | 요청마다 `(enabled, token)`을 읽는다. 켜졌는데 토큰이 비면 거부 | 설정 즉시 반영(DEC-VM-17) |
| 추가 | 401 본문 | `{"error": …}` | `code: "UNAUTHORIZED"` 더함(문장은 같음) | 05 9장 |

### CP-VM-03 `redact.go`

| 구분 | 대상 | 원본 | 바꾼 뒤 | 이유 |
|---|---|---|---|---|
| 수정 | 패키지·이름 | `job.redactSensitive` 등 비공개 | `redact.Value`, `IsSensitiveKey` | 다른 패키지에서 쓴다 |
| 수정 | 가림 문자열 | `***REDACTED***` | `***` | 05 8.1 |
| 삭제 | `redactedRequestForLogging` | Request 전용 | `vm.Redacted`로 새로 씀(계정 이름은 남기고 암호만 가림) | credentials 구조 |

### CP-VM-04 `task_durability.go` (설계만 따름)

| 따른 것 | 새로 쓴 것 |
|---|---|
| JSON Lines append-only, 임시 파일 + rename 압축, 줄 단위 읽기(전체 파싱 금지), 재기동 때 interrupted·자동 재개 없음, outbox pending/delivered/deadletter, 5xx·429·408 재시도와 max_age, 4xx 바로 Dead Letter, 대기가 비면 delivered 비움, Dead Letter 보관 기간 | `jobs.jsonl`(상태·진행률·결과를 상태가 바뀔 때마다 한 줄), 메모리 표, 조회·목록, 진행률 기록 간격 조절, 결과 주소가 없으면 쌓지 않음. ncc-xen 의 `accepted.jsonl`은 두지 않는다(접수 여부만 담아 조회에 못 쓴다) |

원본 908줄은 Middleware 전송(`gateway`)·i18n·stdlogger에 묶여 있어, 그대로 복사하면 고칠 곳이 새로 쓰는 것보다 많았다.

### CP-VM-05 `vm_backup_export.go`

| 구분 | 대상 | 원본 | 바꾼 뒤 | 이유 |
|---|---|---|---|---|
| 수정 | 구조 | 한 함수가 요청 해석부터 결과 송신까지 | `xva.Export`(uuid → 파일)와 `vm.runExport`(경로·세션·스냅샷·결과)로 나눔 | 03 2장(xva 는 상태를 모른다) |
| 추가 | 스냅샷 정책 | Running 만 | `snapshot_mode: always`, `disks`(항상 스냅샷), 스냅샷 uuid 대상에 `disks` 거부 | Q-VM-01, F-VM-02, DEC-VM-29 |
| 수정 | 스냅샷 정리 | `VM.destroy`만(스냅샷 VDI가 남을 수 있다) | 1단계 `UninstallSnapshot`(스냅샷 VDI → VM 레코드) | 1단계 PT-01 실기 |
| 추가 | 스냅샷 준비 | 없음 | 스냅샷을 일반 VM 으로(`set_is_a_template false`, `ha_always_run` 무시) | 1단계 동작(원본 vbr) |
| 추가 | 취소 | 없음 | ctx 가 끝나면 `task.cancel` | DEC-VM-09 |
| 수정 | task 최종 확인 | 12회(12초) | ctx 까지 | 1단계와 같음 |
| 수정 | 진행률 | 규약 2 processing 송신 | `Progress` 콜백 → 작업 기록 | 조회 API |
| 그대로 | `.partial`, `use_compression=true`, 동시 zstd, SHA256, rename 전 Windows 삭제, 결과 필드 이름 | | | 호환 |
| 추가 | 파일 권한 | 없음 | 0666(1단계 원본 chmod) | 기존 동작 |

### CP-VM-06 `vm_backup_restore.go`

| 구분 | 대상 | 원본 | 바꾼 뒤 | 이유 |
|---|---|---|---|---|
| 수정 | 순서 | zstd 해제 → 세션 → SR 확인 | 세션 → SR 확인 → zstd 해제 | 실기 E-05: 없는 SR 은 바로 실패해야 한다 |
| 수정 | 구조 | 한 함수 | `xva.Import`(업로드·task 결과 참조)와 `vm.runRestore`(경로·후처리·결과) | 03 2장 |
| 추가 | 취소 | 없음 | `task.cancel` | DEC-VM-09 |
| 그대로 | `restore`·`force`·`sr_id` 파라미터, ContentLength, 최종 대기 2초 간격, 템플릿 후처리 + `update_allowed_operations`, name_label·home_server 실패해도 성공 유지, 결과 필드 이름 | | | 호환 |
| 수정 | Home Server | uuid → `VM.get_by_uuid` → `set_affinity` | 가져온 VM 참조로 바로(CP-VM-12) | 호출 하나 줄임 |

### CP-VM-07 `vm_backup_verify.go`

| 구분 | 대상 | 원본 | 바꾼 뒤 | 이유 |
|---|---|---|---|---|
| 수정 | 실패 사유 문장 | i18n 키(`vm_backup.e00x`) | 고정 영어 문장 | 기계 진단이라 번역하지 않는다 |
| 추가 | 취소 | 없음 | 읽을 때마다 ctx 확인 | 처리 중 취소 |
| 그대로 | 판정(열기 실패만 오류, 손상은 verified=false), zstd·gzip 매직, 청크 SHA1·`.checksum`, 사유 10건 제한, SHA256 | | | 지시 11 |

### CP-VM-08 `vm_backup_compress.go`

| 구분 | 대상 | 원본 | 바꾼 뒤 | 이유 |
|---|---|---|---|---|
| 삭제 | `compressFileZstd` | 미사용(2026-10-16 이후) | 가져오지 않음 | 쓰지 않는 코드 |
| 수정 | 임시 파일 이름 | `.ncc-zstd-restore-*.xva` | `.vbr-vm-zstd-*.xva` | 이 서비스 이름 |
| 추가 | 취소 | 없음 | 읽을 때마다 ctx 확인, 취소되면 임시 파일 삭제 | 처리 중 취소 |

### CP-VM-09 `vm_backup_util.go`

| 구분 | 대상 | 원본 | 바꾼 뒤 | 이유 |
|---|---|---|---|---|
| 수정 | 실패 반환 | `(string, bool)` | `*xva.Error`(`INVALID_FIELD`, `PATH_NOT_ALLOWED`) | 오류 코드 |
| 추가 | `OutputName` | runExport 안 | 확장자 규칙(.xva, .xva.zst) | 한곳에 |
| 이동 | `xapiBoolField` | util | `vm.xapiBool` | 쓰는 곳 |

### CP-VM-10 `i18n.go`

그대로 복사했다. 바뀐 점은 둘이다.
- `watch.go`(locale 감시)는 가져오지 않았다. `vmconfig`가 설정 변경을 받아 `i18n.SetLocale`을 부른다. viper 감시를 하나로 줄였다.
- 카탈로그는 vbr_vm 문구만 새로 썼다(`catalog_ko.go`, `catalog_en.go`, `catalog_ja.go`, 키 44개). 세 언어의 키와 형식 지시자가 같은지는 `TestCatalogParity`가 본다.

### CP-VM-11 `stdlogger`

코드는 바꾸지 않았다(머리 주석, gofmt 정렬만). 운영에서 알아야 할 원본 동작은 이렇다. `logging.level`이 있으면 `time_format`, `time_zone`, `max_size_mb` 등도 모두 있어야 기동한다(`ValidateLoggingConfig`). `logging` 절을 통째로 빼면 기본값을 쓴다. 05 문서 10장 예와 데모 설정에 전체 키를 적었다.

### CP-VM-12 `mixins/vm.go`

| 구분 | 대상 | 원본 | 바꾼 뒤 | 이유 |
|---|---|---|---|---|
| 수정 | 오류 문장 | `i18n.RawT` | 고정 영어 | `xapi` 패키지의 다른 함수와 같은 방식 |
| 수정 | `SetAffinity` | VM uuid 를 받음 | `SetAffinityByRef`: VM 참조를 받음 | 가져온 직후라 참조가 있다 |

### CP-VM-13 `demo.ps1`

| 따른 것 | 바꾼 것 |
|---|---|
| 진입점 + `demo_modules\` 구조, 메뉴, `Get-Input`의 입력 버퍼 비우기(ncc-xen 2026-08-13), 요청 본문 출력 | 결과를 조회 API 로 기다려 바로 판정(PASS/FAIL 집계). 자동 모드(가짜 XAPI 두 개 + vbr_vm 을 띄우고 16개 시나리오). **토큰·암호를 소스에 두지 않는다**: ncc-xen `demo.ps1`에는 시험 환경 토큰 값이 소스에 들어 있는데(`$DEFAULT_API_TOKEN`) 가져오지 않았다. 암호는 화면 입력 뒤 `vbr_secret encrypt`로 ENC 값으로 바꿔 보낸다. dom0 확인·정리 명령은 출력만 한다 |

## 4. 원본 변경 추적

| 비교일 | 원본 커밋 범위 | 복사한 파일에 해당하는 변경 | 반영 여부와 이유 |
|---|---|---|---|
| 2026-11-19 | `043c6d5` 기준 복사 | - | - |
