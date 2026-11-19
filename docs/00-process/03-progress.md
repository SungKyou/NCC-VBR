# 진행 현황판

기준일: 2026-11-18

2026-11-18부터 작업 위치는 `c:\dev\ncc-vbr-go`(세 서비스 통합)이고, 모든 변경은 Claude Code가 한다. 통합 경위는 [05-merge-record.md](05-merge-record.md)에 있다.

## 1. 대상과 순서

사용자가 정한 순서는 cifs → remove → agent다. 세 서비스는 서로 독립이다.

| 순서 | 서비스 | 원본 파일 | 줄 수 | 빌드 스크립트 | 실행 형식 |
|---|---|---|---|---|---|
| 1 | cifs | `cifs/vbr_cifs.c`, `bs_common.c`, `bs_common.h` | 1,250 + 1,501 + 116 | `cifs/c1` | `vbr_cifs source-dir target-dir seq-no` |
| 2 | remove | `remove/vbr_remove.c`, `bs_common.c`, `bs_common.h` | 721 + 1,506 + 122 | `remove/c1` | `vbr_remove delete-dir days` |
| 3 | agent | `agent/vbr_agent.c`, `dummy2.c`, `bs_common.c`, `bs_common.h` | 5,275 + 205 + 1,448 + 108 | `agent/c1.sh` | `vbr_agent` (상주 데몬, TCP 수신) |

설정 파일은 `node_info.cfg` 하나다. 개발 환경 값이며, 서비스가 읽는 항목이 파일에 없으면 있는 것으로 가정한다(사용자 지시).

## 2. 환경

| 항목 | 값 | 확인 방법 |
|---|---|---|
| 원본 운영 환경 | CentOS Linux | 사용자 설명 |
| 개발 PC | Windows 11 Pro | 실행 환경 |
| Go | 1.26.0 windows/amd64 | `go version` (2026-11-17) |
| Linux 시험 환경 | WSL2 Ubuntu, 커널 6.6.87 | `uname -r` (2026-11-17) |
| 원본 컴파일 환경 | Docker의 alpine:3.20 + gcc | `go/test/parity/Dockerfile` |
| 대상 운영체제 | Linux 우선, Windows는 가능한 범위에서 호환 | 사용자 지시 |

개발 PC의 WSL에는 gcc와 Go가 없다. 그래서 원본 컴파일은 컨테이너에서 하고, Linux 단위 시험은 Windows에서 만든 Linux용 실행 파일을 WSL에서 돌린다.

## 3. 단계별 현황

단계 번호는 [01-migration-process.md](01-migration-process.md)의 번호다.

| 단계 | cifs | remove | agent | 공통 |
|---|---|---|---|---|
| 0 착수 | 완료 | 완료 | 완료 | 완료 |
| 1 현행 분석 | 완료(전체 읽음) | 완료(전체 읽음) | 유지 범위 전체 읽음(재구성 뒤, 아래 4장) | 완료(전체 읽음) |
| 2 요건 정의 | 완료 | 완료 | 재구성 검토서(`agent/00`), 전문 명세 확정(`agent/05` 9장) | - |
| 관문 A 분석 검토 | 사용자 검토 대기 | 사용자 검토 대기 | 사용자 검토 대기 | 사용자 검토 대기 |
| 3 목표 설계 | 완료 | 완료 | 완료(`agent/00` 3~5장) | 완료(복사 규칙 추가) |
| 관문 B 변경 승인 | 결정 대기 5건(Q-12 해결) | 결정 대기 2건 | 결정 대기 2건(Q-A18, A19) | - |
| 4 구현 | 완료(2.068 `Full Scan Day`, `dis_` 보호 포함) | 완료(보존 일수 상한, 파일 이름 기준 판단 포함) | 완료(BACKUP, PARTIAL, RESTORE, Q, C) | agent 범위(`xapi`, `secret`, `logx.Append`) 완료. 2026-11-18 통합 |
| 5 검증 V0 정적 검사 | 통과 | 통과 | 통과 | 통과 |
| 5 검증 V1 단위 시험 | 통과(Windows, Linux) | 통과(Windows, Linux) | 통과(Windows, Linux, 18건 + secret 3건) | 통과 |
| 5 검증 V2 원본 비교 | 통과(33개 항목, 기준 2.0672와 2.068 재구성판) | 통과(9개 항목) | 통과(통합 뒤 같음 97, 의도한 차이 21, 다름 0) | - |
| 5 검증 V3 통합 | 미실행(실제 NAS·포털 필요) | 미실행(실제 NAS 필요) | 미실행(시험 풀·관리 서버 필요, `agent/06` 5장) | - |
| 5 검증 V4 운영 규모 | 미실행(운영 규모 자료 필요) | 미실행(운영 규모 자료 필요) | 미실행(PT-03 원격 내보내기 시간) | - |
| 관문 C 판정 | 개발 검증 통과 | 개발 검증 통과 | 개발 검증 통과 | - |
| 6 전환 준비 | 절차서 초안 작성. 시험 환경 실행 전 | 절차서 초안 작성. 시험 환경 실행 전 | 절차서 초안(`agent/07`). 시험 풀 실행 전 | - |
| 7 전환 | 미착수 | 미착수 | - | - |

관문 A와 B는 이번 작업에서 사용자 승인 없이 지나갔다. 사용자가 "항목이 없으면 있는 것으로 가정"하고 진행하도록 지시했기 때문이다. 승인이 필요한 항목은 원본 동작에 가깝고 자료 손실 위험이 작은 쪽으로 구현했고 [04-open-decisions.md](04-open-decisions.md)에 남겼다.

## 4. agent의 분석 범위

agent는 구현 전에 사용자에게 문제를 먼저 알리도록 지시받았다. 아래 표는 첫 사전 분석 때의 기록이다. 2026-11-17 재구성(agentless, 백업·복구 중심, 조회 후순위) 뒤 유지 범위를 전체 읽었다: 1~591, 592~873, 1047~1152(수신·등록), 1368~1558(관리 작업자), 1560~2467(조회, 재구성 판단용), 2862~3108(취소), 3376~3484(결과 조회), 3485~3782(작업자), 3784~4213(백업·복구), 4216~4436(송신·일련번호), 4667~4713(dummy_call), 4960~5056(XVA 검증의 판정 조건), `bs_common.c` 90~140·523~800. 읽지 않은 범위는 상태 감시 세부(875~1045 는 읽음), `thread_task`(3110~3375), 이벤트·포털(4437~4666), XVA 비압축 검증(5059~5230)으로, 모두 옮기지 않는 기능이다.

| 원본 범위 | 줄 | 읽은 수준 |
|---|---|---|
| 자료 구조, 전역 변수, `main`, `env_initialize` | 1~591 | 전체 읽음 |
| `RealTimeMain`, `MessageProcessing` (수신과 전문 등록) | 592~873 | 전체 읽음 |
| `health_check_processing` (상태 감시) | 875~1045 | 전체 읽음 |
| 수신·송신 스레드, `monitor` | 1047~1367 | 명령만 추출 |
| `mgt_processing` (조회·취소·스레드 관리) | 1368~3484 | 명령만 추출 |
| `sync_processing`, `async_processing` | 3485~3783 | 명령만 추출 |
| `backup_vm` (백업) | 3784~3981 | 전체 읽음 |
| `partial_backup_remove`, `import_vm`, 송신 스레드 | 3983~4422 | 명령만 추출 |
| 신호, 이벤트, 포털, `dummy_call` | 4423~4762 | 전체 읽음 |
| XVA 파일 검증 | 4763~5275 | 함수 목록만 확인 |
| `dummy2.c` | 1~205 | 전체 읽음 |

"명령만 추출"한 범위는 줄 단위로 읽지 않았다. agent 전환 1단계의 첫 작업이 이 범위를 전체 읽는 것이다.

## 5. 검증 실행 기록

| 일시 | 수준 | 명령 | 결과 | 증거 |
|---|---|---|---|---|
| 2026-11-17 | V2 1차 | `docker run ncc-vbr-parity` | 실패. 원본이 시험 환경에서 멈춤(환경 문제) | `evidence/parity-run-1.txt` |
| 2026-11-17 | V2 2차 | 같음 | 실패 4건. 모두 시험 스크립트 문제 | `evidence/parity-run-2.txt` |
| 2026-11-17 | V2 3차 | 같음 | 통과 22, 실패 0 | 보관하지 않음(최종 실행으로 대체) |
| 2026-11-17 | V2 4차 | 같음(이어받기 시험 6개 추가) | 통과 28, 실패 0 | 보관하지 않음(최종 실행으로 대체) |
| 2026-11-17 | 소스 변경 | remove의 보존 일수 검사를 백그라운드 분리 전으로 옮김(`boot.Spec.Check`) | 아래 네 줄은 이 변경 뒤에 다시 실행한 결과다 | - |
| 2026-11-17 | V0 | `gofmt -l .`, `go vet ./...` (Windows, Linux 대상) | 통과. 지적 없음 | `evidence/unit-windows.txt` 앞부분 |
| 2026-11-17 | V1 Windows | `go test -count=1 -v ./...` | 통과 23, 건너뜀 3, 실패 0 | `evidence/unit-windows.txt` |
| 2026-11-17 | V1 Linux | `go/test/run-linux-tests.sh` (WSL Ubuntu) | 통과 25, 건너뜀 1, 실패 0 | `evidence/unit-linux.txt` |
| 2026-11-17 | V2 최종 | `docker run ncc-vbr-parity` | 통과 28, 실패 0 | `evidence/parity-final.txt` |
| 2026-11-17 | 명령행 | Windows 실행 파일 수동 실행 | 통과 | `evidence/cli-windows.txt` |
| 2026-11-17 | agent V2 1~5차 | `docker run ncc-vbr-agent-parity` | 실패. 시험 환경·시험 스크립트 문제와 전환본 결함 1건(일괄 취소 코드) | `evidence/agent/parity-run-1~5.txt`, 원인은 `agent/06` 3.4 |
| 2026-11-17 | 소스 변경 | agent 취소의 상태 재확인(K-AG-13) | 아래 세 줄은 이 변경 뒤에 다시 실행한 결과다 | - |
| 2026-11-17 | V0·V1 Windows (전체) | `gofmt -l .`, `go vet`, `go test -count=1 -v ./...` | 통과 43, 건너뜀 4, 실패 0 | `evidence/agent/unit-windows.txt` |
| 2026-11-17 | V1 Linux (전체) | `go/test/run-linux-tests.sh` + agent 10회 반복 | 통과 46, 건너뜀 1, 실패 0 | `evidence/agent/unit-linux.txt` |
| 2026-11-17 | agent V2 최종 | `docker run ncc-vbr-agent-parity` | 같음 96, 의도한 차이 22, 다름 0 | `evidence/agent/parity-final.txt` |
| 2026-11-17 | cifs·remove V2 재실행 | `docker run ncc-vbr-parity` (공통 logx 변경 뒤) | 통과 | `evidence/parity-final-rerun-agent.txt` |
| 2026-11-18 | 소스 통합 | Codex 폴더의 cifs·remove 변경과 agent를 `c:\dev\ncc-vbr-go`에 합침([05-merge-record.md](05-merge-record.md)). 아래 네 줄은 통합 위치에서 실행한 결과다 | - | - |
| 2026-11-18 | V0·V1 Windows (전체) | `gofmt -l .`, `go vet`, `go test -p 1 -count=1 -v ./...` | 통과 46, 건너뜀 4, 실패 0 | `evidence/merge/unit-windows.txt` |
| 2026-11-18 | V1 Linux (전체) | `go/test/run-linux-tests.sh` + agent 10회 반복 | 통과 49, 건너뜀 1, 실패 0. 반복 10/10 | `evidence/merge/unit-linux.txt` |
| 2026-11-18 | cifs·remove V2 | `docker run ncc-vbr-go-parity` (`Full Scan Day = -`) | 통과 28, 실패 0 | `evidence/merge/parity-cifs-remove.txt` |
| 2026-11-18 | agent V2 | `docker run ncc-vbr-go-agent-parity` | 같음 97, 의도한 차이 21, 다름 0 | `evidence/merge/parity-agent.txt` |
| 2026-11-18 | 소스 변경 | 사용자가 준 2.068 조각과 대조해 cifs `Full Scan Day`의 잘못된 값 경고 로그 삭제. 시험에 2.068 재구성판과 실행 8 추가 | cifs 단위 시험 통과(Windows) | - |
| 2026-11-18 | cifs·remove V2 | `docker run ncc-vbr-go-parity`(실행 8 추가) | 통과 42, 실패 0 | `evidence/merge/parity-cifs-2068.txt` |
| 2026-11-18 | 사용자 Windows 확인 | [06-windows-test-guide.md](06-windows-test-guide.md) cifs W-CIFS-01~09, remove W-RM-01~07 (사용자 실행) | 정상. agent(W-AG)는 이번에 실행하지 않음 | 대화 기록 |
| 2026-11-18 | 소스 변경 | 실기 R1 인증 실패(PowerShell 파이프 BOM)로 `vbr_secret` 이 입력 첫 줄의 UTF-8 BOM 을 떼게 함. `TestReadPassword` 추가 | 단위 시험 통과(Windows), `build.ps1 all` 재빌드 | `agent/06` 2장 |
| 2026-11-18~19 | agent V3 실기(일부) | Windows PC의 `vbr_agent.exe` → 실제 XenServer 9 풀, 가이드 4장 R0~R9 (사용자 실행) | IT-01~05, PT-01, PT-02 통과. 원본 VM이 있는 풀의 preserve=true 복구는 DUPLICATE_MAC_SEED로 거부(운영 안내 반영). 정리 완료 | `evidence/agent/windows-real-20261119/`, `agent/06` 5.3 |

V2 1차와 2차의 원인과 조치는 [cifs/06-test-report.md](../cifs/06-test-report.md) 5장에 있다.

## 6. agent 사전 확인 진행 (2026-11-17)

| 일시 | 받은 것 | 한 일 | 결과 위치 |
|---|---|---|---|
| 2026-11-17 | Q-A01 답변: XAPI로 한다. ncc-xen에 구현이 있다 | ncc-xen `backend/pkg/xapi`와 백업 코드의 구조·의존성 확인(커밋 043c6d5). 실행 위치와 재사용 방식을 새 결정 Q-A09·A10으로 올림 | `agent/02` 1.1절, 3장 |
| 2026-11-17 | Q-A02 답변: XenServer 9 풀 | 읽기 전용 표본 수집 스크립트 작성. `sh -n`, `dash -n` 문법 검사 통과. 실제 호스트 실행은 사용자 | `go/test/agent/collect-xe-samples.sh`, `agent/05` 7장 |
| 2026-11-17 | Q-A03 답변: 2015 전문 표본(엑셀), 운영 로그 1건 | 원본 소스와 대조해 전문 명세 초안 작성. 표본과 현재 소스의 차이, 결함 후보(R-AG-16) 기록 | `agent/05`, `evidence/agent/` |

| 2026-11-17 | 사용자 결정: agentless 원격 실행, 관리·감시 기능 삭제와 백업·복구 중심 재구성, `S` 미지원, ncc-xen 코드는 복사 후 수정·변경 문서화 | 원본 백업·복구(3784~4213행) 전체 읽음. ncc-xen 백업 모듈의 의존과 동작 차이(NX-01~09) 확인. 재구성 검토서, 복사 코드 대장(계획), 양식 T11, 표준 구조의 복사 규칙 작성 | `agent/00`, `agent/08`, `templates/T11`, `common/02` 5장 11번 |

| 2026-11-17 | 사용자 답변: 1.602 기준, XVA 검증 따로, 암호는 ncc-xen 방식, 나머지 권장안. 이어서 조회 후순위 | 구현(`go/internal/agent`, `xapi`, `secret`, `cmd/vbr_agent`, `cmd/vbr_secret`), 단위 시험 21건, 원본 비교 시험 환경(`go/test/agent-parity`) 작성·실행. 변경·결함 대장, 시험 보고서, 전환·운영 안내, 설정 명세, 복사 대장 확정 | `agent/04`, `06`, `07`, `08`, `09`, `evidence/agent/` |

## 7. 남은 일

| 번호 | 일 | 누가 | 조건 |
|---|---|---|---|
| 1 | 결정 대기 항목 확인 | 사용자 | [04-open-decisions.md](04-open-decisions.md) |
| 2 | cifs·remove 통합 시험(V3)과 운영 규모 시험(V4) | 사용자 | 시험 서버, NAS 마운트, 포털 접속 정보 |
| 3 | agent 결정 Q-A18, Q-A19 | 사용자 | [04-open-decisions.md](04-open-decisions.md) 4장 |
| 4 | agent 남은 통합 시험 IT-06·07, PT-03·04 | 사용자 | Linux agent 서버, 실제 관리 서버, `/BACKUP` NAS. PT-01·02, IT-01~05는 2026-11-19 Windows 실기로 통과([agent/06-test-report.md](../agent/06-test-report.md) 5.3) |
| 4-1 | (선택) `collect-xe-samples.sh` 실행 결과 | 사용자 | 조회 기능을 나중에 더할 때 필요. 지금 범위에는 필요 없다 |
| 5 | 조회(`I`) 기능 | 전환 수행자 | 사용자가 필요하다고 할 때(DEC-AG-09) |
| 6 | Git 등록 | 사용자 | 전환 작업 뒤 따로 진행(사용자 지시). 대상은 `c:\dev\ncc-vbr-go` |
| 7 | ~~cifs 2.068 원본 소스~~ | - | 2026-11-18 해결. 2.068 조각으로 재구성판을 만들어 비교했다(Q-12) |
