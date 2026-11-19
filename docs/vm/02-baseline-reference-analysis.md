# vbr_vm 기존·참조 분석서

기준일: 2026-11-19 / 양식: T02(유형 B — 1~12장은 요약, 13장 참조 분석) / 작성: Claude Code

| 기준 | 위치 | 판 |
|---|---|---|
| 기존 서비스 | `go/internal/agent`, `go/cmd/vbr_agent` (1단계 전환본) | 3.000, 실기 2026-11-18~19 |
| 기존 분석 | [agent/00](../agent/00-rescope-review.md), [agent/05](../agent/05-interface-spec.md), [agent/04](../agent/04-change-register.md), [agent/06](../agent/06-test-report.md) 5.3 | - |
| 참조 프로젝트 | `c:\dev\ncc-xen` (읽기만 함) | 커밋 `043c6d5` (2026-11-12) |
| 참조 명세서 | `ncc-xen/docs/NCC-XEN_JobType_표준명세서_v3.36.docx` 1장(규약 1·2, 토큰), 17장(VM_BACKUP), 18장(다국어) | v3.36 (2026-11-12) |

## 1~12. 기존 서비스(vbr_agent 3.000) 요약

기존 서비스는 1단계 문서에 자세히 있다. 여기서는 2단계가 이어받을 기능과 버릴 형식만 정리한다.

### 1.1 기능

| 번호 | 기능 | 동작 | 근거 | 2단계 |
|---|---|---|---|---|
| F-VM-01 | 전체 백업 | VM 스냅샷(요청 이름) → 스냅샷 `/export`(gzip, `use_compression=true`) → `.partial`에 받고 이름 바꿈 → 스냅샷과 스냅샷 VDI 삭제. VM 전원 상태와 무관하게 항상 스냅샷 | `agent/xen.go Backup`, K-AG-11, 실기 R2 | 유지. 스냅샷은 기본 `auto`(Q-VM-01), 압축은 요청(Q-VM-02) |
| F-VM-02 | 부분 백업 | 스냅샷 뒤 빼기로 한 디스크(15자리 자리값 `0`)의 VBD·VDI를 지우고 내보냄 | `agent/xen.go partialRemove`, 실기 R5 | 유지. 자리값 문자열 → 디스크 번호 목록 |
| F-VM-03 | 복구 | `/import`(`restore=<preserve>`, `sr_id`), 결과 VM이 템플릿이면 플래그를 끔 | `agent/transfer.go importFile`, 실기 R3·PT-02 | 유지 + ncc-xen 후처리(allowed_operations 재계산) |
| F-VM-04 | 결과 조회 | transaction 로그 마지막 기록의 단계 기호가 결과 코드(0·2·3·6·C·9) | `agent/tranlog.go`, K-AG-01 | **바꿈**: 작업 상태 기록과 조회 API(DEC-VM-06) |
| F-VM-05 | 취소 | 대기 중만 취소. 처리 중은 거부 | `agent.go cancel`, 실기 R6 | **바꿈**: 처리 중도 취소(DEC-VM-09) |
| F-VM-06 | 일괄 취소 | 대기 중 전부 | K-AG-04·05 | 유지(목록 취소 API) |
| F-VM-07 | 대기열·작업자 | 대기열 100칸, 작업자 2~10(기본 3), 우선순위 0~9 | `registry.go`, K-AG-08 | 작업자 유지, 우선순위 삭제(DEC-VM-11) |
| F-VM-08 | 재기동 복구 | 처리 중이던 작업은 6(중단), 대기 중이던 작업은 이어서 처리 | C-AG-20, 실기 R7 | **바꿈**: 처리 중·대기 중 모두 `interrupted`, 자동 재개 없음(DEC-VM-22, ncc-xen과 같음) |
| F-VM-09 | 접속 허용 주소 | `Allow Client` | C-AG-18 | 토큰 인증으로 대체(DEC-VM-08). 주소 제한은 두지 않는다(DEC-VM-23, ncc-xen과 같음) |
| F-VM-10 | 경로 제한 | `Backup Root` 밖 경로 거부 | C-AG-14 | ncc-xen `sandbox_mode`(기본 꺼짐)로 대체 |

### 1.2 버리는 형식

| 기존 | 이유 |
|---|---|
| 7자리 길이 + `B`+번호 17자리+종류+시간+우선순위 문자열 전문 | JSON API로 바꿈(지시 1) |
| `node_info.cfg` `[VBR Agent for Xenserver]` | `sysconfig.yaml`(DEC-VM-07·17) |
| `transaction-YYYYMM.log`로 결과 조회 | 작업 상태 기록(DEC-VM-06) |
| 결과 문장 `VM BACKUP Success : VM uuid=…` | 구조화한 결과 필드 |
| 조회 결과 코드가 실패에도 0(K-AG-01) | 상태 값 `failed`로 분명히 함 |

### 1.3 실기에서 확인된 사실 (2026-11-18~19, `agent/06` 5.3)

설계가 지켜야 하는 외부 시스템 동작이다.

| 번호 | 사실 | 설계 영향 |
|---|---|---|
| E-01 | 원본 VM이 있는 풀에 preserve=true 복구는 `DUPLICATE_MAC_SEED`로 거부, VM 남지 않음 | 오류 원문을 그대로 결과에 싣는다 |
| E-02 | preserve=true 복구 VM의 uuid는 백업 때 스냅샷 uuid | 결과 설명과 운영 안내에 적는다 |
| E-03 | 같은 VM을 동시에 두 작업이 스냅샷·내보내기해도 성공 | 같은 VM 동시 작업을 막지 않는다 |
| E-04 | 강제 종료 뒤 스냅샷과 `failure` task 기록이 남음 | 재기동 때 남은 자원을 작업 기록에 적어 알린다(정리는 하지 않음, Q-A17과 같음) |
| E-05 | 없는 SR이면 파일을 보내기 전에 1초 안에 실패 | 복구는 SR 확인을 먼저 한다(ncc-xen도 `SR.get_by_uuid` 먼저) |
| E-06 | NFS SR의 `physical-utilisation`은 sr-scan 전 갱신 안 됨 | 결과에 VDI 사용량을 넣지 않는다 |
| E-07 | 2.6 GB(gzip) 백업이 원격 약 4분 30초 | 진행률이 필요하다(XAPI task progress) |

## 13. 참조 프로젝트 분석 (ncc-xen)

### 13.1 따를 규약

| 규약 | ncc-xen 내용 | 참조 위치 | vbr_vm 적용 | 이유 |
|---|---|---|---|---|
| 요청(규약 1) | `POST /api/v1/jobs`, `taskId`(호출자가 UUID v4), `jobType`, `action`, `executionType`(async), `sentAt`, `payload{targetType, targetUUID, masterIp, poolUUID, options}` | 명세 1.3 | **그대로** + payload에 접속 정보(`credentials`) | 지시 4: 접속 정보를 요청으로 받는다. ncc-xen은 Redis 풀 정보에서 얻는다 |
| 접수 응답 | 202 `{taskId, status:"ACCEPTED", sentAt, message:""}`. 중복 taskId도 202 + message "duplicate taskId — already accepted, not re-executed" | 명세 1.4, `handler.go` | **그대로** | |
| 결과 보고(규약 2) | 작업이 끝나면 `POST <middleware>/api/v1/jobs/result`로 `{taskId, jobType, action, result: success/failure/processing, sentAt, payload{targetType, targetUUID, message, options}}`. 진행 중은 `processing` + `percent` | 명세 1.5, `task_durability.go` outbox | **설정으로 켬**(결과 주소가 있을 때만) | middle이 아직 없다(지시 9). 같은 형식을 조회 API 응답에도 쓴다 |
| 조회 | 없음. 결과는 규약 2로만 받는다 | - | **새로 만듦**: `GET /api/v1/jobs/{taskId}`, 목록, 취소 | 지시 5 |
| 이벤트(규약 3) | XenServer 상태 변화 자동 전송 | 명세 1.6 | **안 씀** | 상태 감시는 범위 밖(지시 10) |
| 토큰 | `X-Internal-Token`. 받는 토큰 `NCC_XEN_API_TOKEN`, 보내는 토큰 `GATEWAY_INTERNAL_TOKEN`. `api_auth.enabled=false`면 경고만. 401 본문 `{"error":"unauthorized: missing or invalid X-Internal-Token"}`. 환경 변수가 설정보다 우선. `/health`는 인증 없음 | 명세 1.7, `apiauth.go` | **그대로**, 키 이름만 `VBR_VM_API_TOKEN`, `GATEWAY_INTERNAL_TOKEN` | 같은 middle이 두 서비스를 부를 때 헤더가 같아야 한다 |
| 암호 | `ENC[v1:aes-256-gcm:키ID:…]`, `crypto.enabled`, `crypto.keyring_file`(key.yaml) | `stdcrypto`, `sysconfig.yaml` | **그대로**. 1단계 `go/internal/secret`(CP-10)을 같이 쓴다 | 지시 7 |
| 설정 | `sysconfig.yaml`, viper + fsnotify 500ms 디바운스로 `locale`·`logging` 즉시 반영 | `i18n/watch.go`, `stdlogger/logger/watch.go` | **그대로**, 반영 대상을 `backup`, `concurrency` 일부까지 넓힘 | DEC-VM-17 |
| 로그 | stdlogger(slog 기반, text/json/custom, lumberjack 교체, 즉시 반영) | `stdlogger/logger` | **복사**(DEC-VM-15) | |
| 다국어 | `locale` ko/en/ja, 카탈로그 `catalog_<locale>.go`, 없는 값은 ko. XAPI 오류 원문은 번역하지 않음 | 명세 18장, `i18n` | **그대로**. 카탈로그는 vbr_vm 문구만 새로 씀 | DEC-VM-13 |
| 작업 기록 | `data/tasks/accepted.jsonl`(접수), `outbox_pending/delivered/deadletter.jsonl`(결과 전달). JSON Lines append-only, 압축은 임시 파일 + rename. 재기동 때 미완료는 `interrupted`로 알리고 자동 재개하지 않음 | `task_durability.go` 설계 주석 | **설계를 따르고 상태 기록을 더함**(03 설계 10장) | 조회 API는 상태·진행률이 필요한데 accepted.jsonl은 접수 여부만 담는다 |
| VM_BACKUP export | `options{file_name, export_path, compression: none/gzip/zstd, keep_snapshot, overwrite}`. Halted·Suspended·스냅샷 uuid는 그대로, Running만 임시 스냅샷(`NCC-BACKUP-<VM명>-<시각>`). `.partial` 뒤 이름 바꿈, task 최종 상태로 절단 감지, 스트리밍 중 SHA256 | 명세 17.2, `vm_backup_export.go` | **따름** + 디스크 선택(`disks`) + 스냅샷 정책(Q-VM-01) | |
| VM_BACKUP verify | `targetType=FILE`, `options{file_path}`. gzip·zstd 자동 감지, tar 구조, 청크 SHA1과 `.checksum` 대조, 전체 SHA256. 파일 열기 실패만 failure, 불일치는 success + `verified=false` | 명세 17.3 | **그대로**(지시 11) | |
| VM_BACKUP restore | `targetType=FILE`, `options{file_path, sr_uuid, name_label, restore_original_uuid, force, home_server_uuid}`. zstd는 임시 파일로 풀고 올림. 템플릿 플래그 끄기 + `VM.update_allowed_operations` | 명세 17.4·17.8 | **따름**. `restore_original_uuid` = 기존 preserve | |
| 경로 규칙 | 절대 경로는 그대로, 상대 경로는 `backup.base_path` 아래. `..`와 파일 이름의 구분자·특수문자 거부. `.xva` 자동. `sandbox_mode`면 base_path·허용 목록 밖 거부 | 명세 17.1, `vm_backup_util.go` | **그대로** | |
| 시간 제한 | `backup.timeout_hours` 기본 4 | 명세 17.1 | **그대로** | |
| OVF/OVA | export·restore 지원 | 명세 17.6·17.7 | **안 씀** | 기존 기능에 없음. 범위 밖(DEC-VM-01) |

### 13.2 복사 후보

| 후보 | 참조 경로(줄 수) | 하는 일 | 같이 따라오는 의존 | 판단 | T11 |
|---|---|---|---|---|---|
| 처리기 | `backend/internal/job/handler.go`(190) | 규약 1 바인딩, 202, 중복 taskId | gin, i18n, stdlogger, job 내부 | 일부 복사: 요청 구조체·바인딩·응답. 작업 분배는 새로 씀 | CP-VM-01 |
| 인증 | `backend/internal/apiauth/apiauth.go`(50) | 토큰 미들웨어 | gin | 복사 | CP-VM-02 |
| 가림 | `backend/internal/job/redact.go`(88) | 로그·기록에서 비밀 값 가림 | 없음 | 복사, 가릴 키에 `credentials.password` 추가 | CP-VM-03 |
| 작업 기록 | `backend/internal/job/task_durability.go`(908) | 접수 기록, 결과 outbox, Dead Letter, 재기동 처리 | gateway, i18n, stdlogger | 일부 복사: JSON Lines 쓰기·압축·retention·outbox 재시도. 상태 기록은 새로 씀 | CP-VM-04 |
| 내보내기 | `vm_backup_export.go`(432) | `/export` 스트리밍, zstd 동시 압축, SHA256, 진행률 | klauspost/zstd, xapi mixins, i18n | 복사(1단계 CP-06은 gzip만 옮긴 판이라 다시 복사) | CP-VM-05 |
| 가져오기 | `vm_backup_restore.go`(435) | `/import`, zstd 임시 해제, 후처리 | xapi mixins, i18n | 복사(1단계 CP-07 갱신판) | CP-VM-06 |
| 검증 | `vm_backup_verify.go`(248) | XVA 검증 | klauspost/zstd, i18n | 복사(1단계 CP-08 보류분) | CP-VM-07 |
| 압축 | `vm_backup_compress.go`(177) | zstd 해제(restore용). 2-pass 압축 함수는 미사용 | klauspost/zstd | 해제 함수만 복사 | CP-VM-08 |
| 경로 | `vm_backup_util.go`(122) | 경로 규칙 | config | 복사, 설정 접근만 바꿈 | CP-VM-09 |
| 다국어 | `backend/internal/i18n/i18n.go`, `watch.go` | 카탈로그 조회, locale 즉시 반영 | viper, fsnotify, stdlogger | 복사. 카탈로그(1,705줄)는 복사하지 않고 vbr_vm 문구만 새로 씀 | CP-VM-10 |
| 로그 | `stdlogger/logger/*.go` 9개(1,025) | 구조화 로그, 즉시 반영 | viper, fsnotify, lumberjack | 복사 | CP-VM-11 |
| XAPI | `backend/pkg/xapi` | 1단계 CP-01~04로 이미 `go/internal/xapi`에 있음 | - | 부족한 함수만 추가 복사(`VM.update_allowed_operations`, `SR.get_by_uuid`, `task.cancel`, VBD/VDI 조회 등) | CP-VM-12 |
| 암호 | `stdcrypto/secret` | 1단계 CP-10으로 `go/internal/secret`에 있음 | - | 그대로 씀 | (CP-10) |
| 데모 | `backend/demo.ps1`(179), `demo_modules/demo_common.ps1`(222), `demo_vm_backup.ps1`(224) | 메뉴, 입력, 요청 보내기, 결과 받기 | 결과 수신용 `mock_receiver` | 구조 복사. 결과는 조회 API로 받으므로 수신기 대신 조회 반복 | CP-VM-13 |
| 설정 | `backend/internal/config/config.go`(729) | yaml.v3 구조체, 기본값 | ncc-xen 전용 섹션 다수 | 복사하지 않음. vbr_vm 설정 구조체를 새로 쓰고 기본값 처리 방식만 따름 | - |

### 13.3 ncc-xen에만 있는 전제

| 전제 | ncc-xen | vbr_vm | 복사 때 바꿀 것 |
|---|---|---|---|
| 풀 정보 | Redis에 풀 정보와 상시 XAPI 세션(PoolSession) | 요청마다 마스터 IP·계정·암호를 받음 | 작업마다 로그인·로그아웃. `poolUUID`는 기록용(검사 안 함) |
| 풀 식별 | `masterIp` 또는 `poolUUID` 하나(둘 다면 poolUUID 우선) | Redis가 없어 poolUUID로 마스터를 찾을 수 없다 | `masterIp` 필수 |
| 결과 수신자 | Middleware가 항상 있음 | 없을 수 있다 | outbox는 결과 주소가 있을 때만 |
| 처리 중 정보 | 규약 2 `processing`으로 밀어 보냄 | 조회로 당겨 감 | 진행률을 작업 상태 기록에 남긴다 |
| 취소 | 규약에 취소 요청이 없다 | 취소 API가 있다(F-VM-05) | `task.cancel`, 정리 경로 새로 작성 |
| 디스크 선택 | export에 없다 | 기존 기능(F-VM-02) | 1단계 `partialRemove`를 옮겨 붙인다 |
| 스냅샷 정책 | Running만 임시 스냅샷 | 기존은 항상 스냅샷 | ncc-xen과 같게 기본 `auto`, 요청으로 `always`(Q-VM-01 결정) |
| 압축 기본값 | `none` | 기존은 항상 gzip | ncc-xen과 같게 요청에 없으면 `none`(Q-VM-02 결정) |
| 파일 이름 | `file_name` + `export_path` | 기존은 전체 경로 하나 | ncc-xen 방식. 기존 전체 경로는 `export_path`=디렉터리, `file_name`=이름으로 나눠 보낸다(05 11장) |
| 기존 파일 | `overwrite` 기본 false(있으면 실패) | 기존은 덮어씀 | ncc-xen 방식(DEC-VM-21) |
| Go 모듈 | 여러 모듈 + `replace` | 모듈 하나 | import 경로를 `ncc-vbr/internal/...`로 |

## 점검표

- [x] 참조 프로젝트의 기준 커밋·명세서 판을 적었다(043c6d5, v3.36)
- [x] 복사 후보마다 같이 따라오는 의존을 확인했다(13.2)
- [x] 기존 서비스의 기능이 모두 2단계 처리(유지/바꿈/삭제)로 분류됐다(1.1)
- [ ] 복사 직전 ncc-xen 커밋이 바뀌었는지 다시 확인한다(구현 때)
