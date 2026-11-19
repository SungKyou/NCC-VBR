# agent 시험 계획과 결과

기준일: 2026-11-17 / 양식: T06 / 대상: 전환본 `vbr_agent` 3.000(기준 원본 1.602) / 판정 표현: [02-verification-procedure.md](../00-process/02-verification-procedure.md)

## 1. 판정 요약

| 수준 | 내용 | 결과 | 증거 |
|---|---|---|---|
| V0 정적 검사 | `gofmt -l .`, `go vet ./...`(Windows, `GOOS=linux`) | 통과. 지적 없음 | `evidence/agent/unit-windows.txt` 앞부분 |
| V1 단위 시험 Windows | `go test -count=1 -v ./...` (전체 저장소) | 통과 43, 건너뜀 4, 실패 0 | `evidence/agent/unit-windows.txt` |
| V1 단위 시험 Linux | `go/test/run-linux-tests.sh` (WSL Ubuntu) | 통과 46, 건너뜀 1, 실패 0. agent 시험만 10회 반복해도 통과 | `evidence/agent/unit-linux.txt` |
| V2 원본 비교 | 원본 C 1.602(소스 수정 없이 컴파일) + 가짜 `xe` 대 전환본 + 가짜 XAPI, 같은 요청 순서 | **같음 96, 의도한 차이 22, 다름 0**. 다른 접속으로 온 응답 0 | `evidence/agent/parity-final.txt` |
| V2 회귀 | 공통 `logx` 를 바꿨으므로 cifs·remove 원본 비교를 다시 실행 | 통과(`RESULT: ALL PASS`) | `evidence/parity-final-rerun-agent.txt` |
| 통합 뒤 재실행 (2026-11-18) | `c:\dev\ncc-vbr-go` 에서 V0~V2 전체 | Windows 통과 46·건너뜀 4, Linux 통과 49·건너뜀 1(agent 10회 반복 통과), 원본 비교 **같음 97, 의도한 차이 21, 다름 0**. 차이 1건 감소는 C-AG-03(전송 시점) 1건이 이번에는 같았기 때문 | `evidence/merge/`, [05-merge-record.md](../00-process/05-merge-record.md) |
| V3 통합 (실기 일부) | 실제 XenServer 9 풀(192.168.102.53), agent 는 Windows PC 에서 원격 실행, 요청은 시험 스크립트(`go/test/agent/vbr-send.ps1`) | **2026-11-18~19 사용자 실행: IT-01~05, PT-01, PT-02 통과.** 원본 VM 이 있는 풀에 MAC 보존 복구는 DUPLICATE_MAC_SEED 로 거부(원본과 같다고 봄). 5.3절 | `evidence/agent/windows-real-20261119/` |
| V3 통합 (남은 것) | Linux agent 서버, 실제 관리 서버, `/BACKUP` NAS | **미실행**. IT-06(관리 서버 연동), IT-07(허용 주소 밖 접속), PT-03, PT-04 | - |
| V4 운영 규모 | 큰 VM 의 원격 내보내기 시간, 동시 백업 수 | **미실행**. 5장 PT-03 | - |

판정: **개발 검증 통과**(V0~V2), **실기 백업·복구 동작 확인**(V3 중 IT-01~05·PT-01·PT-02). 운영 전환 가능 판정은 남은 V3(IT-06·07, Linux 서버)과 V4 뒤에 한다.

시험할 수 없었던 것: 실제 XAPI 의 `/export`·`/import` 동작(가짜 서버는 ncc-xen 코드가 쓰는 호출 모양만 흉내 낸다), 실제 관리 서버(ASP)의 응답 해석, 수 GB 파일 전송.

## 2. 단위 시험 (V1)

| 번호 | 시험 함수 | 확인하는 것 | 관련 |
|---|---|---|---|
| T-AG-U01 | TestMessageFrame | 전문 머리 해석, 응답·되돌림 모양, 접수 응답 길이 25 | 05 문서 3장, C-AG-04 |
| T-AG-U02 | TestTranLogFormatAndQuery | transaction 로그 한 건의 바이트 모양(운영 표본과 같은 머리 61자), 결과 조회가 마지막 기록을 읽는 것, 없는 번호·없는 달 | K-AG-01, K-AG-12 |
| T-AG-U03 | TestAsyncBackupFlow | 접수 → 끊김 → 조회. 단계 기록 1·2·3·5·0 과 "send error"(운영 로그와 같은 순서) | K-AG-03 |
| T-AG-U04 | TestAsyncResultOnOpenConnection | 접속을 유지하면 결과를 같은 접속으로 받고 "send success" | C-AG-03 |
| T-AG-U05 | TestQueryProcessingAndNotFound | 처리 중 3 + 요청 본문, 없음 9 + 번호, 번호 길이 오류 | K-AG-01, 2015 표본 |
| T-AG-U06 | TestCancel | 처리 중 거절, 대기 중 취소(문장의 번호는 요청 자신), 두 번째 취소는 not found, 없는 번호, 일괄 취소, 취소된 작업 조회는 C | K-AG-04~07, C-AG-26 |
| T-AG-U07 | TestRejects | 형식 오류 되돌림, `S`·`I` 거절, 모르는 작업 구분, 대기열 100칸 가득 → 8 | C-AG-05, 11~13 |
| T-AG-U08 | TestArgumentErrors | 인자 오류 문장 8종, 백업 루트 밖·`..` 거부, 셸 명령 거절, 정상 복구 인자 | K-AG-09, C-AG-14·17 |
| T-AG-U09 | TestBackupFailureResult | 실패 결과의 코드 9 기록, 조회 코드는 0(원본과 같음) | K-AG-01 |
| T-AG-U10 | TestPriorityOrder | 우선순위 0 → 5(도착 순) → 9 | K-AG-08 |
| T-AG-U11 | TestRestartRecovery | 처리 중 종료 → 재기동 때 6 기록, 대기 중이던 작업은 이어서 처리 | C-AG-20 |
| T-AG-U12 | TestAllowClient | 허용 주소 밖의 접속은 끊는다 | C-AG-18 |
| T-AG-U13 | TestShortMessage | 길이보다 짧은 전문은 6초 뒤 끊는다 | C-AG-22 |
| T-AG-U20 | TestXenBackup | 가짜 XAPI 로 백업: 호출 순서, 스냅샷 VDI 삭제 뒤 VM.destroy, `.partial` 정리, backup.log 단계 문구 | C-AG-07·08·09, K-AG-11 |
| T-AG-U21 | TestXenPartialBackup | 자리 값 0 인 디스크를 내보내기 전에 owner=false + VDI 삭제, 정리 때 두 번 지우지 않음 | C-AG-08, D-AG-04 |
| T-AG-U22 | TestXenExportFailure | 내보내기 실패 때 파일을 남기지 않고 스냅샷 정리 | C-AG-08·09 |
| T-AG-U23 | TestXenSnapshotFailure | 스냅샷 실패 문장 `[ERROR]XAPI Error: …`, 정리할 것 없음 | C-AG-21 |
| T-AG-U24 | TestXenRestore | preserve → `restore=true`, `sr_id`, 결과 VM uuid, 템플릿 플래그 후처리, backup.log | C-AG-25 |
| T-SEC-U01~03 | secret 패키지 | 암호화·복호화, 용도 문자열 불일치 실패, 모르는 필드 거부, 키링 권한 077 거부(Linux만, Windows 는 건너뜀) | Q-A14, CP-10 |

시험 중 고친 것

| 일시 | 증상 | 원인 | 조치 |
|---|---|---|---|
| 2026-11-17 | 머리 줄 길이, `Q0001` 응답 머리 기대값 불일치 | 시험 기대값을 잘못 적음 | 시험 수정 |
| 2026-11-17 | 같은 작업 두 번째 취소가 not found | 원본도 같다(K-AG-07) | 시험 기대값을 원본 동작으로 |
| 2026-11-17 | 가짜 XAPI 의 복구 결과를 못 읽음 | 가짜 참조값 `OpaqueRef:newvm` 이 실제 형식(16진수)이 아님 | 가짜 값 수정 |
| 2026-11-18 | Windows 실기 R1 에서 XAPI 인증 실패(`SESSION_AUTHENTICATION_FAILED`). dom0 직접 로그인은 성공 | PowerShell `$OutputEncoding = UTF8` 파이프가 `vbr_secret encrypt` 입력 앞에 BOM(EF BB BF)을 붙여 암호에 섞였다 | `vbr_secret` 이 첫 줄의 BOM 을 뗀다. `TestReadPassword` 추가, 재빌드·재암호화 뒤 로그인 성공 |
| 2026-11-17 | Linux 에서 T-AG-U03 한 번 실패 | 가짜 백업이 즉시 끝나 관리 서버가 끊기 전에 결과가 써졌다(시험만의 경합. 원본에도 같은 경합이 있다) | 가짜 백업에 200ms 지연. 10회 반복 통과 |

## 3. 원본 비교 시험 (V2)

### 3.1 환경

`go/test/agent-parity/` (Dockerfile, run.sh, fake-bin, fakexapi, driver)

| 쪽 | 구성 | 수신 |
|---|---|---|
| 원본 | alpine:3.20 에서 `vbr_agent.c`·`dummy2.c`·`bs_common.c` 를 소스 수정 없이 컴파일(`-fcommon`, 스레드 스택 8MB 만 더함). 가짜 `xe`·`sar`, `/etc/xensource-inventory`. 상태 감시가 Active 여야 비동기 작업을 하므로 Mount Point·CPU Limit 설정을 넣었다 | 7101 |
| 전환본 | `vbr_agent` + 가짜 XAPI 서버(TLS, 8443). 암호는 `vbr_secret`으로 암호화 | 7102 |
| 시험 자료 | 두 쪽이 같은 VM uuid, 같은 스냅샷 uuid 규칙(이름의 16진수), 같은 복구 VM uuid, 같은 내보내기 지연(이름 `slow` = 12초)을 낸다. 내보내기 파일은 원본 1.602 의 XVA 검증을 통과하는 최소 XVA | - |

### 3.2 시나리오

| 번호 | 내용 |
|---|---|
| P00 | 백업 1건(원본 상태 감시가 Active 가 될 때까지 기다리는 용도 겸) → 접수 응답, 최종 조회 |
| P01 | 형식 오류(`X`로 시작) |
| P02, P03 | 백업 인자 오류(VM uuid), 복구 인자 오류(2015 표본과 같은 빈 칸 두 개) |
| P04 | 느린 백업 5건(작업자 3개라 2건은 대기) → 처리 중 조회, 대기 중 조회, 처리 중 취소, 대기 중 취소, 취소된 작업 조회, 없는 번호·짧은 번호 조회와 취소, 최종 결과 4건 |
| P05, P06 | 부분 백업(디스크 0만), 복구(preserve true) |
| P07 | 일괄 취소 |
| 기록 비교 | 전문 번호별 transaction 로그의 단계·문구·본문 전부, backup.log 의 S·P·E·I 단계 문구 |

대기열 가득(8)은 원본에서 오래 걸리고 불안정해(4차 실행에서 원본이 87번째 요청 부근에서 끝남, D-AG-11) 단위 시험 T-AG-U07 로만 확인했다.

### 3.3 의도한 차이(EXPECTED 22건)

| 코드 | 건수 | 내용 | 변경 대장 |
|---|---|---|---|
| C-AG-01 | 12 (응답 6, 기록 6) | 백업 성공 결과 문장. 원본 `XVA file check Success : …` / 전환본 `VM BACKUP Success : …`. 파일 이름과 전문 머리는 같다 | C-AG-16 |
| C-AG-02 | 1 | 원본 형식 오류 기록 뒤에 이전 버퍼 내용이 붙음 | C-AG-02, D-AG-05 |
| C-AG-03 | 9 | 비동기 결과 송신 기록. 원본 "ASYNC RESULT send success" / 전환본 "send error". 본문은 같다(C-AG-01 제외) | C-AG-03, D-AG-01 |

C-AG-03 은 원본 결함의 재현이다. 시험 프로그램은 요청마다 접속을 새로 열고 닫는데, 원본은 결과를 끊긴 접속의 fd 번호로 썼고, 그 번호는 이미 다음 조회 접속이 받아 쓰고 있었다. 운영에서는 같은 일이 관리 서버의 다른 요청 접속에서 생길 수 있다.

### 3.4 실행 기록

| 차수 | 결과 | 원인 | 조치 | 증거 |
|---|---|---|---|---|
| 1 | 원본이 뜨지 않음 | 원본 공통 라이브러리가 설정 `PROGRAM` 을 요구 | 시험 설정에 추가 | `evidence/agent/parity-run-1.txt` |
| 2 | 원본이 "already running" 으로 끝남 | 원본 중복 실행 검사(`ps \| grep vbr_agent`)가 전환본 준비 명령(`… vbr_agent.keyring`)을 셌다(D-AG-06) | 원본을 먼저 완전히 띄우고 키링 이름을 바꿈 | `parity-run-2.txt` |
| 3 | 다름 130 | 일괄 취소 결과 코드(전환본 결함, `C` 로 고침), 원본 결함 2건, 원본의 1초 간격 처리로 시간이 어긋남 | 전환본 수정, 의도한 차이 규칙 추가 | `parity-run-3.txt` |
| 4 | 다름 128 | 원본의 응답이 1초 간격이라 "처리 중" 시점이 어긋남, 대기열 가득 단계에서 원본 종료(D-AG-11) | 지연을 12초로, 대기열 가득 단계를 비교에서 뺌, 다른 접속으로 온 응답을 따로 셈 | `parity-run-4.txt` |
| 5 | 다름 1 | 조회를 되풀이할 때 같은 전문 번호를 다시 써서 원본이 앞 접속으로 응답 | 관리 서버처럼 요청마다 새 번호 | `parity-run-5.txt` |
| 최종 | **같음 96, 의도한 차이 22, 다름 0** | - | - | `parity-final.txt` |

## 4. 추적표 (요건 → 원본 → 전환본 → 시험)

| 요건 | 원본 | 전환본 | 시험 |
|---|---|---|---|
| 전문 틀과 응답 | 796~873, 1124~1144행 | `agent/frame.go`, `agent.go handle` | U01, U07, P01 |
| 비동기 백업 | 3784~3981행 | `worker.go backup`, `xen.go Backup`, `transfer.go export` | U03, U08, U20, U22, U23, P00, P04 |
| 부분 백업 | 3983~4115행 | `xen.go partialRemove` | U21, P05 |
| 복구 | 4119~4185행 | `worker.go restore`, `transfer.go importFile` | U08, U24, P03, P06 |
| 결과 조회 | 3376~3484행 | `tranlog.go Query`, `agent.go query` | U02, U05, U09, P04 |
| 취소 | 2918~3108행 | `agent.go cancel`, `cancelOne` | U06, P04, P07 |
| 우선순위·대기열 | 826, 1047~1152, 3608~3782행 | `registry.go` | U07, U10 |
| 재기동 복구 | 564~586행 | `registry.go Restore` | U11 |
| 로그 3종 | `bs_common.c` BACKUP_LOG, IMPORT_LOG, TRAN_LOG2 | `tranlog.go`, `steplog.go`, `logx` | U02, U20, U24, P 전체 |
| 암호 보관 | (없음) | `secret`, `cmd/vbr_secret` | T-SEC-U01~03, 비교 환경 기동. 2026-11-18 `TestReadPassword`(줄 끝 CR·LF, PowerShell UTF-8 BOM 제거, 빈 암호 거부) 추가: Windows 실기 시험에서 `$OutputEncoding = [Text.Encoding]::UTF8` 상태로 파이프 입력한 암호 앞에 BOM이 붙어 XAPI 인증이 실패할 수 있음을 개발 PC에서 재현했다 |

## 5. 통합 시험 계획 (V3, V4) — 사용자 실행

시험 풀: XenServer 9, 192.168.102.53. agent 서버는 풀 마스터의 443 포트에 닿아야 하고 `/BACKUP` 에 운영과 같은 NAS 를 마운트한다.

### 5.1 먼저 확인할 것

| 번호 | 확인 | 방법 | 판정 기준 |
|---|---|---|---|
| PT-01 | 스냅샷 정리 뒤 디스크가 남지 않는가 | 시험 VM 하나로 BACKUP 1건 → `xe vdi-list is-a-snapshot=true` 에 그 스냅샷의 VDI 가 없어야 한다 | 남은 VDI 0 |
| PT-02 | 복구 preserve | 같은 XVA 를 `true`·`false` 로 RESTORE → `xe vif-list vm-uuid=<새 VM> params=MAC` 를 원본 VM 과 비교 | true 면 MAC 같음, false 면 다름 |
| PT-03 | 원격 내보내기 시간 | 운영 크기 VM 1대를 원본 agent(dom0)와 전환본(원격)으로 각각 백업, `backup.log`·`server.log` 의 시각으로 비교 | 판정 조건 아님(DEC-AG-06). 시간을 기록한다 |
| PT-04 | 운영 판 | 운영 호스트에서 `./vbr_agent -version` | 1.602 인지 기록 |

### 5.2 통합 시나리오

| 번호 | 요청(관리 서버 또는 시험 프로그램) | 기대 |
|---|---|---|
| IT-01 | BACKUP | 접수 OK → 조회 3 → 조회 0 + `VM BACKUP Success …`. `/BACKUP/…xva` 생성, 권한 666 |
| IT-02 | PARTIAL(디스크 2개 VM, 자리 `100000000000000`) | XVA 를 복구했을 때 디스크 0만 있다 |
| IT-03 | RESTORE(preserve false) | 조회 0 + 새 VM uuid. XenCenter 에서 시작할 수 있다(템플릿 아님) |
| IT-04 | 대기 중 취소, 처리 중 취소 | 원본과 같은 문장(06 문서 2장 U06) |
| IT-05 | 백업 중 agent 종료(`kill -TERM`) → 재기동 | 그 작업 조회가 6. XenServer 에 남은 task·스냅샷 확인(정리는 Q-A17 범위 밖) |
| IT-06 | 관리 서버 실제 연동 | 관리 서버 화면에서 백업·복구 결과가 정상 표시 |
| IT-07 | 허용 주소 밖에서 접속 | 접속이 끊긴다 |

증거: 각 시나리오의 `transaction-YYYYMM.log`, `backup.log`, `server.log` 사본과 `xe` 확인 출력.

### 5.3 실기 시험 기록 (Windows PC → XenServer 9, 2026-11-18~19)

절차: [06-windows-test-guide.md](../00-process/06-windows-test-guide.md) 4장 R0~R9. 사용자가 PC 명령과 dom0 `xe` 명령을 실행하고 출력을 전달했다. 전환 수행자는 풀에 접속하지 않았다.

| 항목 | 값 |
|---|---|
| agent | `go/bin/windows-amd64/vbr_agent.exe` 3.000, 작업 폴더 `C:\temp\vbr`, 포트 7102, `Async Thread = 2`, `Allow Client = 127.0.0.1` |
| 풀 | XenServer 9, 마스터 192.168.102.53(xenserver9-3), PC 10.10.3.75 에서 443 접속 |
| 시험 VM | `42097417-…` `copy_20gb2`, halted. 디스크 0 20 GiB(`b5adc735-…`), 디스크 1 1 GiB(`b2408a84-…`). 기존 스냅샷 `926b6f3f-…` 1개 |
| SR | `ASUS NFS` `e4138437-…` |
| 증거 | `evidence/agent/windows-real-20261119/` 의 `server.log`, `backup.log`, `transaction-test-requests.log`(시험 요청 번호 `202611182000…`만 추림. `Wait-Vbr` 반복 조회 기록 약 11,400건은 뺐다) |

| 번호 | 시나리오 | 결과 | 판정 |
|---|---|---|---|
| R1 | 준비, 암호 암호화, agent 기동 | 첫 시도 `SESSION_AUTHENTICATION_FAILED`. 원인은 PowerShell 파이프가 암호 앞에 붙인 UTF-8 BOM. `vbr_secret` 수정(2장 시험 중 고친 것, `TestReadPassword`) 뒤 재암호화, `XAPI login success` | 통과(수정 뒤) |
| R2 IT-01 | 전체 백업 | 코드 0 `VM BACKUP Success`, `full.xva` 2,588,192,829바이트, 4분 28초. 처리 중 조회 3. backup.log 단계 4개 | 통과 |
| PT-01 | 스냅샷 정리 | 임시 스냅샷 `547e3b2d-…` 없음. 스냅샷은 기존 `926b6f3f-…` 하나, 디스크마다 스냅샷 VDI 는 기존 것 1개(`7e0df078-…`, `4e865ac2-…`, 스냅샷 시각 2026-09-30). 남은 export task 없음 | 통과 |
| R3 IT-03 | 복구 preserve=false | 코드 0, NEW1 `23fe6b44-…` `vbrtest-full`, halted, 템플릿 아님, 디스크 0·1, MAC `ca:da:99:4c:22:0f`(원본 `5a:f2:8a:47:97:a1` 과 다름) | 통과 |
| R4 | 원본 VM 이 있는 풀에 preserve=true | 결과 전문 코드 9, 조회 코드 0(K-AG-01), 본문 `[ERROR]XAPI task failure: DUPLICATE_MAC_SEED: de99e29a-…`(원본 VM 의 mac_seed). 남은 VM 없음, backup.log 에 `VM import failed` | 실패 처리 정상. 운영 주의(07 문서) |
| PT-02 | MAC 보존 | NEW1 백업(`new1.xva`, 4분 26초) → NEW1 삭제 → preserve=true 복구 → NEW2 `930aefc6-…` MAC `ca:da:99:4c:22:0f`(NEW1 과 같음), 템플릿·스냅샷 아님, 디스크 0·1. NEW2 uuid 는 백업 때의 스냅샷 uuid 와 같다 | 통과 |
| R5 IT-02 | 부분 백업(디스크 0만) + 복구 false | 코드 0, `part.xva` 2,588,140,361바이트(디스크 1 은 거의 빈 디스크), 4분 24초. NEW3 `b9bda7ee-…` 디스크 0 하나, 20 GiB, VHD 5,984,395,776바이트(sr-scan 뒤. 직후에는 45,056 으로 보였다). 임시 스냅샷 `699ddfb1-…` 와 그 VDI 없음 | 통과 |
| R6 IT-04 | 작업자 2개에 백업 3개, 조회·취소 | 11번 조회 3, 13번 조회 2, 13번 취소 0 `Request message is cancel.[20261118200000022]`, 11번 취소 9 `Request message is processing.`, 13번 조회 C. 11·12번 동시에 시작해 코드 0(4분 32~33초), `c13.xva` 없음, 13번 transaction 은 1·2·C 단계만. 남은 스냅샷·task 없음 | 통과 |
| R7 IT-05 | 백업 중 강제 종료(`Stop-Process -Force`) → 재기동 | 종료 전 조회 3. 재기동 때 `[20261118200000030] Task cancel.   Please check result.`, `Restart recovery : canceled [1], requeued [0]`, 조회 6, transaction 6 단계 `Task cancel(mode 1)`. `kill.xva.partial` 168,558,592바이트만 남고 `kill.xva` 없음. dom0 에 `vbrtest-kill` 스냅샷과 `failure` export task 기록이 남음(Q-A17 범위 밖, R9 에서 스냅샷 삭제) | 통과 |
| R8 | 실제 XAPI 오류 | 결과 전문 코드 9, 조회 코드 0(K-AG-01). 없는 VM 백업: `[ERROR]XAPI Error: UUID_INVALID \| VM \| 0000…`. 없는 SR 복구: `[ERROR]XAPI Error: UUID_INVALID \| SR \| 0000…`, 파일 전송 전에 1초 안에 실패. `x.xva`·`.partial` 없음 | 통과 |
| R9 | 정리 | NEW2·NEW3·`vbrtest-kill` 삭제. 남은 것: 시험 VM(halted), 기존 스냅샷 `926b6f3f-…`, 기존 스냅샷 VDI 2개, R7 의 `failure` task 기록. PC 백업 파일 삭제, 로그는 보존 | 완료 |

이번 실기에서 확인한 것

- `server.log` 의 `Send to REQUEST Manager internal error[번호]` 는 결과를 보낼 접속이 이미 닫혀서 나온다. 시험 스크립트는 요청마다 접속을 닫으므로 매번 나오며 결과는 transaction 로그에 남아 조회로 받는다(K-AG-03, C-AG-03 과 같은 경로).
- MAC 보존 복구(preserve=true)는 파일 속 VM uuid 도 보존한다. 백업은 스냅샷을 내보내므로 복구 VM 의 uuid 는 백업 때의 스냅샷 uuid 다. 같은 파일을 두 번 preserve=true 로 복구하는 경우는 실행하지 않았다(uuid 중복으로 실패할 것으로 본다).
- 같은 VM 을 두 작업이 동시에 스냅샷·내보내기해도 둘 다 성공했다.
- 백업 크기·시간은 VDI 의 `physical-utilisation` 으로 가늠할 수 없다(스냅샷 체인이 있으면 맨 끝 VHD 만 센다). 이 VM(체인 데이터 약 5.6 GiB)은 압축 2.59 GB, 약 4분 30초.
- 원본 C agent 로 같은 풀에서 비교 실행하지는 않았다. "원본과 같다" 는 원본 소스(`vbr_agent.c` 4154~4163 의 `xe vm-import … preserve=…`, force 없음)와 XAPI 동작에 근거한 판단이다.
