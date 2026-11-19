# agent 복사 코드 대장 (ncc-xen → ncc-vbr)

기준일: 2026-11-17 / 양식: [T11](../templates/T11-copied-code-register.md) / 상태: 복사·수정 완료, 단위 시험 통과
근거 결정: [00-rescope-review.md](00-rescope-review.md) DEC-AG-07, 5장
실제 차이: [`evidence/agent/copied-code.diff`](../evidence/agent/copied-code.diff) (ncc-xen 원본 대비 `diff -u`, 2026-11-17 생성)

## 1. 기준

| 항목 | 내용 |
|---|---|
| 원본 프로젝트 | ncc-xen, `c:\dev\ncc-xen` (모듈 `github.com/ncc/xen-backend`, `ncc-xen/stdcrypto`) |
| 기준 커밋 | `043c6d5` (2026-11-12) |
| 복사 방식 | import 하지 않았다. `go.mod`에 ncc-xen 모듈이 없다(외부 의존 0) |
| 들여오지 않은 것 | `stdlogger`, `internal/i18n`, `klauspost/compress`(zstd), `golang.org/x/crypto`, `gopkg.in/yaml.v3`, Redis, gin |
| 기계적 변환 | 스크립트로 했다: `i18n.RawT("키")` → ncc-xen `catalog_en.go`의 영어 문장, i18n·xapi import 제거, `package mixins` → `xapi`, `*xapi.Client` → `*Client`, CRLF → LF(gofmt) |
| 머리 주석 | 복사한 파일마다 `// 출처: … (커밋 043c6d5, 2026-11-17 복사)` 와 이 대장의 CP 번호 |

## 2. 복사 목록

| 번호 | 원본 경로 | 대상 경로 | 가져온 범위 | 상태 |
|---|---|---|---|---|
| CP-01 | `backend/pkg/xapi/xmlrpc.go` | `go/internal/xapi/xmlrpc.go` | 파일 전체 | 복사·수정 완료 |
| CP-02 | `backend/pkg/xapi/client.go` | `go/internal/xapi/client.go` | 파일 전체 | 복사·수정 완료 |
| CP-03 | `backend/pkg/xapi/mixins/vm.go` | `go/internal/xapi/vm.go` | 함수 13개: GetRecord, GetByUUID, Snapshot, DestroyByRef, GetUUIDByRef, TaskCreate, TaskDestroy, TaskGetStatus, TaskGetProgress, TaskGetErrorInfo, TaskGetResultRefs(+ `taskResultRefPattern`), SetIsATemplate, UpdateAllowedOperations | 복사 완료(함수 본문 변경 없음) |
| CP-04 | `backend/pkg/xapi/mixins/task.go` | `go/internal/xapi/task.go` | 파일 전체 | 복사 완료(기계적 변환만) |
| CP-05 | `mixins/sr.go`, `host.go`, `pool.go` | - | - | **보류**. 조회(`I`) 기능을 후순위로 미뤄 필요 없다(C-AG-13) |
| CP-06 | `backend/internal/job/vm_backup_export.go` `runExport` 203~375행 | `go/internal/agent/transfer.go` `export` | 내보내기 HTTP 흐름 | 옮겨 고쳐 씀(아래 3장) |
| CP-07 | `backend/internal/job/vm_backup_restore.go` `runImport` 158~353행 | `go/internal/agent/transfer.go` `importFile` | 가져오기 HTTP 흐름, 템플릿 플래그 후처리 | 옮겨 고쳐 씀 |
| CP-08 | `backend/internal/job/vm_backup_verify.go` | - | - | **복사하지 않음**. XVA 검증은 따로 진행(사용자 결정) |
| CP-09 | `backend/internal/job/vm_backup_util.go` | - | - | **복사하지 않음**. 설정 구조에 묶여 있어 같은 규칙을 `agent/config.go` `insideRoots`에 새로 썼다 |
| CP-10 | `stdcrypto/secret/*.go` 10개 | `go/internal/secret/` | aes256gcm, algorithm, api, config, envelope, errors, keyring, filemode_unix, filemode_other, load | 복사·수정 완료 |

새로 작성한 것(복사 아님): `go/internal/xapi/backup_ops.go`(SetHAAlwaysRun, DiskVBDs, SetVBDOwnerFalse, DestroyVDI, UninstallSnapshot, CancelTask, SRByUUID), `go/internal/agent/*`, `go/cmd/vbr_agent`, `go/cmd/vbr_secret`.

## 3. 바뀐 부분

`copied-code.diff`의 변경 줄 수: client.go 90, task.go 26, xmlrpc.go 23, secret/load.go 20, secret/config.go 12, 그 밖 secret 파일 각 2(머리 주석).

### CP-01 `xmlrpc.go`

| 구분 | 대상 | 바꾼 것 | 이유 |
|---|---|---|---|
| 수정 | 오류 문장 10곳 | `i18n.RawT("pkg_xapi_xmlrpc.e0xx")` → 영어 문장 | i18n 패키지를 들여오지 않는다 |
| 삭제 | import | `internal/i18n` | 같다 |

### CP-02 `client.go`

| 구분 | 대상 | 바꾼 것 | 이유 |
|---|---|---|---|
| 수정 | `Client.log`, `NewClient` | `*logger.Entry` → 새 `Logger` 인터페이스(`Printf`), nil 허용. `logf` 추가 | `stdlogger`를 들여오지 않는다. ncc-vbr `logx`가 만족한다 |
| 수정 | `Login` | 마스터 전환 로그를 `logf`로, 로그인 성공 디버그 로그 삭제, 클라이언트 이름 `ncc-xen-backend` → `ncc-vbr-agent` | 같다. XAPI 세션 기록에 이 프로그램 이름이 남게 |
| 수정 | `call` | User-Agent `ncc-xen-backend/1.0` → `ncc-vbr-agent/3.0` | 같다 |
| 삭제 | `call` 의 migrate 디버그 로그 두 곳 | 요청·응답 XML 출력 | 이 프로그램은 migrate 를 부르지 않는다 |
| 수정 | 오류 문장 | `i18n.RawT` → 영어 문장 | i18n |
| 유지 | `redactSessionRefInLog` | 지금은 부르는 곳이 없지만 지우지 않았다 | 원본과의 차이를 줄인다 |

### CP-03 `vm.go`, CP-04 `task.go`

함수 본문은 원본과 같다(i18n 치환만). `copied-code.diff` 끝에 함수별 비교 결과가 있다(13개 모두 "원본과 같음").
`TaskGetErrorInfo`의 "알 수 없는 오류" 한국어 문장도 원본 그대로다.

### CP-06·07 `transfer.go` (옮겨 고쳐 씀)

| 구분 | 원본(ncc-xen) | 전환본 | 이유 |
|---|---|---|---|
| 수정 | `Request`, `BackupSettings`, `poolSession` 에서 값을 꺼낸다 | 인자로 받는다 | NX-06 |
| 수정 | 주소 `https://<masterIP>/export` | 마스터 전환 뒤의 `BaseURL` 에 경로를 붙인다 | 세션 구조가 없다 |
| 삭제 | 진행률 고루틴과 게이트웨이 송신(`SendProcessing`) | 없음 | NX-05. 관리 서버는 `Q`로 상태를 본다 |
| 삭제 | zstd 압축·해제, sha256 계산, OVF 분기, `keep_snapshot`·`overwrite` 옵션, 홈 서버·이름 지정 후처리 | 없음 | NX-04. 원본 vbr 에 없는 기능 |
| 수정 | 스냅샷은 켜진 VM만, 이름 `NCC-BACKUP-…`(runExport 앞부분) | 백업 흐름은 `xen.go Backup`이 원본 vbr 순서로 한다. 항상 요청 이름으로 스냅샷 | NX-01, K-AG-11 |
| 수정 | 임시 스냅샷 정리 `VM.destroy` | `UninstallSnapshot`(스냅샷 VDI 삭제 → `VM.destroy`) | NX-02, C-AG-08 |
| 유지 | `.partial` 에 쓰고 이름 바꾸기 | 같다 | NX-03, C-AG-09 |
| 수정 | export 뒤 task 상태 확인 12초 | `waitTask`: ctx 가 끝날 때까지 | 큰 VM 에서 12초를 넘을 수 있다 |
| 추가 | - | ctx 가 끝나면 `task.cancel`(`watchCancel`) | 종료 때 XenServer 쪽 작업이 남지 않게 |
| 추가 | - | 파일 닫기 오류도 실패로 본다 | 쓰기 실패 미검출 방지 |
| 추가 | - | 파일 권한 0666(원본 vbr `chmod`) | 원본 vbr 동작 |
| 수정 | 복구 결과 VM 이 여럿이면 첫 번째를 쓰고 경고 | 정확히 1개가 아니면 실패 | 원본 vbr: `vm-import` 출력이 uuid 하나일 때만 성공 |
| 유지 | 템플릿이면 `SetIsATemplate(false)` + `UpdateAllowedOperations` | 같다(실패는 기록만) | ncc-xen 실기 확인 사항 |
| 확인 필요 | `sr_id`, `restore` 파라미터 "실기검증 전" | 그대로 쓴다 | PT-02 |

### CP-10 `secret`

| 구분 | 대상 | 바꾼 것 | 이유 |
|---|---|---|---|
| 수정 | `config.go` 구조체 태그 | `yaml:"…"` → `json:"…"` | YAML 라이브러리를 들여오지 않는다 |
| 수정 | `load.go` | `safeyaml` 검사와 `yaml.Unmarshal` → `encoding/json` + `DisallowUnknownFields` + 뒤에 남는 자료 거부. `MarshalKeyringConfig` 는 `json.MarshalIndent` | 같다. 모르는 필드 거부는 원본과 같은 목적 |
| 유지 | 봉투 형식 `ENC[v1:aes-256-gcm:키ID:…]`, 용도 문자열을 AAD 로 쓰는 것, 키링 파일 권한 검사(unix, 077 금지) | 같다 | Q-A14 "ncc-xen 과 같은 방식" |
| 차이 | 키링 파일 | ncc-xen 은 YAML, ncc-vbr 는 JSON | 두 프로젝트의 키링 파일은 서로 바꿔 쓸 수 없다. 봉투(ENC[…])는 같은 키면 서로 풀 수 있다 |

## 4. 원본 변경 추적

| 비교일 | 원본 커밋 범위 | 해당 변경 | 반영 |
|---|---|---|---|
| 2026-11-17 | 043c6d5 (복사 시점) | - | - |

전환 뒤에는 `git -C c:\dev\ncc-xen log 043c6d5..HEAD -- backend/pkg/xapi backend/internal/job/vm_backup_export.go backend/internal/job/vm_backup_restore.go stdcrypto/secret`로 바뀐 것을 확인하고 이 표에 적는다(운영 절차 `07` 문서 6장).
