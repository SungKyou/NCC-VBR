# vbr_vm 시험 계획과 결과

기준일: 2026-11-19 / 대상: `go/cmd/vbr_vm` 4.000, `go/internal/{vm,vm/api,xva,jobstore,vmconfig,apiauth,redact,i18n,stdlogger}` / 상태: **개발 검증 통과(V0·V1·데모 자동 모드), 실기 V2·V3 일부 통과(I01~I03, I05~I10, 2026-11-19 Windows PC), 남은 실기 I04·L01 등(6장)**. 판정 표현은 [검증 절차](../00-process/02-verification-procedure.md) 5장.

## 1. 판정

| 수준 | 내용 | 판정 | 실행일 | 증거 |
|---|---|---|---|---|
| V0 정적 검사 | `gofmt -l .`(0건), `go vet ./...`, `govulncheck ./...` v1.9.0 | 통과. govulncheck 첫 실행은 호출되는 취약점 6건 → 의존·도구를 올린 뒤 0건(DEC-VM-28) | 2026-11-19 | `evidence/vm/unit-windows.txt` 앞부분, `evidence/vm/govulncheck.txt` |
| V1 단위 시험 Windows | `go test -p 1 -count=1 -v ./...`(전체 저장소) | 통과 76, 건너뜀 4, 실패 0. vbr_vm 새 시험 24건과 복사한 stdlogger 시험 1건 포함 | 2026-11-19 | `evidence/vm/unit-windows.txt` |
| V1 단위 시험 Linux | WSL Ubuntu, `go/test/run-linux-tests.sh`(시험 실행 파일 15개) | 통과 75, 건너뜀 1, 실패 0 | 2026-11-19 | `evidence/vm/unit-linux.txt` |
| 데모 자동 모드 | `go/test/vm/demo.ps1 -Auto`: 가짜 XAPI 2개 + `vbr_vm.exe` 실제 실행 | **PASS 16, FAIL 0** | 2026-11-19 | `evidence/vm/demo-auto.txt` |
| 회귀(DEC-VM-18·28·33) | Go 판·의존·`vbr_secret` 변경 뒤 cifs·remove·agent | 단위 시험 위 V1에 포함 통과. cifs·remove 원본 비교 `RESULT: ALL PASS`. agent 원본 비교 다름 0(2차), 마지막 3번은 다름 3·1·0: P02 한 요청의 시점 경합(5장) | 2026-11-19 | `evidence/vm/regression-*.txt` |
| V2 기존 대비 비교 | 1단계 실기 R1~R9 시나리오를 새 API로 다시 실행해 풀에 남는 결과를 1단계 기록(`agent/06` 5.3)과 비교 | 일부 통과(5.1). R2·R3·R7·R8 대응 항목은 같은 모양. R4(MAC 보존)·R6 같은 조건(gzip·always)은 남음 | 2026-11-19 | `evidence/vm/windows-real-20261119/` |
| V3 통합 | 실제 XenServer 9 풀(192.168.102.53), demo.ps1 실기 모드 1~9 | 일부 통과(5.1): I01·I02·I03·I05·I06·I07·I08·I09. 실기에서 전환본 결함 3건을 찾아 고쳤다(5.1 아래, [04](04-change-register.md) 6장) | 2026-11-19 | 같음 |
| V4 운영 규모 | 운영 크기 VM, 동시 3개 | 미실행(사용자 실기 필요) | | |

전체 판정: **개발 검증 통과, 실기 일부 통과**. 실기에서 찾은 결함 3건은 고친 뒤 Windows 77/4/0, Linux 76/1/0 으로 다시 통과했다(`evidence/vm/windows-real-20261119/unit-*-after-fixes.txt`). 남은 실기는 6장.

## 2. 시험 환경

| 항목 | 값 |
|---|---|
| 개발 PC | Windows 11, Go toolchain go1.27.4(`go.mod`가 지정해 자동으로 받음, DEC-VM-28), pwsh 7.6 |
| Linux | WSL Ubuntu(`go/test/run-linux-tests.sh`) |
| 가짜 XAPI | `go/internal/vm/vmtest`(1단계 `agent/xen_test.go`의 가짜를 넓힘: 전원 상태, 디스크 3개, 스냅샷, `task.cancel`, 느린 내보내기, 결과 VM, 틀린 암호). 단위 시험이 직접 쓰고, `go/test/vm/fakexapi`(`vm-fakexapi.exe`)가 데모 자동 모드용으로 띄운다 |
| 실제 풀 | XenServer 9, 마스터 192.168.102.53, 시험 VM `42097417-…`(지우지 않음), 기존 스냅샷 `926b6f3f-…`(지우지 않음), SR `ASUS NFS` |

## 3. 시험 목록

| 번호 | 수준 | 확인하는 것 | 방법 | 관련 |
|---|---|---|---|---|
| T-VM-U01 | V1 | 정상 요청 → 202, 응답 모양이 ncc-xen과 같다 | httptest + 가짜 작업자 | REQ-VM-01, API-VM-01 |
| T-VM-U02 | V1 | 필수 값 없음·형식 오류 → 400과 코드·필드(표로 action별 전부) | httptest | REQ-VM-02 |
| T-VM-U03 | V1 | 같은 taskId 두 번, 동시에 100번 → 실행 1번, 나머지 duplicate | httptest + 고루틴 | REQ-VM-04 |
| T-VM-U10 | V1 | XAPI 오류 원문이 `error.detail`에 그대로, `status: failed` | 가짜 XAPI 오류 | REQ-VM-03, C-VM-05 |
| T-VM-U20 | V1 | export: `.partial` → 이름 바꿈, task 실패면 파일 없음, SHA256·크기 | 가짜 XAPI | REQ-VM-05 |
| T-VM-U21 | V1 | `disks:[0]` → 스냅샷의 디스크 1 VBD·VDI 삭제 뒤 내보냄, 원본 VM 디스크는 그대로 | 가짜 XAPI 호출 기록 | REQ-VM-06 |
| T-VM-U22 | V1 | 성공·실패·취소 모두 임시 스냅샷과 VDI 삭제, 삭제 실패 시 `snapshot_cleanup_*` | 가짜 XAPI | REQ-VM-07 |
| T-VM-U23 | V1 | compression none·gzip·zstd 각각 파일 이름·`use_compression` 파라미터·내용 | 가짜 XAPI | REQ-VM-08 |
| T-VM-U24 | V1 | 파일 있음 + overwrite false → `FILE_EXISTS`, true → 덮어씀. 확장자 자동. 상대·절대 경로 | 임시 디렉터리 | REQ-VM-09, C-VM-16·17 |
| T-VM-U25 | V1 | sandbox_mode 켬/끔 | 임시 디렉터리 | C-VM-10 |
| T-VM-U26 | V1 | snapshot_mode auto/always × Halted/Running/스냅샷 uuid/disks | 가짜 XAPI | C-VM-19, 03 3장 표 |
| T-VM-U30 | V1 | restore: `restore`·`force`·`sr_id` 파라미터, 템플릿 후처리 + allowed_operations | 가짜 XAPI | REQ-VM-10, C-VM-14 |
| T-VM-U31 | V1 | zstd 파일 restore → 임시 해제 후 업로드, 끝나면 임시 파일 삭제 | 가짜 XAPI | REQ-VM-11 |
| T-VM-U32 | V1 | name_label·home_server 후처리 실패해도 succeeded | 가짜 XAPI | C-VM-15 |
| T-VM-U40 | V1 | verify: 정상, 청크 손상, tar 손상, 열기 실패(→ failed) | 시험 XVA 파일 | REQ-VM-12 |
| T-VM-U41 | V1 | verify_after_export 결과가 export 결과에 들어감 | 가짜 XAPI | REQ-VM-13 |
| T-VM-U50 | V1 | 조회: 상태·진행률·단계 갱신, 목록 거르기·정렬·limit | httptest | REQ-VM-14 |
| T-VM-U51 | V1 | 취소: 대기 → 200 canceled, 처리 중 → 202 후 canceled + 정리, 끝난 것 → 409 | 느린 가짜 XAPI | REQ-VM-15 |
| T-VM-U52 | V1 | max_jobs 2에 작업 3개 → 셋째는 queued. 설정을 3으로 바꾸면 바로 시작 | 느린 가짜 XAPI | REQ-VM-16 |
| T-VM-U53 | V1 | 기록에 queued·running이 남은 채 다시 열면 interrupted | 임시 디렉터리 | REQ-VM-17 |
| T-VM-U54 | V1 | 압축: taskId마다 마지막 줄, 보관 기간 지난 것 삭제, 도중 중단에도 파일 안 깨짐 | 임시 디렉터리 | REQ-VM-19 |
| T-VM-U60 | V1 | 결과 보고: 주소 없음 → 안 보냄, 5xx → 재시도, 400 → Dead Letter, 토큰 헤더 | httptest 수신기 | REQ-VM-18 |
| T-VM-U70 | V1 | /health 인증 없이 200 | httptest | REQ-VM-20 |
| T-VM-U71 | V1 | 설정 즉시 반영: locale, max_jobs, api_auth. 잘못된 YAML이면 이전 값 유지. 재기동 키는 경고 | 임시 설정 파일 | REQ-VM-N03, DEC-VM-17 |
| T-VM-U72 | V1 | 종료 신호: 새 접수 503, 처리 중 → interrupted | 느린 가짜 XAPI | REQ-VM-N03 |
| T-VM-U73 | V1 | 작업 로그에 taskId 속성 | 메모리 로그 | REQ-VM-N07 |
| T-VM-U74 | V1 | locale ko/en/ja 문구, 모르는 값은 ko, detail은 번역 안 함 | 단위 | REQ-VM-N09 |
| T-VM-U80 | V1 | 토큰 없음·틀림 → 401(ncc-xen 문장), enabled=false → 통과 + 경고 | httptest | REQ-VM-N06 |
| T-VM-U81 | V1 | crypto true + 평문 암호 → 400, false → 받음 + 경고 | httptest | REQ-VM-N06 |
| T-VM-U82 | V1 | 로그·작업 기록·조회 응답·결과 보고 어디에도 암호 평문이 없다(전부 검색) | 시험 끝에 파일·응답 검색 | REQ-VM-N06 |
| T-VM-I01 | V3 | 전체 백업(1단계 R2와 비교: 파일 크기, 시간, 스냅샷 정리) | demo.ps1 실기 | REQ-VM-05·07 |
| T-VM-I02 | V3 | 디스크 0만 백업 + 복구(R5와 비교) | demo.ps1 실기 | REQ-VM-06 |
| T-VM-I03 | V3 | 복구 restore_original_uuid=false(R3와 비교: MAC 다름, 템플릿 아님, XenCenter Start 가능) | demo.ps1 실기 | REQ-VM-10 |
| T-VM-I04 | V3 | 복구 true(R4·PT-02와 비교: 원본 있으면 DUPLICATE_MAC_SEED, 지운 뒤면 MAC 같음) | demo.ps1 실기 | REQ-VM-10 |
| T-VM-I05 | V3 | verify: 백업 파일 3종(none·gzip·zstd) | demo.ps1 실기 | REQ-VM-12 |
| T-VM-I06 | V3 | 동시 3개, 조회로 진행률이 오르는지(R6와 비교) | demo.ps1 실기 | REQ-VM-14·16 |
| T-VM-I07 | V3 | 처리 중 취소 → 풀에 임시 스냅샷·export task가 남지 않음 | demo.ps1 실기 + dom0 확인 | REQ-VM-15 |
| T-VM-I08 | V3 | 없는 VM·SR·틀린 암호(R8과 비교) | demo.ps1 실기 | REQ-VM-03 |
| T-VM-I09 | V3 | 처리 중 강제 종료 → 재기동 뒤 interrupted, 남은 스냅샷 기록(R7과 비교) | demo.ps1 실기 | REQ-VM-17 |
| T-VM-I10 | V3 | Halted VM 스냅샷 없이 내보내는 동안 VM 시작 시도(Q-VM-01 자료) | dom0 `xe vm-start` | Q-VM-01 |
| T-VM-L01 | V4 | 운영 크기 VM 백업 시간: gzip·zstd, 1단계 `vbr_agent`와 같은 조건 | 사용자 실행 | REQ-VM-N02 |

## 4. 추적표 (계획한 시험 → 실제 시험)

요건 → 시험 번호는 3장 "관련" 열과 [01](01-requirements.md) 3·4장 "검증" 열이 같다. 아래는 시험 번호가 어느 시험 함수·데모 단계로 실행됐는지다.

| 시험 번호 | 실제 시험 | 결과 |
|---|---|---|
| U01, U03 | `api_test.go` `TestDuplicateConcurrent`(50개 동시, 실행 1번), 모든 접수 시험의 202 모양 | 통과 |
| U02 | `TestSubmitValidation`(19가지), 데모 U02 | 통과 |
| U10 | `TestXAPIErrors`(UUID_INVALID 원문, 틀린 암호, export HTTP 500, task failure) | 통과 |
| U20, U26 | `TestExportHaltedAuto` | 통과 |
| U21, U22, U26 | `TestExportRunningWithDisks`, `TestExportDiskNotFound` | 통과 |
| U23, U41 | `TestExportZstdVerifyAfter`, 데모 U41 | 통과 |
| U24, U25 | `TestExportFileExists`, `xva/path_test.go` `TestPathRules` | 통과 |
| U30, U32 | `TestRestore`(파라미터, 템플릿·allowed_operations·이름·Home Server, 없는 SR 은 업로드 전 실패) | 통과 |
| U31 | `TestRestoreZstd`(푼 내용 = 원본, 임시 파일 없음) | 통과 |
| U40 | `TestVerify`(정상, 체크섬 불일치, 없는 파일) | 통과 |
| U50, U52 | `TestWorkersAndCancelQueued`(목록 거르기, max_jobs 1→2 즉시 반영, cancel-queued) | 통과 |
| U51 | `TestCancel`(대기 200, 처리 중 202→canceled, task.cancel, .partial 삭제, 409, 404), 데모 I07 | 통과 |
| U53, U54 | `jobstore/store_test.go` `TestReopenMarksInterrupted`, `TestCompactAndRetention` | 통과 |
| U60 | `TestOutbox`(주소 없음, 5xx 재시도, 400 Dead Letter, 토큰 헤더) | 통과. `send_progress`(진행 중 보고)는 시험 없음(아래 6장) |
| U70, U80 | `TestHealthAndAuth`, 데모 U70·U80 | 통과 |
| U71 | `vmconfig/config_test.go` `TestLoadAndReload`(반영, 잘못된 파일은 이전 값 유지, 재기동 키 경고), 데모 U71(실제 파일 감시로 locale 전환) | 통과 |
| U72 | `TestShutdown` | 통과 |
| U73 | 데모 실행 로그 `vbr_vm.log`에 `taskId=` 속성 확인(증거 `demo-auto.txt` 작업 폴더) | 정적 확인 |
| U74 | `i18n/catalog_test.go` `TestCatalogParity`(키 44개 × 3개 언어, 형식 지시자) | 통과 |
| U80~U82 | `TestHealthAndAuth`, `TestSubmitValidation`(평문 거부), `TestSecretsNotLeaked`(ENC 복호화로 성공, 평문·ENC 값이 기록·응답에 없음), 데모 U82(로그·기록) | 통과 |
| I01~I03, I05~I09 | 데모 실기 모드(5.1) | 통과(I08 없는 SR 은 시험 스크립트 수정 뒤 사용자 판정, I09 의 `snapshot_left` 표시는 단위 시험으로 확인) |
| I10 | 가이드 6.3(메뉴 2 auto·none + dom0 `xe vm-start`) | 통과(5.1) |
| I04, L01 | 데모 실기 모드 | 미실행(6장) |

## 5. 실행 기록

| 일시 | 수준 | 명령 | 결과 | 실패 원인 | 조치 | 증거 |
|---|---|---|---|---|---|---|
| 2026-11-19 | 회귀 1차 | `go.mod` go 1.25 + toolchain go1.27.3 뒤 전체 단위 시험·원본 비교 | Windows 51/4/0, Linux 50/1/0, cifs·remove ALL PASS(43), agent 같음 96·의도한 차이 22·다름 0 | - | - | (2차로 대체) |
| 2026-11-19 | V1 | `go test ./internal/vm/...` | 16 통과 | - | - | - |
| 2026-11-19 | V1 | `go test ./internal/jobstore ./internal/vmconfig …` | 실패 2 | 전환본: 재기동 처리에서 단계를 "done"으로 바꾼 뒤 오류 설명을 만들어 원래 단계가 빠짐. 시험: 설정 파일에 `locale` 키 중복 | `jobstore.Open` 순서 수정, 시험 수정 | - |
| 2026-11-19 | 데모 1차 | `demo.ps1 -Auto` | FAIL 1(기동 실패 `logging.time_format is required`) | 시험: 데모 설정의 `logging` 절이 불완전(stdlogger 원본 규칙) | 데모 설정에 전체 키, 05 문서에 규칙 명시(DEC-VM-30) | - |
| 2026-11-19 | 데모 2차 | `demo.ps1 -Auto` | PASS 16 | - | - | - |
| 2026-11-19 | V0 | `govulncheck ./...` | 호출되는 취약점 6건 | 의존·도구 판: go1.27.3 `net/http`, x/net v0.51.0, quic-go v0.59.0 | x/net v0.61.0, quic-go v0.64.1, toolchain go1.27.4(DEC-VM-28) | `govulncheck.txt`(재검사 결과로 덮음) |
| 2026-11-19 | V0 | `govulncheck ./...` 재검사 | 0건 | - | - | `govulncheck.txt` |
| 2026-11-19 | V0·V1 | gofmt·vet·전체 단위 시험(Windows, Linux) | Windows 76/4/0, Linux 75/1/0 | - | - | `unit-windows.txt`, `unit-linux.txt` |
| 2026-11-19 | 회귀 2차 | 원본 비교(go1.27.4) | cifs·remove ALL PASS, agent 같음 97·의도한 차이 21·다름 0 | - | - | `regression-parity-*.txt` |
| 2026-11-19 | 데모 3차 | `demo.ps1 -Auto`(go1.27.4 빌드) | PASS 16, FAIL 0. 띄운 프로세스 남지 않음 | - | - | `demo-auto.txt` |
| 2026-11-19 | 회귀 3차 | `vbr_secret` 용도 인자(DEC-VM-33) 뒤 전체 단위 시험, 빌드, 원본 비교 | Windows 76/4/0, cifs·remove ALL PASS. agent 원본 비교 3번: 다름 3 → 1 → 0 | 시험 환경(시점 경합): 차이는 모두 P02(인자 오류 결과) 하나이고 C·Go 양쪽 방향으로 번갈아 난다. 결과 기록의 마지막 단계가 5에서 0으로 바뀌는 순간에 조회가 걸린다. agent 코드는 이번에 바뀌지 않았다(`git status`) | 시험 도구는 고치지 않고 알려진 불안정 항목으로 둔다(아래) | `regression-parity-agent-final-run1~3.txt` |
| 2026-11-19 | 실기 준비 | 사용자 PC 에서 `vbr_vm.exe -config C:/temp/vbrvm/sysconfig.yaml`(05 10장 예, `time_zone: "Asia/Seoul"`) | 기동 실패 `invalid logging.time_zone: "Asia/Seoul"` | 전환본: Windows 에는 IANA 시간대 자료가 없고 실행 파일이 Go 의 `zoneinfo.zip`도 찾지 못한다. 데모 자동 모드 설정은 `time_zone: "local"`이라 잡지 못했다 | `cmd/vbr_vm/main.go`에 `_ "time/tzdata"`(시간대 자료를 실행 파일에 넣음, 약 450 KB). 재빌드 뒤 같은 설정 형식(`Asia/Seoul`)으로 기동·`/health` 200, 로그 시각 KST 확인 | 대화 기록 |
| 2026-11-19 | 실기 준비 | `demo.ps1` 메뉴 1(실기 모드) | 키링 경로를 비운 채 넘어가 암호화 실패, 빈 암호로 메뉴 2 를 보내 400 `MISSING_FIELD payload.credentials.password` | 시험 스크립트: 키링 경로에 기본값이 없고, 빈 값과 암호화 실패를 막지 않았다 | 기본값(`$env:VBR_VM_KEYRING`, 없으면 현재 폴더 `key.yaml`), 파일이 있을 때까지 다시 물음, 암호화 실패면 세션 준비 실패로 처리. `demo.ps1 -Auto` PASS 16 재확인 | 대화 기록 |
| 2026-11-19 | 실기 중 | `demo.ps1` 메뉴 4·5(사용자 지적) | 복구·검증의 `file_path` 기본값 없음. 메뉴 6 은 압축·스냅샷을 묻지 않아 none·auto 로 같은 VM 을 3개 보냄(1단계 R6 과 조건이 다르고 파일이 VM 디스크 크기만큼) | 시험 스크립트 | 메뉴 2·3에서 마지막으로 성공한 백업 파일을 복구·검증 기본값으로, 빈 값이면 다시 물음. 메뉴 6 에 VM·compression(기본 gzip)·snapshot_mode(기본 always) 입력과 dom0 확인 안내. 메뉴 8 에 없는 SR(`XAPI_ERROR UUID_INVALID \| SR`, 1단계 R8) 추가. `demo.ps1 -Auto` PASS 16 | 대화 기록 |
| 2026-11-19 16:30 | V3 I06 | `demo.ps1` 메뉴 6(고치기 전 판: none·auto, Halted 시험 VM 같은 것 3개 동시) | 3개 모두 succeeded, 각 5,983,571,968바이트(I01 `test1.xva`와 같은 크기), 4분 0~12초. `snapshotUsed=false`. 같은 Halted VM 의 스냅샷 없는 내보내기 3개가 동시에 진행됨 | 진행 중 관찰 두 가지: ① `.partial` 크기가 탐색기·`Get-ChildItem`에서 0 으로 보임 — Windows 는 쓰는 중인 파일의 디렉터리 항목 크기를 닫을 때 갱신한다(서비스 문제 아님) ② 진행률이 1→26% 까지만 오르다 끝에서 99% 로 건너뜀 — XAPI export task 의 progress 를 그대로 쓰는 ncc-xen 방식(`progress*98+1`)이 실제 전송량을 따라가지 않는다 | ②는 개선 여부 사용자 결정 대기 | `C:/temp/vbrvm/vbr_vm.log`, `tasks\jobs.jsonl` |
| 2026-11-19 16:37 | V3 I07 | `demo.ps1` 메뉴 7(`snapshot_mode: always`, 15초 뒤 취소) | `canceled` / `CANCELED`, `.partial` 지워짐. **임시 스냅샷 `55689cc6-…`(NCC-BACKUP-copy_20gb2-20261119163708)이 남음**: `VDI.destroy failed: VDI_IN_USE` | 전환본: `task.cancel` 직후 바로 스냅샷을 지웠다. XenServer 는 내보내기를 멈추는 동안 스냅샷 디스크를 더 잡고 있다. 가짜 XAPI 는 이 상태를 흉내 내지 않아 단위 시험이 잡지 못했다 | ① `xva.settleIfCanceled`: 취소된 export·import 는 XAPI task 가 pending·cancelling 을 벗어날 때까지(최대 60초) 기다린 뒤 돌아온다 ② `vm.uninstall`: `VDI_IN_USE`·`OTHER_OPERATION_IN_PROGRESS`면 2초 간격으로 `cleanupTimeout`(2분) 안에서 다시 지운다. 공통 `xapi` 패키지는 바꾸지 않음 ③ 가짜 XAPI 에 `Cancelling`·`VDIInUse`, 회귀 시험 `TestCancelSnapshotInUse`. 전체 단위 시험 통과. 남은 스냅샷은 사용자가 dom0 에서 지움. 메뉴 7 재실기 필요 | 대화 기록, `C:/temp/vbrvm/vbr_vm.log` |
| 2026-11-19 | V3 I07 재실기 | 새 빌드(취소 정리 수정) 메뉴 7 | 정상(사용자 확인: canceled, 임시 스냅샷 남지 않음) | - | - | 대화 기록 |
| 2026-11-19 | V3 I08 | `demo.ps1` 메뉴 8 | 없는 VM `XAPI_ERROR UUID_INVALID \| VM`, 없는 복구 파일 `FILE_IO_ERROR`, 틀린 암호 `XAPI_AUTH_FAILED SESSION_AUTHENTICATION_FAILED`, 필수 값 없음 400 `MISSING_FIELD payload.options.file_name` 모두 PASS. **없는 SR 은 FAIL**(`FILE_IO_ERROR`) | 시험 스크립트: 없는 SR 케이스에 없는 파일을 넣었다. 서비스는 로컬 파일 확인 → 로그인 → SR 확인 → 해제·업로드 순이라 파일 오류가 먼저 났다(전송 전 SR 실패라는 설계 의도 E-05 와 맞음) | 케이스가 실제로 있는 백업 파일(기본: 마지막 백업 또는 `test1.xva`)을 쓰게 고침. 재실기 필요 | 대화 기록 |
| 2026-11-19 15:55 | V3 I09 | 메뉴 2(`always`) 3% 에서 `Stop-Process -Name vbr_vm -Force` → 재기동 → `GET /api/v1/jobs?status=interrupted` | `export-58d5225919c6`: `interrupted`, `INTERRUPTED`, detail `service restarted while the task was running (step exporting)`, percent 3 에서 멈춤(자동 재개 없음). `test3.xva.partial` 753,805,824바이트와 dom0 임시 스냅샷 1개가 남음(1단계 R7 과 같은 모양). **남은 스냅샷 uuid 가 결과에 없음** | 전환본: REQ-VM-17·05 9장(INTERRUPTED detail 에 남은 스냅샷 uuid)을 구현하지 않았다 | 임시 스냅샷을 만들면 바로 작업 기록 `result.snapshot_left`에 uuid 를 적고(`progress.note`), 재기동 때 detail 에 `; temporary snapshot may remain: <uuid>`를 붙인다. 정상·실패·취소로 끝나면 최종 결과가 덮어써 없어진다. 시험 `TestReopenMarksInterrupted`, `TestCancelSnapshotInUse`에 확인 추가. 전체 단위 시험 통과. 재빌드 뒤 I09 재실기 필요 | 대화 기록 |

agent 원본 비교의 같음·의도한 차이 수가 실행마다 96/22, 97/21로 다른 것과 회귀 3차의 P02 차이는 시험 환경의 시점 경합이다. 그중 같음·의도한 차이 수가 다른 것은 C-AG-03(전송 시점) 1건이 실행마다 달라지기 때문이다. 통합 기록([05-merge-record.md](../00-process/05-merge-record.md))에도 같은 설명이 있다.

### 5.1 실기 결과 요약 (2026-11-19, Windows PC → XenServer 9 풀)

`vbr_vm.exe`(Windows)를 `C:/temp/vbrvm`에서 띄우고 `demo.ps1` 실기 모드로 실행했다. 시험 VM `42097417-…`(Halted, 이름 `copy_20gb2`). 근거: `evidence/vm/windows-real-20261119/jobs.jsonl`, `vbr_vm.log`(암호·토큰·ENC 값 없음 확인).

| 시험 | 조건 | 결과 | 1단계 대응 | 판정 |
|---|---|---|---|---|
| I01 전체 백업 | none, auto(Halted → 스냅샷 없음) | 5,983,571,968바이트, 108초, SHA256 기록 | R2(gzip·스냅샷 2.59 GB, 4분 30초). 조건이 달라 크기·시간은 비교하지 않음 | 통과 |
| I02 디스크 선택 | `disks:[1]`, 임시 스냅샷 | 2,134,528바이트, 임시 스냅샷 생성·삭제 | R5(디스크 0). 디스크 번호가 다름 | 통과 |
| I03 복구 | `test1.xva`, `restore_original_uuid=false`, `name_label: test_1_sk` | 122초, 새 VM `0e96b402-…`, 템플릿 아님 | R3 | 통과 |
| I05 검증 | `test1.xva`(none) | `verified=true`, chunk 5,698, 6초 | (1단계 없음) | 통과. gzip·zstd 파일은 남음 |
| I06 동시 | 고치기 전 메뉴: none·auto, 같은 Halted VM 3개 | 3개 모두 성공, 각 5,983,571,968바이트, 240~252초. **같은 Halted VM 의 스냅샷 없는 내보내기 여러 개가 동시에 돈다** | R6(gzip·스냅샷, 작업자 2) | 통과. R6 같은 조건은 남음 |
| I07 처리 중 취소 | always, 15초 뒤 취소 | 1차: canceled, `.partial` 삭제, **임시 스냅샷이 남음**(`VDI_IN_USE`) → 결함 D-VM-02 수정 → 2차: canceled, 스냅샷·`.partial` 없음 | R6 의 처리 중 취소 거부를 대체 | 통과(2차) |
| I08 오류 | 없는 VM / 없는 SR / 없는 파일 / 틀린 암호 / 필수 값 없음 | `XAPI_ERROR UUID_INVALID \| VM`, (없는 SR 은 시험 스크립트가 없는 파일을 넣어 `FILE_IO_ERROR` → 스크립트 수정), `FILE_IO_ERROR`, `XAPI_AUTH_FAILED`, 400 `MISSING_FIELD` | R8 | 통과(사용자 판정) |
| I09 강제 종료 | always, 3% 에서 `Stop-Process -Force` → 재기동 | `interrupted`/`INTERRUPTED`, detail `… (step exporting)`, 다시 실행 안 함. `.partial` 753,805,824바이트와 임시 스냅샷이 남음(R7 과 같은 모양). **결과에 남은 스냅샷 uuid 가 없음** → 결함 D-VM-03 수정(`snapshot_left`) | R7 | 통과(사용자 판정, 수정분은 단위 시험) |

| I10 Halted 내보내기 중 VM 시작 | Halted, auto·none(스냅샷 없음), 진행 1% 에서 dom0 `xe vm-start` | `vm-start`가 응답 없이 기다리다가 내보내기가 끝난 뒤 시작됨. 백업 68초, 5,983,571,968바이트, succeeded, `snapshot_used=false` | (1단계는 항상 스냅샷이라 해당 없음) | 통과. Q-VM-01 판단 자료의 추정(VM 잠김)을 확인: 거부가 아니라 대기 |

실기 중 관찰(결함 아님)

| 관찰 | 내용 | 반영 |
|---|---|---|
| `.partial` 크기 0 | 쓰는 중인 파일은 Windows 디렉터리 항목 크기가 닫을 때 갱신돼 탐색기·`Get-ChildItem`에 0 으로 보인다 | 07 7장 |
| 진행률 | XAPI export task 의 progress 가 실제 전송량보다 낮다. none 백업에서 25~26% 까지만 오르고 끝에서 99% 로 건너뛴다(ncc-xen 같은 계산) | 결정 대기 Q-VM-03([04](04-change-register.md) 5장) |
| 복구 순서 | 로컬 파일 확인 → 로그인 → SR 확인 → 해제·업로드. 없는 파일이면 SR 보다 파일 오류가 먼저 난다 | 05 9장 그대로(설계 의도와 맞음) |

## 6. 실행하지 못한 시험

| 번호 | 사유 | 재개 조건 | 누가 |
|---|---|---|---|
| I04 복구 MAC 보존 | 실기에서 하지 않았다 | 메뉴 4 에서 `restore_original_uuid` y. 원본이 있으면 `DUPLICATE_MAC_SEED`(R4) | 사용자 |
| I05 gzip·zstd 검증 | none 파일만 검증했다 | 메뉴 2 를 gzip·zstd 로 백업한 뒤 메뉴 5 | 사용자 |
| I06 R6 같은 조건 | 고치기 전 메뉴로 none·auto 만 했다 | 메뉴 6, gzip·always | 사용자 |
| I08 없는 SR | 시험 스크립트 수정 뒤 다시 돌리지 않았다(사용자 판정 정상) | 메뉴 8 | 사용자 |
| I09 `snapshot_left` 표시 | 수정분(D-VM-03)을 실기로 다시 보지 않았다(단위 시험 통과, 사용자 판정 정상) | I09 다시 | 사용자 |
| L01 (V4) | 운영 크기 VM | 운영 VM 하나로 gzip·zstd 백업 시간 측정 | 사용자 |
| U60 `send_progress` | 진행 중 결과 보고는 middle 이 정해지지 않아 시험하지 않았다. 코드는 같은 outbox 경로를 쓴다 | middle 연동 때 | 수행자 |
| Linux 실행 파일 기동 | 단위 시험은 Linux 에서 통과했지만 `vbr_vm` 실행 파일을 Linux 에서 띄워 보지 않았다 | Linux 서버 단계 | 사용자·수행자 |

## 7. 사용자 실행 시험의 절차

T-VM-I01~I10, L01은 9장의 demo.ps1 실기 모드로 한다. 절차는 [06-windows-test-guide.md](../00-process/06-windows-test-guide.md) 6장에 있다(메뉴가 없는 I09·I10은 6.3).

## 8. 알려진 한계

- 가짜 XAPI는 호출 모양과 순서만 흉내 낸다. 실제 디스크 데이터·압축 효율은 V3에서만 확인한다.
- 관리 서버·middle과의 실제 연동은 둘 다 없어 하지 않는다(지시 9). 결과 보고는 httptest 수신기와 demo.ps1의 간이 수신기로만 확인한다.

## 9. 데모·시험 스크립트

| 항목 | 내용 |
|---|---|
| 위치 | `go/test/vm/demo.ps1`, `go/test/vm/demo_modules/`(ncc-xen 구조, CP-VM-13) |
| 대상 | ① 자동 모드: 가짜 XAPI와 `vbr_vm`을 스크립트가 띄우고 시나리오를 돌려 PASS/FAIL 집계 ② 실기 모드: 사용자가 마스터 주소·VM·SR을 넣고 메뉴로 실행 |
| 입력 | 서비스 주소, 토큰, XAPI 암호는 실행 때 입력(`Read-Host -AsSecureString`)하거나 환경 변수. 암호는 스크립트가 `vbr_secret encrypt`로 ENC 값으로 바꿔 보낸다(BOM 문제는 1단계에서 고침). 화면·파일에 평문을 남기지 않는다 |
| 출력 | 단계마다 `PASS`/`FAIL` + 근거 한 줄(예: `status=succeeded file_size_bytes=2588192829`). FAIL은 응답 원문 |
| 정리 | 실기 모드는 시험이 만든 VM·스냅샷 uuid를 모아 두었다가 마지막 메뉴에서 "uuid·이름 확인 → 지우기"를 한다. 시험 VM과 기존 스냅샷은 목록에서 빼고 지우지 않는다 |

| 메뉴 | 시나리오 | 판정 | 시험 번호 |
|---|---|---|---|
| 1 | 서비스 상태(/health), 토큰 확인 | 자동 | U70, U80 |
| 2 | 전체 백업(압축 선택) → 진행률 조회 → 결과 | 자동(상태·필드) + 수동(크기) | I01 |
| 3 | 디스크 선택 백업 | 같음 | I02 |
| 4 | 복구(MAC 보존 선택) | 자동(vm_uuid) + dom0 확인 안내 | I03, I04 |
| 5 | 검증 | 자동(verified) | I05 |
| 6 | 동시 작업과 목록 조회 | 자동 | I06 |
| 7 | 처리 중 취소 | 자동(canceled) + dom0 확인 안내 | I07 |
| 8 | 오류 응답(없는 VM·SR, 틀린 암호, 필수 값 없음) | 자동(코드) | I08, U02 |
| 9 | 정리 | 수동 확인 뒤 | - |
| A | 자동 모드 전체 | 자동 | U 계열 대표 |
