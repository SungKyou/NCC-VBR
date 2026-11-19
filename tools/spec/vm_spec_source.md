---
product: NCC-VBR VM Backup Service
title: Job Type 표준 명세서
subtitle: 호출자(관리 서버 · Middleware) ↔ vbr_vm 인터페이스 정의
version: v1.00
date: 2026. 11. 19.
isodate: 2026-11-19
header: NCC-VBR vbr_vm  Job Type 표준 명세서  v1.00
footer: NamuTech  NCC-VBR
output: docs/vm/NCC-VBR_VM_JobType_표준명세서_v1.00.docx
---

| 구분 | 내용 |
|---|---|
| 버전 | v1.00 (최초) |
| 대상 서비스 | vbr_vm 4.000 — NCC-VBR VM Backup Service (실행 파일 vbr_vm, systemd ncc-vbr-vm.service) |
| 참조 규약 | NCC-Xen Backend Job Type 표준 명세서 v3.36 — 1장(규약 1·2, API 토큰)과 17장(VM_BACKUP). 다른 점은 10장 |
| 주요 내용 | XenServer VM 을 XVA 파일로 백업(export, 디스크 선택 포함)하고, XVA 를 검증(verify)하고, VM 으로 복구(restore)한다. 요청은 규약 1(JSON), 결과는 조회 API 와 규약 2(설정 시). 풀 접속 정보(마스터 주소·계정·ENC 암호)를 요청마다 받는다 |
| 기술 스택 | Go 1.26(toolchain go1.27.4) / Gin / viper(설정 즉시 반영) / stdlogger / klauspost zstd / XenServer XAPI XML-RPC·HTTP |
| 구현 상태 | VM_BACKUP 1개 jobType, action 3개(export / verify / restore) 개발 완료 + 조회·취소 API |
| 검증 상태 | 개발 검증 통과(단위 시험 Windows 77/4/0, Linux 76/1/0, 데모 자동 16/16, govulncheck 0). 실제 XenServer 9 풀 실기 I01~I03·I05~I10 통과(2026-11-19). 남은 실기 I04·L01 등은 13장 |
| 원문과 상세 | 이 명세서의 원문은 저장소 tools/spec/vm_spec_source.md 이다. 상세 근거는 docs/vm/05-interface-spec.md(인터페이스), 03(설계), 04(변경·결정 대장), 06(시험), 07(운영) |
| 구현상태 범례 | ✅ 개발 완료   🔧 개발중   📋 미개발(예정)   🆕 신규 |

# 1. 개요

## 1.1 통신 흐름

vbr_vm 은 호출자(관리 서버, 나중에 Middleware)와 HTTP 로 통신하고, XenServer 풀 마스터와는 XAPI(443)로 통신한다. NCC-Xen 의 규약 1·2 형식을 따르고, 결과를 당겨 가는 조회 API 를 더했다. XenServer 이벤트 전송(NCC-Xen 규약 3)은 없다.

| 규약 | 방향 | 엔드포인트 | 설명 |
|---|---|---|---|
| 규약 1 | 호출자 → vbr_vm | POST /api/v1/jobs | 작업 요청. 필수 값만 검사하고 즉시 202 ACCEPTED |
| 조회 | 호출자 → vbr_vm | GET /api/v1/jobs/{taskId}, GET /api/v1/jobs | 작업 상태·진행률·결과 조회(🆕 NCC-Xen 에 없음) |
| 취소 | 호출자 → vbr_vm | POST /api/v1/jobs/{taskId}/cancel, POST /api/v1/jobs/cancel-queued | 대기·처리 중 작업 취소(🆕) |
| 상태 | 호출자 → vbr_vm | GET /health | 서비스 상태. 인증 없음 |
| 규약 2 | vbr_vm → 호출자 | POST <result_report.url> | 작업이 끝나면 결과 보고. 설정에 주소가 있을 때만 |
| XAPI | vbr_vm → XenServer | XML-RPC, /export, /import | 스냅샷·내보내기·가져오기. 풀 마스터 443 |

## 1.2 공통 규칙

| 규칙 | 내용 |
|---|---|
| 비동기 처리 | 모든 작업은 비동기다. 접수를 기록한 뒤 202 ACCEPTED 를 돌려주고 실제 일은 작업자가 한다. 결과는 조회 API 로 확인하거나 규약 2 로 받는다 |
| taskId | 호출자가 만든다(UUID v4 권장, 1~64자, 영문·숫자·-·_). 같은 taskId 는 다시 실행하지 않는다(1.4) |
| Pool 식별 | payload.masterIp 필수. poolUUID 는 기록·로그용이며 검사하지 않는다(NCC-Xen 과 다름, 10장) |
| 접속 정보 | 풀 마스터 계정·암호를 요청마다 payload.credentials 로 받는다. 서비스 하나가 여러 풀을 다룬다. 작업마다 로그인하고 끝나면 로그아웃한다 |
| 검증 범위 | 필수 값과 형식만 검사한다(없으면 400). VM·SR·파일이 실제로 있는지는 미리 확인하지 않는다. 처리 중 XAPI·파일 오류가 나면 원문을 error.detail 에 싣고 failed 로 끝낸다 |
| 진행률 | 조회 API 의 percent(0~100)와 step. 규약 2 진행 보고는 result_report.send_progress=true 일 때만 |
| 동시 처리 | concurrency.max_jobs(기본 3)개를 동시에 처리하고 나머지는 접수 순서대로 대기한다. 우선순위 없음. 대기열이 차면 503 |
| 시각 | RFC 3339 UTC(예 2026-11-19T01:02:03Z) |
| 필드 이름 | 바깥 필드는 camelCase, options 안은 snake_case(NCC-Xen 과 같음). 모르는 필드는 무시한다 |
| 다국어 message | payload.message 는 locale(ko / en / ja, 기본 ko)에 따른다. error.detail(XAPI 원문)·오류 코드는 번역하지 않는다(9장) |
| HTTP 제한 | 본문 1 MiB, 읽기 15초 · 쓰기 30초 · 유휴 60초(NCC-Xen 과 같음) |

## 1.3 규약 1 — 요청 구조 (호출자 → vbr_vm)

엔드포인트: POST /api/v1/jobs

| 필드 | 필수 | 타입 | 예시 | 설명 |
|---|---|---|---|---|
| taskId | 필수 | string | UUID v4 | 호출자가 생성. 전체 작업 추적 키 |
| jobType | 필수 | string | VM_BACKUP | VM_BACKUP 만 받는다. 다르면 400 UNSUPPORTED_JOB |
| action | 필수 | string | export | export / verify / restore |
| executionType | 선택 | string | async | async 만. 다른 값이면 400 INVALID_FIELD |
| sentAt | 선택 | string | 2026-11-19T... | 호출자 송신 시각. 기록만 한다 |
| payload | 필수 | object | {} | 아래 |

payload 공통 필드:

| 필드 | export | verify | restore | 타입 | 설명 |
|---|---|---|---|---|---|
| targetType | VM | FILE | FILE | string | 다른 값이면 400 INVALID_FIELD |
| targetUUID | 필수 | 생략 | 생략 | string | 백업할 VM 또는 스냅샷 uuid. 형식(8-4-4-4-12)만 검사 |
| masterIp | 필수 | 생략 | 필수 | string | 풀 마스터 주소(IP 또는 이름, https:// 생략 가능). HOST_IS_SLAVE 가 오면 새 마스터를 따라간다 |
| poolUUID | 선택 | 생략 | 선택 | string | 기록·로그용. 검사하지 않는다 |
| credentials.username | 선택 | - | 선택 | string | XAPI 계정. 기본 root |
| credentials.password | 필수 | - | 필수 | string | XAPI 암호. ENC[v1:aes-256-gcm:…] (1.7). crypto.enabled=true 에서 평문이면 400 PLAINTEXT_PASSWORD_REJECTED |
| options | 필수 | 필수 | 필수 | object | action 별 파라미터(3~5장) |

## 1.4 규약 1 — 즉시 응답

| HTTP | 언제 | 본문 |
|---|---|---|
| 202 | 접수 | {"taskId":"…","status":"ACCEPTED","sentAt":"<수신 시각>","message":""} — NCC-Xen 과 같음 |
| 202 | 같은 taskId 가 이미 있음 | 위와 같고 message 가 "duplicate taskId — already accepted, not re-executed"(NCC-Xen 과 같은 고정 문장, locale 과 무관) |
| 400 | 요청 오류(7장) | {"error":"<문구>","code":"<코드>","field":"<JSON 경로>"} |
| 401 | 토큰 오류(1.7) | {"error":"unauthorized: missing or invalid X-Internal-Token","code":"UNAUTHORIZED"} |
| 500 | 작업 기록을 쓰지 못함 | code STORE_ERROR. 기록 없이 202 를 주지 않는다 |
| 503 | 대기열 가득 · 종료 중 | code QUEUE_FULL / SERVICE_STOPPING |

중복 taskId: 끝난 작업을 포함해 같은 taskId 면 상태와 무관하게 다시 실행하지 않는다. 거의 동시에 들어온 같은 taskId 도 한 번만 실행된다(중복 확인과 접수 기록을 같은 잠금 안에서 한다, NCC-Xen v3.36 과 같은 방식). 보관 기간(task_store.retention_days)이 지나 기록에서 지워진 taskId 는 새 작업으로 받는다.

## 1.5 규약 2 — 결과 보고 (vbr_vm → 호출자)

엔드포인트: POST <result_report.url> (호출자가 제공. 설정이 비어 있으면 보내지 않고 조회 API 로만 확인한다)

| 필드 | 타입 | 값 | 설명 |
|---|---|---|---|
| taskId | string | 6f1c2a4e-... | 요청의 taskId 그대로 |
| jobType | string | VM_BACKUP | 요청의 jobType |
| action | string | export | 요청의 action |
| result | string | success / failure / processing | processing 은 send_progress=true 일 때 진행 중 보고 |
| sentAt | string | 2026-11-19T... | vbr_vm 전송 시각 |
| payload.targetType | string | VM / FILE | 대상 종류 |
| payload.targetUUID | string | uuid | 대상 uuid(export) |
| payload.message | string | VM 백업 완료: … | locale 문구 |
| payload.options | object | { file_path: … } | 결과 필드(3~5장). 실패·취소·중단이면 error_code, error_detail 을 더한다 |

헤더: GATEWAY_INTERNAL_TOKEN 값이 있으면 X-Internal-Token 을 싣는다. 다시 보내기는 NCC-Xen outbox 와 같다: 5xx·429·408·연결 오류는 다시 보내고 result_report.max_age_hours(기본 24)가 지나면 Dead Letter, 400·401 등은 바로 Dead Letter. 결과 보고 실패는 작업 상태에 영향을 주지 않는다.

## 1.6 조회 · 취소 API (🆕 NCC-Xen 에 없음)

| 메서드 · 경로 | 하는 일 | 응답 |
|---|---|---|
| GET /api/v1/jobs/{taskId} | 작업 하나 조회 | 200 작업 상태(아래). 없으면 404 TASK_NOT_FOUND(보관 기간이 지나 지운 것 포함) |
| GET /api/v1/jobs | 작업 목록. 질의 status(쉼표로 여러 개), action, since, limit(1~1000, 기본 100) | 200 {"items":[…], "count":n}. 접수 최신순, 목록에서는 request 를 뺀다 |
| POST /api/v1/jobs/{taskId}/cancel | 작업 하나 취소 | 200 대기 중이라 바로 canceled. 202 처리 중이라 취소 시작(step canceling), 정리가 끝나면 canceled. 404 TASK_NOT_FOUND. 409 TASK_ALREADY_FINISHED |
| POST /api/v1/jobs/cancel-queued | 대기 중 작업 모두 취소. 처리 중은 건드리지 않는다 | 200 {"canceled":[…], "count":n} |
| GET /health | 서비스 상태. 인증 없음 | 200(아래 예). 종료 중이면 503 status=stopping |

GET /health 응답 예:

```json
{ "status": "ok", "service": "vbr_vm", "version": "4.000", "startedAt": "2026-11-19T08:22:34Z",
  "running": 0, "queued": 0, "authEnabled": true, "cryptoEnabled": true }
```

작업 상태(조회 응답):

| 필드 | 타입 | 설명 |
|---|---|---|
| taskId, jobType, action | string | 요청 그대로 |
| status | string | queued / running / succeeded / failed / canceled / interrupted(6장) |
| result | string | 규약 2 값으로 바꾼 것: queued·running → processing, succeeded → success, 나머지 → failure |
| percent | int | 0~100. queued 는 0, 끝나면 100(13장 진행률 주의) |
| step | string | queued, login, snapshot, exporting, cleanup, verifying, decompressing, uploading, importing, postprocess, canceling, done |
| acceptedAt, startedAt, finishedAt | string | 없으면 빠진다 |
| request | object | 받은 요청(암호는 ***) |
| payload | object | 규약 2 payload 와 같은 모양: targetType, targetUUID, message, options(결과 필드) |
| error | object | 실패·취소·중단일 때 {"code":"…","detail":"<원문>"} |

처리 중 조회 예:

```json
{
  "taskId": "6f1c2a4e-3b7d-4c55-9a1e-2d8f0b6c7e91", "jobType": "VM_BACKUP", "action": "export",
  "status": "running", "result": "processing", "percent": 42, "step": "exporting",
  "acceptedAt": "2026-11-19T01:00:00Z", "startedAt": "2026-11-19T01:00:01Z",
  "payload": { "targetType": "VM", "targetUUID": "42097417-…", "message": "VM 내보내는 중 (42%)",
               "options": { "snapshot_left": "55689cc6-…" } }
}
```

## 1.7 API 토큰 인증과 암호 — X-Internal-Token, ENC

| 항목 | 호출자 → vbr_vm (받는 쪽 검사) | vbr_vm → 호출자 (규약 2) |
|---|---|---|
| 토큰(sysconfig.yaml api_auth.*) | VBR_VM_API_TOKEN | GATEWAY_INTERNAL_TOKEN |
| 적용 범위 | /api/v1/* 전체. /health 는 인증 없음 | 결과 보고 |
| 켜고 끄기 | api_auth.enabled=true 일 때만 검사(즉시 반영). true 인데 토큰이 비면 기동 실패. false 면 검사하지 않고 기동·변경 때 경고 로그 | 값이 있으면 헤더를 싣고 없으면 생략 |
| 실패 | 401 {"error":"unauthorized: missing or invalid X-Internal-Token","code":"UNAUTHORIZED"}(문장은 NCC-Xen 과 같음). 비교는 상수 시간 | 1.5 다시 보내기 규칙 |

운영 규칙: ① 같은 이름의 환경 변수(VBR_VM_API_TOKEN, GATEWAY_INTERNAL_TOKEN)가 설정 값보다 우선한다. ② crypto.enabled=true 면 토큰과 요청 속 XAPI 암호는 ENC[v1:aes-256-gcm:…] 값이다. 키링은 key.yaml(NCC-Xen 과 같은 형식). ③ 암호화 도구는 vbr_secret 이다: 요청 암호는 vbr_secret encrypt <키링>(용도 ncc-vbr/xapi-password), 토큰은 vbr_secret encrypt <키링> sysconfig.api_auth.VBR_VM_API_TOKEN. 평문은 표준 입력으로 넘겨 명령 기록에 남기지 않는다. ④ 호출자는 vbr_vm 과 같은 key.yaml 로 암호화해 보낸다. ⑤ 복호화한 암호는 XAPI 로그인에만 쓰고 작업 기록에 남기지 않는다. 로그·작업 기록·조회 응답·결과 보고의 password, token 등은 항상 ***로 가린다.

설정 예(sysconfig.yaml):

```yaml
api_auth:
  enabled: true                          # 받는 쪽 검사(즉시 반영)
  VBR_VM_API_TOKEN: "ENC[v1:aes-256-gcm:k1:...]"
  GATEWAY_INTERNAL_TOKEN: ""             # 결과 보고에 싣는 토큰(없으면 헤더 생략)
crypto:
  enabled: true                          # false 면 평문 허용(시험용, 경고 로그)
  keyring_file: /opt/ncc-vbr/vm/key.yaml
```

# 2. jobType 목록 및 구현 상태

| # | jobType | action | 대상(targetType) | 구현상태 | 비고 |
|---|---|---|---|---|---|
| 1 | VM_BACKUP | export | VM | ✅ 개발 완료 | 전체 백업, 디스크 선택 백업(options.disks), 압축 none/gzip/zstd, 백업 뒤 검증 |
| 1 | VM_BACKUP | verify | FILE | ✅ 개발 완료 | XVA 구조·청크 체크섬 검증, 압축 형식 자동 판별 |
| 1 | VM_BACKUP | restore | FILE | ✅ 개발 완료 | MAC·uuid 보존 선택, zstd 자동 해제, 템플릿 후처리, 이름·Home Server 지정 |

범위 밖: dom0 상태 점검, 풀·VM 현황 조회, OVF/OVA, XenServer 이벤트 전송, 남은 임시 스냅샷 자동 정리.

# 3. VM_BACKUP export — VM 을 XVA 파일로 내보내기

## 3.1 options

| 필드 | 타입 | 필수 | 기본 | 설명 |
|---|---|---|---|---|
| file_name | string | 필수 | - | 저장 파일 이름. 확장자 .xva(zstd 면 .xva.zst) 자동. 구분자·특수문자(\ / : * ? " < > \|)와 .. 불가 → 400 INVALID_FIELD |
| export_path | string | 선택 | backup.base_path | 저장 디렉터리. 절대 경로는 그대로, 상대 경로는 base_path 아래 |
| compression | string | 선택 | none | none / gzip(XAPI 쪽 압축, XenCenter 호환) / zstd(vbr_vm 이 받으면서 압축, XenCenter 로 복구 불가). 다른 값은 400 |
| disks | int 배열 | 선택 | 전부 | 담을 디스크 번호(userdevice). 예 [0]. 빈 배열은 400. VM 에 없는 번호면 작업 failed(DISK_NOT_FOUND). 주면 항상 임시 스냅샷 |
| snapshot_mode | string | 선택 | auto | auto / always(3.2) |
| snapshot_name | string | 선택 | NCC-BACKUP-<VM 이름>-<YYYYMMDDhhmmss> | 임시 스냅샷 이름. MAC 보존 복구한 VM 의 이름이 된다 |
| keep_snapshot | bool | 선택 | false | 임시 스냅샷을 지우지 않고 남긴다(결과 snapshot_uuid) |
| overwrite | bool | 선택 | false | 같은 이름 파일이 있으면 덮어쓴다. false 면 작업 failed(FILE_EXISTS) |
| verify_after_export | bool | 선택 | backup.verify_after_export(false) | 끝난 뒤 verify 까지 한다. 결과 verify 필드 |

⚠ 기존 vbr_agent 와 같은 결과를 원하면 compression: "gzip", snapshot_mode: "always" 를 함께 보낸다. 기본값은 압축 없음이고 Halted VM 은 스냅샷 없이 내보낸다.

## 3.2 임시 스냅샷 정책

| 조건 | auto(기본) | always |
|---|---|---|
| disks 지정 | 임시 스냅샷(디스크를 지워야 해서) | 임시 스냅샷 |
| targetUUID 가 스냅샷 | 그대로 내보냄(disks 를 주면 INVALID_FIELD — 사용자 스냅샷의 디스크를 지우지 않으려고) | 그대로 내보냄 |
| Running | 임시 스냅샷 | 임시 스냅샷 |
| Halted · Suspended | 스냅샷 없이 그대로 내보냄(NCC-Xen 방식) | 임시 스냅샷(기존 vbr_agent 방식) |
| 그 밖의 전원 상태(Paused 등) | failed VM_STATE_UNSUPPORTED | 임시 스냅샷 |

동작 확인(실기 2026-11-19): auto 로 Halted VM 을 내보내는 동안 dom0 xe vm-start 는 거부되지 않고 내보내기가 끝날 때까지 기다린 뒤 시작된다. 큰 VM 이면 백업 시간만큼 시작이 늦어지므로, 백업 중에도 바로 켜야 하는 VM 은 always 로 보낸다. 같은 Halted VM 의 스냅샷 없는 내보내기 여러 개는 동시에 진행된다.

## 3.3 처리 순서

① 암호 복호화와 XAPI 로그인 ② 스냅샷 정책에 따라 임시 스냅샷 생성 — 만들자마자 작업 기록 options.snapshot_left 에 uuid 를 적는다 ③ disks 가 있으면 스냅샷에서 나머지 디스크의 VBD·VDI 삭제(원본 VM 디스크는 건드리지 않음) ④ XAPI task 를 만들고 /export 스트림을 <파일>.partial 에 쓴다(zstd 면 받으면서 압축, SHA256 계산) ⑤ 스트림이 끝나도 XAPI task 결과가 success 일 때만 성공. 아니면 EXPORT_INCOMPLETE ⑥ .partial 을 최종 이름으로 바꾼다 ⑦ 임시 스냅샷 삭제(스냅샷 VDI → 스냅샷 VM 레코드). 성공·실패·취소와 무관하게 한다. 디스크가 아직 사용 중(VDI_IN_USE, OTHER_OPERATION_IN_PROGRESS)이면 2초 간격으로 최대 2분 다시 지운다 ⑧ verify_after_export 면 검증.

## 3.4 결과 필드 (payload.options)

| 필드 | 설명 |
|---|---|
| file_path, file_size_bytes, checksum_sha256 | 최종 파일 경로·크기·디스크에 남은 내용의 SHA256 |
| format, compression | "xva", none / gzip / zstd |
| exported_uuid | 실제로 내보낸 uuid(임시 스냅샷이면 스냅샷 uuid) |
| power_state, snapshot_used | 대상 전원 상태, 임시 스냅샷 사용 여부 |
| disks | 담은 디스크 번호(disks 를 줬을 때) |
| snapshot_uuid | keep_snapshot=true 로 남긴 스냅샷 |
| snapshot_cleanup_failed, snapshot_cleanup_error | 임시 스냅샷을 지우지 못했을 때 uuid 와 오류. 작업 결과는 그대로 둔다(NCC-Xen 과 같음) |
| verify | verify_after_export 결과(4.2 필드) |
| duration_seconds, completedAt | 걸린 시간, 끝난 시각 |

요청 예 — 디스크 0 만, 기존과 같은 gzip·스냅샷:

```json
{
  "taskId": "6f1c2a4e-3b7d-4c55-9a1e-2d8f0b6c7e91",
  "jobType": "VM_BACKUP", "action": "export", "executionType": "async",
  "payload": {
    "targetType": "VM",
    "targetUUID": "42097417-b9ee-2f97-8eeb-3612efbcb25d",
    "masterIp": "192.168.102.53",
    "credentials": { "username": "root", "password": "ENC[v1:aes-256-gcm:k1:...]" },
    "options": { "file_name": "copy_20gb2-d0", "export_path": "/BACKUP/pool1",
                 "disks": [0], "compression": "gzip", "snapshot_mode": "always" }
  }
}
```

# 4. VM_BACKUP verify — XVA 파일 무결성 검증

## 4.1 options

| 필드 | 타입 | 필수 | 설명 |
|---|---|---|---|
| file_path | string | 필수 | 검증할 파일. 절대 경로 또는 base_path 아래 상대 경로 |

압축 형식(zstd · gzip · 비압축)은 파일 앞부분으로 스스로 판별한다. tar 구조와 청크별 SHA1(.checksum)을 확인한다. 파일을 열 수 없을 때만 failed 이고, 손상은 succeeded + verified=false 와 사유로 돌려준다(NCC-Xen 17.3 과 같은 판정).

## 4.2 결과 필드

| 필드 | 설명 |
|---|---|
| verified | true / false |
| chunk_count, checksum_entries | 확인한 청크 수, 체크섬 항목 수 |
| compression_detected | none / gzip / zstd |
| checksum_sha256, file_path, file_size_bytes | 파일 SHA256·경로·크기 |
| failure_reasons | 손상 사유(최대 10건, 영어 고정 문장) |
| completedAt | 끝난 시각 |

# 5. VM_BACKUP restore — XVA 파일을 VM 으로 복구

## 5.1 options

| 필드 | 타입 | 필수 | 기본 | 설명 |
|---|---|---|---|---|
| file_path | string | 필수 | - | 복구할 파일. gzip · zstd · 비압축 자동 판별 |
| sr_uuid | string | 필수 | - | 대상 SR |
| restore_original_uuid | bool | 선택 | false | true 면 MAC·uuid 보존(기존 preserve=true). 원본 VM 이 같은 풀에 있으면 XenServer 가 DUPLICATE_MAC_SEED 로 거부한다 |
| force | bool | 선택 | false | XAPI force 그대로 |
| name_label | string | 선택 | 파일 속 이름 | 가져온 뒤 VM 이름 변경. 실패해도 작업은 성공(name_label_set_error) |
| home_server_uuid | string | 선택 | - | 가져온 뒤 Home Server 지정. 실패해도 작업은 성공 |

## 5.2 처리 순서

① 로컬 파일 확인(없으면 FILE_IO_ERROR) ② XAPI 로그인 ③ SR 확인 — 없는 SR 이면 업로드 전에 바로 failed(XAPI_ERROR UUID_INVALID | SR) ④ zstd 면 임시 파일로 해제 ⑤ /import 업로드, XAPI task 결과 대기 ⑥ 결과 VM 이 템플릿이면 일반 VM 으로 바꾸고 VM.update_allowed_operations(XenCenter Start 가 비활성이 되지 않게) ⑦ name_label, home_server 적용 ⑧ 임시 해제 파일 삭제. 복구한 VM 은 시작하지 않는다.

## 5.3 결과 필드

| 필드 | 설명 |
|---|---|
| vm_uuid | 새 VM uuid |
| sr_uuid, file_path, file_size_bytes, restore_original_uuid | 요청·파일 정보 |
| is_a_template_fixed, is_a_template_fix_error, allowed_operations_fix_error | 템플릿 후처리 결과(실패해도 succeeded 유지) |
| name_label, name_label_set_error | 이름 변경 결과 |
| home_server_uuid, home_server_applied, home_server_set_error | Home Server 지정 결과 |
| additional_vm_refs_ignored | 파일에 VM 이 여럿일 때 무시한 나머지 |
| duration_seconds, completedAt | 걸린 시간, 끝난 시각 |

# 6. 작업 상태 · 취소 · 재기동

## 6.1 상태 모델

| 상태 | 뜻 | 들어오는 조건 | 나가는 전이 |
|---|---|---|---|
| queued | 접수, 대기 | 접수(202 전에 기록) | running, canceled, interrupted |
| running | 처리 중 | 작업자가 꺼냄 | succeeded, failed, canceled, interrupted |
| succeeded | 성공 | 처리 끝 | - |
| failed | 실패 | 오류, 시간 초과(backup.timeout_hours) | - |
| canceled | 취소 | 취소 API | - |
| interrupted | 중단 | 종료 신호, 또는 재기동 때 끝나지 않은 기록 발견 | - |

## 6.2 취소

대기 중 작업은 바로 canceled(200). 처리 중 작업은 202 를 돌려주고 ① 진행 중인 HTTP 스트림을 끊고 XAPI task.cancel ② XAPI task 가 pending·cancelling 을 벗어날 때까지 최대 60초 기다림 ③ 취소와 무관한 새 context 로 정리(임시 스냅샷 삭제, .partial · 임시 해제 파일 삭제, 최대 2분) ④ canceled, error.code CANCELED. 끝난 작업의 취소는 409. 기존 vbr_agent 는 처리 중 취소를 거부했다.

## 6.3 종료와 재기동

| 경우 | 동작 | 남는 것 |
|---|---|---|
| 정상 종료(SIGTERM · SIGINT, systemctl stop) | 새 접수를 503 으로 막고 처리 중 작업을 취소 경로로 정리한 뒤 interrupted 로 기록(최대 30초) | 정리하지 못한 것은 결과 snapshot_left |
| 강제 종료 · 장애 | 정리하지 못한다 | 임시 스냅샷, <파일>.partial |
| 재기동 | 기록의 queued · running 을 interrupted 로 바꾼다. 자동으로 다시 실행하지 않는다. 결과 보고가 켜져 있으면 failure 로 알린다 | error.detail 에 중단 단계와 "temporary snapshot may remain: <uuid>", 결과에 snapshot_left |

재기동 뒤 처리: GET /api/v1/jobs?status=interrupted 로 찾아 다시 보낼지 호출자가 정한다. snapshot_left 의 스냅샷(이름 NCC-BACKUP-…)은 dom0 xe snapshot-uninstall 로, .partial 은 파일을 지워 정리한다(실기 I09 에서 확인).

# 7. 오류 처리

요청 단계(HTTP 응답):

| 코드 | HTTP | 뜻 | 다시 보내도 되는가 |
|---|---|---|---|
| INVALID_REQUEST | 400 | JSON 이 아니거나 너무 큼 | 고친 뒤 |
| MISSING_FIELD | 400 | 필수 필드 없음(field 에 경로) | 고친 뒤 |
| INVALID_FIELD | 400 | 형식 오류(uuid 형식, 파일 이름 문자, 빈 disks 등) | 고친 뒤 |
| UNSUPPORTED_JOB | 400 | 모르는 jobType · action · targetType 조합 | 아니오 |
| PLAINTEXT_PASSWORD_REJECTED | 400 | crypto.enabled=true 인데 평문 암호 | 암호화 뒤 |
| UNAUTHORIZED | 401 | 토큰 | 토큰 고친 뒤 |
| TASK_NOT_FOUND | 404 | 조회 · 취소 대상 없음 | 아니오 |
| TASK_ALREADY_FINISHED | 409 | 끝난 작업 취소 | 아니오 |
| QUEUE_FULL | 503 | 대기열 가득 | 나중에 |
| SERVICE_STOPPING | 503 | 종료 중 | 재기동 뒤 |
| STORE_ERROR | 500 | 작업 기록을 쓰지 못해 접수하지 않음 | 저장소 확인 뒤 |

작업 단계(작업 상태의 error.code):

| 코드 | 상태 | 뜻 | detail |
|---|---|---|---|
| CREDENTIAL_DECRYPT_FAILED | failed | 키링으로 암호를 풀 수 없음 | 원인(암호 값은 싣지 않음) |
| XAPI_CONNECT_FAILED | failed | 마스터에 접속 못 함 | 연결 오류 원문 |
| XAPI_AUTH_FAILED | failed | SESSION_AUTHENTICATION_FAILED | XAPI 원문 |
| XAPI_ERROR | failed | 그 밖의 XAPI 오류(UUID_INVALID, DUPLICATE_MAC_SEED 등) | XAPI 원문(예 UUID_INVALID \| VM \| 0000…) |
| DISK_NOT_FOUND | failed | disks 의 번호가 VM 에 없음 | 요청 번호와 VM 의 번호 |
| INVALID_FIELD | failed | 스냅샷 uuid 대상에 disks 를 줌 | 사유 |
| VM_STATE_UNSUPPORTED | failed | auto 에서 Running · Halted · Suspended 가 아닌 전원 상태 | 전원 상태 |
| PATH_NOT_ALLOWED | failed | sandbox_mode 에서 허용 밖 경로 | 경로 |
| FILE_EXISTS | failed | overwrite=false 인데 파일 있음 | 경로 |
| FILE_IO_ERROR | failed | 파일 쓰기 · 읽기 실패, 복구 파일 없음 | 원문 |
| EXPORT_INCOMPLETE | failed | 스트림은 끝났는데 XAPI task 가 성공이 아님 | task 상태 · 오류 |
| TIMEOUT | failed | backup.timeout_hours 넘음 | 경과 시간 |
| CANCELED | canceled | 취소 요청 | 취소 경위 |
| INTERRUPTED | interrupted | 종료 신호 또는 재기동 | 중단 단계, 남은 스냅샷 uuid |

# 8. 설정 (sysconfig.yaml)

반영 시점: 즉시 = 파일을 저장하면 재기동 없이 반영(viper + fsnotify). 새 작업부터 = 즉시 반영하되 이미 시작한 작업은 시작 때 값으로 끝낸다. 재기동 = 다시 띄워야 반영(바꾸면 경고 로그). 바뀐 파일을 읽지 못하면 이전 설정을 그대로 쓰고 오류 로그를 남긴다.

| 경로 | 기본 | 뜻 | 반영 |
|---|---|---|---|
| server.port / server.listen | 8090 / 0.0.0.0 | HTTP 수신 | 재기동 |
| api_auth.enabled | true | 토큰 검사 | 즉시 |
| api_auth.VBR_VM_API_TOKEN | - | 받는 토큰(ENC). 환경 변수 우선 | 즉시 |
| api_auth.GATEWAY_INTERNAL_TOKEN | - | 결과 보고 토큰. 환경 변수 우선 | 즉시 |
| crypto.enabled / crypto.keyring_file | true / ./key.yaml | 암호화, 키링 | 즉시 |
| locale | ko | ko / en / ja | 즉시 |
| logging.* | stdlogger 기본 | level 을 적으면 format, output, file_name, max_size_mb, max_backups, max_age_days, compress, local_time, time_format, time_zone, log_pattern, service_name 도 모두 적어야 기동한다 | 즉시 |
| backup.base_path | ./backups | 상대 경로 기준 | 새 작업부터 |
| backup.sandbox_mode / allowed_absolute_paths | false / [] | 허용 밖 경로 거부 | 새 작업부터 |
| backup.timeout_hours | 4 | 작업 시간 제한 | 새 작업부터 |
| backup.verify_after_export | false | options 생략 때 기본 | 새 작업부터 |
| concurrency.max_jobs / max_queue | 3(1~10) / 100 | 동시 처리 수, 대기열 크기 | 즉시 |
| xapi.verify_ssl / progress_interval_sec | false / 5 | 인증서 검증, 진행률 갱신 간격 | 새 작업부터 |
| task_store.dir / retention_days | ./data/tasks / 30 | 작업 기록 위치, 보관 일수 | 재기동 / 즉시 |
| result_report.url / send_progress | "" / false | 결과 보고 주소(비면 안 보냄), 진행 중 보고 | 즉시 |
| result_report.max_age_hours / deadletter_retention_days | 24 / 30 | 다시 보내기 한도, Dead Letter 보관 | 즉시 |

설정 예(운영 Linux):

```yaml
server:
  port: 8090
api_auth:
  enabled: true
  VBR_VM_API_TOKEN: "ENC[v1:aes-256-gcm:k1:...]"
  GATEWAY_INTERNAL_TOKEN: ""
crypto:
  enabled: true
  keyring_file: /opt/ncc-vbr/vm/key.yaml
locale: ko
logging:
  level: info
  format: custom
  output: both
  file_name: /var/log/ncc-vbr/vbr_vm.log
  max_size_mb: 50
  max_backups: 5
  max_age_days: 28
  compress: true
  local_time: true
  time_format: "2006-01-02 15:04:05.000"
  time_zone: "Asia/Seoul"
  log_pattern: "[{time}][{level}] {msg} {attrs}"
  service_name: vbr_vm
backup:
  base_path: /BACKUP
  sandbox_mode: false
  allowed_absolute_paths: []
  timeout_hours: 4
  verify_after_export: false
concurrency:
  max_jobs: 3
  max_queue: 100
xapi:
  verify_ssl: false
  progress_interval_sec: 5
task_store:
  dir: /var/lib/ncc-vbr/vm/tasks
  retention_days: 30
result_report:
  url: ""
```

작업 기록 파일(task_store.dir): jobs.jsonl(상태가 바뀔 때마다 한 줄, 기동 때와 1시간마다 taskId 마다 마지막 줄만 남기고 보관 기간이 지난 것은 뺀다), outbox_pending.jsonl · outbox_delivered.jsonl · outbox_deadletter.jsonl(결과 보고, NCC-Xen 형식).

# 9. 다국어(i18n)

| 항목 | 내용 |
|---|---|
| 설정 | locale: ko(기본) / en / ja. 저장 즉시 반영. 모르는 값은 ko |
| 번역 대상 | payload.message, 로그 문구 |
| 번역하지 않는 것 | error.detail(XAPI · 파일 오류 원문), 오류 코드, 중복 taskId 문장, verify failure_reasons |
| 일본어 | 네이티브 검수 전(NCC-Xen 과 같은 상태) |

# 10. NCC-Xen 규약(v3.36)과 다른 점

| 항목 | NCC-Xen | vbr_vm | 이유 |
|---|---|---|---|
| 풀 접속 정보 | Redis 풀 정보와 상시 세션. payload 에 계정 없음 | payload.credentials 필수, 작업마다 로그인 | 요청마다 풀을 받는다(Redis 없음) |
| 풀 식별 | masterIp 또는 poolUUID(poolUUID 우선) | masterIp 필수, poolUUID 는 기록만 | poolUUID 로 마스터를 찾을 수 없다 |
| 결과 받기 | 규약 2 밀어 보내기만 | 조회 API + 규약 2(설정 시) | 호출자가 당겨 갈 수 있게 |
| 취소 | 없음 | 1.6 취소 API | 기존 기능 |
| 규약 3 이벤트 | 있음 | 없음 | 범위 밖 |
| export 디스크 선택 | 없음 | options.disks | 기존 부분 백업 |
| export 스냅샷 | Running 만 | 기본 같음(auto). snapshot_mode: always, snapshot_name, keep_snapshot 추가 | 기존 동작을 고를 수 있게 |
| export 압축 기본 | none | 같음(none) | - |
| 백업 뒤 검증 | 없음(따로 verify) | verify_after_export | - |
| 임시 스냅샷 정리 | VM.destroy 만 | 스냅샷 VDI → VM 레코드, 사용 중이면 다시 시도 | 스냅샷 VDI 가 남지 않게 |
| 400 본문 | {"error": …} | error + code + field | 호출자가 코드로 분기 |
| 실패 결과 보고 | message 에 오류 | options.error_code, error_detail 추가 | 코드로 분기 |
| 토큰 이름 | NCC_XEN_API_TOKEN | VBR_VM_API_TOKEN | 서비스마다 토큰 |
| OVF/OVA, boot_mode | 있음 | 없음 | 범위 밖 |

# 11. 기존 vbr_agent 전문과의 대응

| 기존(vbr_agent 3.000) | vbr_vm | 달라진 점 |
|---|---|---|
| B…BACKUP <vm> <스냅샷 이름> <전체 경로> | export, targetUUID, options.snapshot_name, export_path + file_name, compression: "gzip", snapshot_mode: "always" | 경로를 디렉터리와 이름으로 나눈다. 우선순위 없음. 기존과 같은 결과는 두 옵션을 함께 |
| PARTIAL <vm> <15자리> <이름> <경로> | export + options.disks | 15자리 자리값 → 번호 목록(자리 i 가 1 이면 i) |
| RESTORE <경로> <true\|false> <sr> | restore, file_path, restore_original_uuid, sr_uuid | 이름 변경, Home Server 지정 추가 |
| Q<번호> 조회 → 결과 코드 0 · 2 · 3 · 6 · C · 9 | GET /api/v1/jobs/{taskId} 의 status | 0 → succeeded / failed(실패도 0 이던 것을 나눔), 2 → queued, 3 → running, 6 → interrupted, C → canceled, 9 → 404 |
| C<번호> 취소, 일괄 취소 | 1.6 취소 API | 처리 중도 취소된다 |
| 결과 문장 VM BACKUP Success : … | payload.options 필드 | 문장 대신 필드 |
| 결과 [ERROR]XAPI Error: … | status failed, error.code, error.detail | 원문은 detail 에 그대로 |
| node_info.cfg | sysconfig.yaml(즉시 반영) | 8장 |
| vbr_secret encrypt | 같은 도구 · 같은 키링 형식 | 토큰용 용도 인자 추가 |

# 12. 버전 이력

| 버전 | 날짜 | 내용 |
|---|---|---|
| v1.00 | 2026. 11. 19. | 최초 작성. vbr_vm 4.000 기준. VM_BACKUP export / verify / restore, 조회 · 취소 API, 토큰 · ENC 암호, 설정 즉시 반영, 다국어. 실기에서 찾아 고친 결함 반영: Windows 시간대 자료(D-VM-01), 처리 중 취소 뒤 임시 스냅샷 정리 — task 종료 대기와 삭제 재시도(D-VM-02), 중단 결과의 남은 스냅샷 uuid snapshot_left(D-VM-03) |

# 13. 검증 상태와 알려진 제약

## 13.1 실기 결과 (2026-11-19, Windows PC → XenServer 9 풀)

| 시험 | 내용 | 결과 |
|---|---|---|
| I01 전체 백업 | none · auto(Halted, 스냅샷 없음) | ✅ 5,983,571,968바이트, 108초 |
| I02 디스크 선택 | disks [1], 임시 스냅샷 | ✅ 스냅샷 생성 · 삭제 |
| I03 복구 | restore_original_uuid=false, name_label | ✅ 새 VM, 템플릿 아님, 122초 |
| I05 검증 | none 파일 | ✅ verified=true, 청크 5,698 |
| I06 동시 3개 | none · auto, 같은 Halted VM | ✅ 3개 모두 성공, 240~252초 |
| I07 처리 중 취소 | always, 15초 뒤 취소 | ✅ canceled, 스냅샷 · .partial 없음(D-VM-02 수정 뒤) |
| I08 오류 | 없는 VM · SR · 파일, 틀린 암호, 필수 값 없음 | ✅ 오류 코드 · 원문 확인 |
| I09 강제 종료 | 처리 중 강제 종료 → 재기동 | ✅ interrupted, 자동 재개 없음, 남은 스냅샷 · .partial 확인 |
| I10 내보내기 중 VM 시작 | auto · Halted, dom0 xe vm-start | ✅ 시작이 내보내기 끝까지 기다림(3.2) |

남은 시험: I04(MAC 보존 복구 — DUPLICATE_MAC_SEED 확인), I05 gzip · zstd 파일, I06 기존과 같은 조건(gzip · always), L01 운영 크기 VM 백업 시간, Linux 서버에서 실행 파일 기동, 결과 보고 send_progress.

## 13.2 알려진 제약

| 항목 | 내용 |
|---|---|
| 진행률(📋 결정 대기 Q-VM-03) | percent 는 XAPI export task 의 progress 를 쓴다(NCC-Xen 과 같은 계산). XAPI 가 실제 전송량보다 낮게 보고해 none 백업이 25% 근처까지만 오르다 끝에서 99% 로 건너뛴다. 끝났는지는 status 로 판단한다. 받은 바이트 기준 계산으로 바꾸는 안을 검토 중이다 |
| zstd 백업 | .xva.zst 는 vbr_vm restore 로만 복구된다(XenServer · XenCenter 는 zstd 를 모른다) |
| 남은 임시 스냅샷 | 강제 종료 등으로 남은 스냅샷은 자동으로 지우지 않는다(snapshot_left 로 알린다) |
| 통신 | HTTP(암호는 요청 안에서도 ENC 값). HTTPS 수신은 Middleware 연동 때 검토 |
| Windows 표시 | Windows 에서 쓰는 중인 .partial 은 탐색기에 크기 0 으로 보인다(닫을 때 갱신). 진행은 조회 API 로 본다 |

# 14. 향후 계획

| 항목 | 상태 | 비고 |
|---|---|---|
| 진행률 계산 개선 | 📋 결정 대기 | Q-VM-03 |
| Middleware 연동(규약 2 실제 수신, HTTPS) | 📋 미개발 | Middleware 확정 뒤 |
| 남은 임시 스냅샷 자동 정리 | 📋 범위 밖 | 운영에서 필요할 때 |
| OVF/OVA | 📋 범위 밖 | 요청이 있을 때 |
| XAPI 세션 재사용 | 📋 검토 | 동시 작업을 크게 늘릴 때 |
