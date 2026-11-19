# vbr_vm 인터페이스 명세

기준일: 2026-11-19 / 양식: T05(1·2·7·8~11장. 3~6장은 해당 없음) / 상태: **구현 반영(2026-11-19)**. 관문 A 뒤 구현하며 정한 것(DEC-VM-28~32)을 반영했다. Q-VM-01(스냅샷 기본 `auto`)·Q-VM-02(압축은 요청, 없으면 `none`)는 2026-11-19 사용자 결정을 반영했다.

참조 규약: ncc-xen JobType 표준명세서 v3.36 1장(규약 1·2, 토큰)과 17장(VM_BACKUP). 다른 점은 8.4에 모았다.

## 1. 목록

| 번호 | 종류 | 이름 | 방향 | 상대 |
|---|---|---|---|---|
| IF-VM-01 | 명령행 | `vbr_vm` | 입력 | 운영자, systemd |
| IF-VM-02 | 설정 파일 | `sysconfig.yaml` | 입력 | 운영자 |
| IF-VM-03 | 키링 | `key.yaml` | 입력 | `vbr_secret keygen` |
| IF-VM-04 | HTTP API | `/api/v1/jobs…`, `/health` | 수신 | 관리 서버, middle, demo.ps1 |
| IF-VM-05 | HTTP 결과 보고 | `POST <result_report.url>` | 송신 | middle(설정 시) |
| IF-VM-06 | XAPI | XML-RPC, `/export`, `/import` | 송신 | XenServer 풀 마스터 443 |
| IF-VM-07 | 파일 | XVA(`.xva`, `.xva.zst`), `.partial` | 출력·입력 | NFS 등 |
| IF-VM-08 | 작업 기록 | `task_store.dir` 아래 JSON Lines | 출력·입력 | 이 서비스(재기동 때) |
| IF-VM-09 | 로그 | `logging.file_name` | 출력 | 운영자 |

## 2. 명령행

```
vbr_vm [-config <sysconfig.yaml 경로>] [-version]
```

| 인자 | 뜻 | 필수 | 기본 |
|---|---|---|---|
| `-config` | 설정 파일 | 아니오 | 실행 파일과 같은 디렉터리의 `sysconfig.yaml`, 없으면 현재 디렉터리 |
| `-version`, `-ver` | `vbr_vm 4.000` 출력 후 종료 | 아니오 | - |

항상 앞에서 실행한다(백그라운드 분리 없음, DEC-VM-24). systemd 서비스 `ncc-vbr-vm.service`로 띄운다. 종료는 SIGINT·SIGTERM.

## 7. 종료 코드

| 코드 | 뜻 |
|---|---|
| 0 | 정상 종료(종료 신호) |
| 1 | 기동 실패(설정 오류, 포트 사용 중, 인증이 켜졌는데 토큰이 비었음, 키링을 못 읽음) |
| 255 | 인자 오류 |

## 8. HTTP API

### 8.1 공통

| 항목 | 내용 |
|---|---|
| 기준 주소·판 | `/api/v1`. 필드를 지우거나 뜻을 바꿀 때만 `/api/v2` |
| 인증 | `X-Internal-Token: <토큰>`. `api_auth.enabled=true`일 때 `/api/v1/*` 전부 검사, `/health`는 검사 안 함. 실패하면 401 `{"error":"unauthorized: missing or invalid X-Internal-Token"}`(ncc-xen과 같은 문장) |
| 본문 | `application/json; charset=utf-8`. 시각은 RFC 3339 UTC(`2026-11-19T01:02:03Z`). 바깥 필드는 camelCase, `options` 안은 snake_case(ncc-xen과 같음) |
| 모르는 필드 | 무시한다(ncc-xen과 같음) |
| 제한 시간 | 읽기 15초, 쓰기 30초, 유휴 60초(ncc-xen과 같음) |
| 본문 크기 | 1 MiB 넘으면 400 |
| 비밀 값 | `payload.credentials.password`는 `ENC[v1:aes-256-gcm:…]`(복호화 용도 `ncc-vbr/xapi-password`, 1단계 `vbr_secret encrypt`로 만든다, DEC-VM-27). `crypto.enabled=false`일 때만 평문도 받는다. 로그·작업 기록·조회 응답·결과 보고에서는 항상 `***`로 가린다 |

### 8.2 끝점 목록

| 번호 | 메서드·경로 | 하는 일 | 성공 응답 | 요건 |
|---|---|---|---|---|
| API-VM-01 | `POST /api/v1/jobs` | 작업 접수(export, verify, restore) | 202 | REQ-VM-01~04 |
| API-VM-02 | `GET /api/v1/jobs/{taskId}` | 작업 하나 조회 | 200 | REQ-VM-14 |
| API-VM-03 | `GET /api/v1/jobs` | 작업 목록 | 200 | REQ-VM-14 |
| API-VM-04 | `POST /api/v1/jobs/{taskId}/cancel` | 작업 하나 취소 | 202(처리 중) / 200(대기 중) | REQ-VM-15 |
| API-VM-05 | `POST /api/v1/jobs/cancel-queued` | 대기 중 작업 모두 취소 | 200 | REQ-VM-15 |
| API-VM-06 | `GET /health` | 서비스 상태 | 200 | REQ-VM-20 |

### 8.3 끝점별 명세

#### API-VM-01 `POST /api/v1/jobs`

바깥 필드(ncc-xen 규약 1과 같음)

| 필드 | 형식 | 필수 | 뜻 | 없거나 틀렸을 때 |
|---|---|---|---|---|
| `taskId` | string | 예 | 호출자가 만든 작업 번호(UUID v4 권장, 1~64자, 영문·숫자·`-`·`_`) | 400 `MISSING_FIELD` / `INVALID_FIELD` |
| `jobType` | string | 예 | `VM_BACKUP` | 400 `UNSUPPORTED_JOB` |
| `action` | string | 예 | `export` / `verify` / `restore` | 400 `UNSUPPORTED_JOB` |
| `executionType` | string | 아니오 | `async`만 | 다른 값이면 400 `INVALID_FIELD` |
| `sentAt` | string | 아니오 | 호출자 송신 시각. 기록만 한다 | 형식 검사 안 함 |
| `payload` | object | 예 | 아래 | 400 `MISSING_FIELD` |

`payload` 공통

| 필드 | 형식 | export | verify | restore | 뜻 |
|---|---|---|---|---|---|
| `targetType` | string | `VM` | `FILE` | `FILE` | 다른 값이면 400 `INVALID_FIELD` |
| `targetUUID` | string | 필수 | 생략 | 생략 | 백업할 VM(또는 스냅샷) uuid. 형식(8-4-4-4-12 16진수)만 본다 |
| `masterIp` | string | 필수 | 생략 | 필수 | 풀 마스터 주소(IP 또는 이름, `https://` 생략 가능). 마스터가 바뀌어 HOST_IS_SLAVE가 오면 새 마스터를 따라간다 |
| `poolUUID` | string | 선택 | 생략 | 선택 | 기록·로그용. 검사하지 않는다(8.4) |
| `credentials.username` | string | 선택(기본 `root`) | - | 선택 | XAPI 계정 |
| `credentials.password` | string | 필수 | - | 필수 | XAPI 암호. `ENC[...]`(8.1). 평문이 왔는데 `crypto.enabled=true`면 400 `PLAINTEXT_PASSWORD_REJECTED` |
| `options` | object | 필수 | 필수 | 필수 | 아래 |

`options` — export(ncc-xen 17.2 + 디스크 선택)

| 필드 | 형식 | 필수 | 기본 | 뜻 |
|---|---|---|---|---|
| `file_name` | string | 예 | - | 저장 파일 이름. `.xva`(zstd면 `.xva.zst`) 자동. 구분자·특수문자(`\ / : * ? " < > \|`)와 `..` 불가 → 400 `INVALID_FIELD` |
| `export_path` | string | 아니오 | `backup.base_path` | 저장 디렉터리. 절대 경로는 그대로, 상대 경로는 base_path 아래 |
| `compression` | string | 아니오 | `none` | `none` / `gzip`(XAPI 쪽 압축) / `zstd`(이 서비스가 받으면서 압축, XenCenter로 복구 불가). 다른 값은 400 `INVALID_FIELD`. 기존 vbr_agent와 같게 하려면 `gzip`을 보낸다(Q-VM-02) |
| `disks` | int 배열 | 아니오 | 전부 | 담을 디스크 번호(userdevice). 예 `[0]`. 빈 배열은 400. VM에 없는 번호면 작업 `failed`(`DISK_NOT_FOUND`) |
| `snapshot_mode` | string | 아니오 | `auto` | `auto`(ncc-xen: Running만 임시 스냅샷, Halted·Suspended는 그대로) / `always`(기존: 항상 임시 스냅샷). `disks`를 주면 항상 `always`(Q-VM-01) |
| `snapshot_name` | string | 아니오 | `NCC-BACKUP-<VM 이름>-<YYYYMMDDhhmmss>` | 임시 스냅샷 이름. 기존 BACKUP 전문의 스냅샷 이름 자리. MAC 보존 복구한 VM의 이름이 된다(실기 PT-02) |
| `keep_snapshot` | bool | 아니오 | false | 임시 스냅샷을 지우지 않고 남긴다 |
| `overwrite` | bool | 아니오 | false | 같은 이름 파일이 있으면 덮어쓴다. false면 작업 `failed`(`FILE_EXISTS`) |
| `verify_after_export` | bool | 아니오 | `backup.verify_after_export`(false) | 끝난 뒤 verify까지 한다. 결과의 `verify` 필드 |

`options` — verify(ncc-xen 17.3)

| 필드 | 형식 | 필수 | 뜻 |
|---|---|---|---|
| `file_path` | string | 예 | 검증할 파일. 절대 경로 또는 base_path 아래 상대 경로 |

`options` — restore(ncc-xen 17.4)

| 필드 | 형식 | 필수 | 기본 | 뜻 |
|---|---|---|---|---|
| `file_path` | string | 예 | - | 복구할 파일. gzip·zstd·비압축 자동 판별 |
| `sr_uuid` | string | 예 | - | 대상 SR uuid |
| `restore_original_uuid` | bool | 아니오 | false | true면 MAC·uuid 보존(기존 preserve=true). 원본 VM이 같은 풀에 있으면 XenServer가 거부한다(`DUPLICATE_MAC_SEED`) |
| `force` | bool | 아니오 | false | XAPI `force` 그대로 |
| `name_label` | string | 아니오 | 파일 속 이름 | 가져온 뒤 VM 이름을 바꾼다. 실패해도 작업은 성공, `name_label_set_error` |
| `home_server_uuid` | string | 아니오 | - | 가져온 뒤 Home Server 지정. 실패해도 작업은 성공 |

응답

| HTTP | 언제 | 본문 |
|---|---|---|
| 202 | 접수 | `{"taskId":"…","status":"ACCEPTED","sentAt":"<수신 시각>","message":""}` |
| 202 | 같은 taskId가 이미 있음 | 위와 같고 `message`가 `"duplicate taskId — already accepted, not re-executed"`(ncc-xen과 같은 문장, locale과 무관) |
| 400 | 9장의 요청 오류 | `{"error":"<문구>","code":"<코드>","field":"<JSON 경로>"}` |
| 401 | 토큰 | 8.1 |
| 503 | 대기열 가득, 종료 중 | `{"error":"…","code":"QUEUE_FULL" 또는 "SERVICE_STOPPING"}` |

예 — 디스크 0만 백업

```json
{
  "taskId": "6f1c2a4e-3b7d-4c55-9a1e-2d8f0b6c7e91",
  "jobType": "VM_BACKUP",
  "action": "export",
  "executionType": "async",
  "payload": {
    "targetType": "VM",
    "targetUUID": "42097417-b9ee-2f97-8eeb-3612efbcb25d",
    "masterIp": "192.168.102.53",
    "credentials": { "username": "root", "password": "ENC[v1:aes-256-gcm:k1:...]" },
    "options": { "file_name": "copy_20gb2-d0", "export_path": "/BACKUP/pool1", "disks": [0] }
  }
}
```

#### API-VM-02 `GET /api/v1/jobs/{taskId}`

| HTTP | 언제 | 본문 |
|---|---|---|
| 200 | 있음 | 작업 상태(아래) |
| 404 | 없음(보관 기간이 지나 지운 것 포함) | `{"error":"…","code":"TASK_NOT_FOUND"}` |

작업 상태

| 필드 | 형식 | 뜻 |
|---|---|---|
| `taskId`, `jobType`, `action` | string | 요청 그대로 |
| `status` | string | `queued` / `running` / `succeeded` / `failed` / `canceled` / `interrupted`(03 설계 10장) |
| `result` | string | ncc-xen 규약 2 값으로 바꾼 것: queued·running → `processing`, succeeded → `success`, 나머지 → `failure` |
| `percent` | int | 0~100. queued 0, 끝나면 100 |
| `step` | string | `queued`, `login`, `snapshot`, `exporting`, `cleanup`, `verifying`, `decompressing`, `uploading`, `importing`, `postprocess`, `done` |
| `acceptedAt`, `startedAt`, `finishedAt` | string | 없으면 빠진다 |
| `request` | object | 받은 요청(암호는 `***`) |
| `payload` | object | 규약 2의 payload와 같은 모양: `targetType`, `targetUUID`, `message`(locale 문구), `options`(결과 필드, 아래) |
| `error` | object | 실패·취소·중단일 때 `{"code":"…","detail":"<XAPI·파일 오류 원문>"}` |

결과 필드(`payload.options`) — ncc-xen 17.2~17.4와 같은 이름

| action | 필드 |
|---|---|
| export | `file_path`, `file_size_bytes`, `checksum_sha256`, `format`("xva"), `compression`, `exported_uuid`, `power_state`, `snapshot_used`, `snapshot_uuid`(keep_snapshot), `snapshot_cleanup_failed`, `snapshot_cleanup_error`, `duration_seconds`, `completedAt`, 더한 것: `disks`(담은 번호), `verify`(verify_after_export 결과 객체) |
| verify | `verified`, `chunk_count`, `checksum_entries`, `compression_detected`, `checksum_sha256`, `file_path`, `file_size_bytes`, `failure_reasons`, `completedAt` |
| restore | `vm_uuid`, `sr_uuid`, `file_path`, `file_size_bytes`, `restore_original_uuid`, `is_a_template_fixed`, `is_a_template_fix_error`, `allowed_operations_fix_error`, `name_label`, `name_label_set_error`, `home_server_uuid`, `home_server_applied`, `home_server_set_error`, `additional_vm_refs_ignored`, `duration_seconds`, `completedAt` |
| 취소·중단 | `snapshot_left`(지우지 못한 임시 스냅샷 uuid), `partial_file_removed`(bool) |

예 — 처리 중

```json
{
  "taskId": "6f1c2a4e-3b7d-4c55-9a1e-2d8f0b6c7e91", "jobType": "VM_BACKUP", "action": "export",
  "status": "running", "result": "processing", "percent": 42, "step": "exporting",
  "acceptedAt": "2026-11-19T01:00:00Z", "startedAt": "2026-11-19T01:00:01Z",
  "payload": { "targetType": "VM", "targetUUID": "42097417-…", "message": "VM 내보내는 중 (42%)", "options": {} }
}
```

#### API-VM-03 `GET /api/v1/jobs`

| 질의 | 뜻 | 기본 |
|---|---|---|
| `status` | 쉼표로 여러 개(`queued,running`) | 전부 |
| `action` | `export` 등 | 전부 |
| `since` | 이 시각 뒤에 접수된 것 | 없음 |
| `limit` | 최대 개수(1~1000) | 100 |

응답 200 `{"items":[<작업 상태>…],"count":<n>}`. 접수 시각 최신순. 목록의 `request`는 빼고 돌려준다(크기를 줄이려고).

#### API-VM-04 `POST /api/v1/jobs/{taskId}/cancel`

| HTTP | 언제 | 본문 |
|---|---|---|
| 200 | 대기 중이라 바로 취소됨 | 작업 상태(`status: canceled`) |
| 202 | 처리 중. XAPI task 취소를 시작함. 정리가 끝나면 `canceled` | 작업 상태(`status: running`, `step: canceling`) |
| 404 | 없음 | `TASK_NOT_FOUND` |
| 409 | 이미 끝남(succeeded·failed·canceled·interrupted) | `TASK_ALREADY_FINISHED` |

#### API-VM-05 `POST /api/v1/jobs/cancel-queued`

대기 중 작업만 모두 취소한다(기존 일괄 취소). 응답 200 `{"canceled":["<taskId>",…],"count":<n>}`. 처리 중 작업은 건드리지 않는다.

#### API-VM-06 `GET /health`

인증 없음. 응답 200 `{"status":"ok","service":"vbr_vm","version":"4.000","startedAt":"…","running":<n>,"queued":<n>,"authEnabled":true,"cryptoEnabled":true}`. 종료 중이면 503 `{"status":"stopping",…}`.

#### API-VM-07 결과 보고 `POST <result_report.url>` (송신)

`result_report.url`이 있을 때만. 본문은 ncc-xen 규약 2와 같다.

```json
{ "taskId": "…", "jobType": "VM_BACKUP", "action": "export", "result": "success",
  "sentAt": "…", "payload": { "targetType": "VM", "targetUUID": "…", "message": "…", "options": { … } } }
```

- 보내는 때: 끝났을 때(success / failure). `result_report.send_progress=true`면 진행 중에도 `processing` + `options.percent`를 `progress_interval_sec`마다 보낸다(기본 false).
- 헤더: `GATEWAY_INTERNAL_TOKEN` 값이 있으면 `X-Internal-Token`을 싣는다.
- 실패·취소·중단이면 `payload.options`에 `error_code`, `error_detail`을 더한다. `message`는 locale 문구다(DEC-VM-32).
- 다시 보내기: ncc-xen outbox와 같다. 5xx·429·408·연결 오류는 다시 보내고 `max_age_hours`가 지나면 Dead Letter. 400·401 등은 바로 Dead Letter.

### 8.4 참조 규약(ncc-xen v3.36)과 다른 점

| 항목 | ncc-xen | vbr_vm | 이유 |
|---|---|---|---|
| 풀 접속 정보 | Redis 풀 정보와 상시 세션. payload에 계정 없음 | `payload.credentials` 필수, 작업마다 로그인 | 지시 4 |
| 풀 식별 | `masterIp` 또는 `poolUUID`(poolUUID 우선) | `masterIp` 필수, `poolUUID`는 기록만 | Redis가 없어 poolUUID로 마스터를 못 찾는다 |
| 결과 받기 | 규약 2 밀어 보내기만 | 조회 API(02~05) + 규약 2(설정 시) | 지시 5·9 |
| 취소 | 없음 | API-VM-04·05 | 기존 기능 |
| export 디스크 선택 | 없음 | `options.disks` | 기존 기능(F-VM-02) |
| export 스냅샷 | Running만 | 기본은 같음(`auto`). `snapshot_mode: always`, `snapshot_name`을 더함 | 기존 동작을 요청으로 고를 수 있게(Q-VM-01) |
| export 압축 기본 | `none` | 같음(`none`) | Q-VM-02 |
| 백업 뒤 검증 | 없음(따로 verify) | `verify_after_export` | DEC-VM-10 |
| 400 본문 | `{"error":"invalid request body: …"}` | `error`에 더해 `code`, `field` | 호출자가 코드로 분기 |
| 토큰 이름 | `NCC_XEN_API_TOKEN` | `VBR_VM_API_TOKEN` | 서비스마다 토큰을 따로 둔다 |
| OVF/OVA, boot_mode | 있음 | 없음 | 범위 밖 |

## 9. 오류 코드

요청 단계(HTTP 응답)

| 코드 | HTTP | 뜻 | 다시 보내도 되는가 |
|---|---|---|---|
| `INVALID_REQUEST` | 400 | JSON이 아니거나 너무 큼 | 고친 뒤 |
| `MISSING_FIELD` | 400 | 필수 필드 없음(`field`에 경로) | 고친 뒤 |
| `INVALID_FIELD` | 400 | 형식이 틀림(uuid 형식, 파일 이름 문자, 빈 `disks`) | 고친 뒤 |
| `UNSUPPORTED_JOB` | 400 | 모르는 jobType·action·targetType 조합 | 아니오 |
| `PLAINTEXT_PASSWORD_REJECTED` | 400 | `crypto.enabled=true`인데 평문 암호 | 암호화 뒤 |
| `UNAUTHORIZED` | 401 | 토큰 | 토큰 고친 뒤 |
| `TASK_NOT_FOUND` | 404 | 조회·취소 대상 없음 | 아니오 |
| `TASK_ALREADY_FINISHED` | 409 | 끝난 작업 취소 | 아니오 |
| `QUEUE_FULL` | 503 | 대기열 가득(`concurrency.max_queue`) | 나중에 |
| `SERVICE_STOPPING` | 503 | 종료 중 | 재기동 뒤 |
| `STORE_ERROR` | 500 | 작업 기록을 쓰지 못해 접수하지 않음(기록 없이 202를 주지 않는다) | 저장소 확인 뒤 |

작업 단계(작업 상태의 `error.code`)

| 코드 | 상태 | 뜻 | `detail` |
|---|---|---|---|
| `CREDENTIAL_DECRYPT_FAILED` | failed | 키링으로 암호를 풀 수 없음 | 원인(키 ID 없음 등, 암호 값은 싣지 않음) |
| `XAPI_CONNECT_FAILED` | failed | 마스터에 접속 못 함 | 연결 오류 원문 |
| `XAPI_AUTH_FAILED` | failed | `SESSION_AUTHENTICATION_FAILED` | XAPI 원문 |
| `XAPI_ERROR` | failed | 그 밖의 XAPI 오류(예: `UUID_INVALID`, `DUPLICATE_MAC_SEED`) | XAPI 원문(예: `UUID_INVALID \| VM \| 0000…`) |
| `DISK_NOT_FOUND` | failed | `disks`의 번호가 VM에 없음 | 요청한 번호와 VM의 번호 |
| `INVALID_FIELD` | failed | `targetUUID`가 스냅샷인데 `disks`를 줌(스냅샷의 디스크를 지우지 않으려고, DEC-VM-29) | 사유 |
| `VM_STATE_UNSUPPORTED` | failed | `auto`에서 Running·Halted·Suspended 가 아닌 전원 상태(DEC-VM-29) | 전원 상태 |
| `PATH_NOT_ALLOWED` | failed | `sandbox_mode`에서 허용 밖 경로 | 경로 |
| `FILE_EXISTS` | failed | `overwrite=false`인데 파일 있음 | 경로 |
| `FILE_IO_ERROR` | failed | 파일 쓰기·읽기 실패 | 원문 |
| `EXPORT_INCOMPLETE` | failed | 스트림은 끝났는데 XAPI task가 성공이 아님 | task 상태·오류 |
| `TIMEOUT` | failed | `backup.timeout_hours` 넘음 | 경과 시간 |
| `CANCELED` | canceled | 취소 요청 | 누가 언제(API, 일괄) |
| `INTERRUPTED` | interrupted | 서비스가 멈췄다 다시 뜸, 또는 종료 신호 | 남은 스냅샷 uuid |

`message`(사람이 읽는 문구)는 locale을 따르고 `error.detail`은 번역하지 않는다(ncc-xen 18장과 같음).

## 10. 설정 파일 (`sysconfig.yaml`)

반영 시점: **즉시** = 파일을 저장하면 재기동 없이 반영(viper + fsnotify, 500ms 디바운스). **새 작업부터** = 즉시 반영하되 이미 시작한 작업은 시작 때 값으로 끝낸다. **재기동** = 다시 띄워야 반영(바꾸면 경고 로그).

| 경로 | 형식 | 기본 | 뜻 | 반영 | 기존 설정 대응 |
|---|---|---|---|---|---|
| `server.port` | int | 8090 | HTTP 수신 포트 | 재기동 | `PORT-NO` |
| `server.listen` | string | `0.0.0.0` | 수신 주소 | 재기동 | - |
| `api_auth.enabled` | bool | true | 토큰 검사 | 즉시 | `Allow Client`(대체) |
| `api_auth.VBR_VM_API_TOKEN` | string | - | 받는 토큰(`ENC[...]`). 환경 변수 `VBR_VM_API_TOKEN`이 우선 | 즉시 | - |
| `api_auth.GATEWAY_INTERNAL_TOKEN` | string | - | 결과 보고에 싣는 토큰. 환경 변수 우선 | 즉시 | - |
| `crypto.enabled` | bool | true | false면 평문 암호·토큰 허용(시험용, 경고 로그) | 즉시 | - |
| `crypto.keyring_file` | string | `./key.yaml` | 키링 | 즉시(다음 복호화부터) | `XAPI Keyring` |
| `locale` | string | ko | `ko` / `en` / `ja`. 모르는 값은 ko | 즉시 | - |
| `logging.*` | - | ncc-xen stdlogger와 같음. **`logging.level`을 적으면 나머지 키(`time_format`, `time_zone`, `max_size_mb` 등)도 모두 적어야 기동한다**(stdlogger 원본 동작, DEC-VM-30). 절을 통째로 빼면 stdlogger 기본값(text, ./logs/app.log). 아래 예를 그대로 쓰면 된다(`level`, `format`, `output`, `file_name`, `max_size_mb`, `max_backups`, `max_age_days`, `compress`, `local_time`, `time_format`, `time_zone`, `log_pattern`, `add_source`, `service_name`) | 로그 | 즉시 | `MAX Log size` |
| `backup.base_path` | string | `./backups` | 상대 경로의 기준 | 새 작업부터 | `Backup Root` |
| `backup.sandbox_mode` | bool | false | true면 base_path와 허용 목록 밖 거부 | 새 작업부터 | `Backup Root` 검사(대체) |
| `backup.allowed_absolute_paths` | string 배열 | [] | sandbox 허용 목록 | 새 작업부터 | - |
| `backup.timeout_hours` | int | 4 | 작업 시간 제한 | 새 작업부터 | - |
| `backup.verify_after_export` | bool | false | `options.verify_after_export` 생략 때 | 새 작업부터 | - |
| `concurrency.max_jobs` | int | 3 (1~10) | 동시 처리 수. 늘리면 바로, 줄이면 처리 중인 것이 끝나는 대로 | 즉시 | `Async Thread` |
| `concurrency.max_queue` | int | 100 | 대기열 크기 | 즉시 | 기존 100칸 |
| `xapi.verify_ssl` | bool | false | 마스터 인증서 검증 | 새 작업부터 | `XAPI Verify SSL` |
| `xapi.progress_interval_sec` | int | 5 | 진행률 갱신 간격 | 새 작업부터 | - |
| `task_store.dir` | string | `./data/tasks` | 작업 기록 위치 | 재기동 | `transaction-YYYYMM.log`(대체) |
| `task_store.retention_days` | int | 30 | 끝난 작업 기록 보관 | 즉시 | - |
| `result_report.url` | string | "" | 결과 보고 주소(비면 안 보냄) | 즉시 | - |
| `result_report.send_progress` | bool | false | 진행 중 보고 | 즉시 | - |
| `result_report.max_age_hours` | int | 24 | 다시 보내기 한도 | 즉시 | - |
| `result_report.deadletter_retention_days` | int | 30 | Dead Letter 보관 | 즉시 | - |

- 파일 위치: `-config` → 실행 파일 옆 → 현재 디렉터리.
- 모르는 키: 경고하고 무시.
- 바뀐 파일을 읽지 못하면(YAML 오류, 범위 밖 값): 이전 설정을 그대로 쓰고 오류 로그를 남긴다. 기동 때 읽지 못하면 종료 코드 1.
- 예:

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

## 11. 기존 인터페이스 대응 (vbr_agent 3.000 → vbr_vm)

| 기존 | 새 인터페이스 | 달라진 점 |
|---|---|---|
| `B<번호>A<시간><우선>BACKUP <vm> <스냅샷 이름> <전체 경로>` | API-VM-01 `action: export`, `targetUUID`, `options.snapshot_name`, `export_path`+`file_name`, **`compression: "gzip"`, `snapshot_mode: "always"`** | 기존과 같은 결과를 원하면 굵은 두 옵션을 함께 보낸다(기본은 압축 없음, Halted VM은 스냅샷 없이). 경로를 디렉터리와 이름으로 나눈다. 우선순위 없음 |
| `PARTIAL <vm> <15자리> <이름> <경로>` | 같은 export + `options.disks` | 15자리 자리값 → 번호 목록(자리 i가 `1`이면 i를 넣는다) |
| `RESTORE <경로> <true\|false> <sr>` | API-VM-01 `action: restore`, `file_path`, `restore_original_uuid`, `sr_uuid` | 결과 VM 이름 바꾸기, Home Server 지정을 더 할 수 있다 |
| `Q<번호>` 조회 → 결과 코드 0·2·3·6·C·9 | API-VM-02 `status` | 0(끝남) → `succeeded`/`failed`(실패도 0이던 것을 나눔), 2 → `queued`, 3 → `running`, 6 → `interrupted`, C → `canceled`, 9(없음) → 404 |
| `C<번호>` 취소 | API-VM-04 | 처리 중도 취소된다 |
| 일괄 취소 | API-VM-05 | 같음 |
| (없음) | API-VM-03 목록, API-VM-06 상태 | 새로 생김 |
| 결과 문장 `VM BACKUP Success : VM uuid=…, Snapshot VM uuid=…, backup filename=…` | `payload.options.exported_uuid`, `file_path` 등 | 문장 대신 필드 |
| 결과 `[ERROR]XAPI Error: …` | `status: failed`, `error.code`, `error.detail` | 오류 원문은 `detail`에 그대로 |
| `node_info.cfg` | `sysconfig.yaml`(10장 마지막 열) | |
| `vbr_secret encrypt` | 같은 도구, 같은 키링 형식 | 같음 |
