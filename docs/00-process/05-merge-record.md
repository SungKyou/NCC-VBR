# 소스 통합 기록

기준일: 2026-11-18

## 1. 결정

| 항목 | 내용 | 근거 |
|---|---|---|
| 통합 위치 | `c:\dev\ncc-vbr-go`. 세 서비스(cifs, remove, agent)를 Go 모듈 하나(`ncc-vbr`)에 둔다 | 사용자 지시(2026-11-18) |
| 운영 | 세 서비스는 계속 따로 운영한다. 실행 파일(`vbr_cifs`, `vbr_remove`, `vbr_agent`)과 버전은 서비스별로 따로 둔다 | 사용자 지시 |
| 통합 이유 | 공통 기반 층(`boot`, `config`, `daemon`, `logx`)의 중복을 없앤다. 원본 C에서 `bs_common.c`가 세 벌로 복제된 문제를 되풀이하지 않는다 | 사용자 지시, 이 문서 3장 |
| 작업 도구 | 통합 뒤 모든 서비스 변경은 Claude Code가 한다. Codex는 이 저장소를 수정하지 않는다 | 사용자 지시(2026-11-18) |
| 이전 폴더 | `c:\dev\ncc-vbr`(agent 작업), `c:\dev\ncc-vbr(codex)`(cifs·remove 작업)는 읽기만 했고 고치지 않았다. 통합 뒤에는 기록으로만 쓴다 | - |

## 2. 출처와 공통 조상

두 폴더는 2026-11-17 12:35 무렵의 같은 소스에서 갈라졌다. Codex 폴더의 `go_code`가 그 시점 사본이다. 공통 조상과 비교해 각 폴더가 바꾼 파일은 다음과 같고, 두 쪽이 함께 바꾼 파일은 없다.

| 파일 | Codex(cifs·remove) | Claude Code(agent) | 통합본에 쓴 쪽 |
|---|---|---|---|
| `go/internal/cifs/cifs.go`, `copier.go`, `cifs_test.go` | 바꿈(C-CIFS-21, 22) | - | Codex |
| `go/internal/remove/remove.go`, `remove_test.go` | 바꿈(C-RM-05 보강, C-RM-17) | - | Codex |
| `go/internal/logx/logx.go` | - | `Append`, `Now` 추가 | Claude Code |
| `go/build.ps1`, `go/build.sh` | - | agent·시험 도구 빌드 추가 | Claude Code |
| `go/internal/agent`, `xapi`, `secret`, `go/cmd/vbr_agent`, `vbr_secret`, `go/test/agent`, `agent-parity` | 없음 | 새로 만듦 | Claude Code |
| 그 밖의 `go/` 파일 | 같음 | 같음 | Codex 폴더 사본 |
| 원본 C(`agent/`, `cifs/`, `remove/`), `node_info.cfg` | 같음 | 같음 | 같은 내용 |
| `docs/` | 새 간략 문서 9편(`docs`), 이전 문서 사본(`docs_code`) | 양식 기반 문서 전체 | Claude Code 문서를 기준으로 하고, Codex 변경은 해당 문서에 옮겨 적었다(4장). Codex 문서 9편은 `docs/history/codex-2026-11-17/`에 그대로 보관 |

옮기지 않은 것: Codex 폴더의 `go_code`(공통 조상 사본, 빌드 산출물 포함 34MB)와 `docs_code`(이전 문서 사본)는 위 표의 비교에만 썼다. 빌드 산출물 `go/bin`은 통합 위치에서 다시 만들었다.

## 3. 패키지 의존

서비스끼리는 서로 import하지 않는다. 같이 쓰는 것은 기반 층뿐이다.

| 층 | 패키지 | 쓰는 서비스 |
|---|---|---|
| 기반 | `boot`, `config`, `daemon`, `logx` | cifs, remove, agent |
| 파일 처리 | `walk`, `fsx` | cifs, remove |
| 포털 | `portal` | cifs |
| XenServer | `xapi`, `secret`(ncc-xen에서 복사, `agent/08`) | agent |
| 서비스 | `cifs`, `remove`, `agent` | 각자 |

기반 층을 고치면 세 서비스의 단위 시험과 원본 비교 시험을 모두 다시 돌린다([02-verification-procedure.md](02-verification-procedure.md)).

## 4. 문서에 옮긴 Codex 변경

| 변경 | 옮긴 곳 |
|---|---|
| cifs `Full Scan Day`(2.068 기능) | `cifs/04` C-CIFS-21, K-CIFS-03 보충, `cifs/02` 12장, `common/03`, 결정 대기 Q-12 |
| cifs `dis_` 이름 바꾸기 전 원본 `X` 확인 | `cifs/04` C-CIFS-22 |
| remove 보존 일수 상한(106,751일) | `remove/04` C-RM-05, 결정 대기 Q-06 |
| remove 대상 판단을 파일 이름으로 | `remove/04` C-RM-17(K-RM-03을 대체) |
| Codex 세션의 시험 기록(사용자 보고 포함) | `docs/history/codex-2026-11-17/verification.md`(그대로 보관) |

## 5. 통합 뒤 검증 (2026-11-18, 통합 위치에서 실행)

| 수준 | 명령 | 결과 | 증거 |
|---|---|---|---|
| V0 | `gofmt -l .`, `go vet ./...`(Windows, Linux 대상) | 지적 없음 | `evidence/merge/unit-windows.txt` 앞부분 |
| V1 Windows | `go test -p 1 -count=1 -v ./...` | 통과 46, 건너뜀 4, 실패 0 | `evidence/merge/unit-windows.txt` |
| V1 Linux | `go/test/run-linux-tests.sh`(WSL Ubuntu), agent 10회 반복 | 통과 49, 건너뜀 1, 실패 0. 반복 10/10 | `evidence/merge/unit-linux.txt` |
| 빌드 | `go\build.ps1 all` | 4개 실행 파일 × 2개 OS, 시험 도구 2개 | `go/bin` |
| V2 cifs·remove | `docker run --rm ncc-vbr-go-parity` | 통과 28, 실패 0 | `evidence/merge/parity-cifs-remove.txt` |
| V2 agent | `docker run --rm ncc-vbr-go-agent-parity` | 같음 97, 의도한 차이 21, 다름 0, 엉뚱한 응답 0 | `evidence/merge/parity-agent.txt` |

agent 원본 비교의 같음이 이전 최종(96, 22)보다 1건 많다. 원본이 결과를 보낼 때 관리 쪽이 이미 접속을 끊었는지에 따라 `send success`와 `send error`가 갈리는 기록(C-AG-03) 1건이 이번에는 두 쪽이 같았기 때문이다. 판정 기준(다름 0)은 같다.

cifs 원본 비교 시험의 기준은 저장소에 있는 2.0672다. 2.0672에는 `Full Scan Day`가 없으므로, 시험 설정(`go/test/parity/run.sh`)에 `Full Scan Day = -`를 넣어 전환본에서도 끄고 비교한다. 키가 없으면 전환본이 일요일로 보아 일요일 실행에서만 결과가 달라질 수 있기 때문이다. 위 결과는 이 설정을 넣은 뒤의 실행이다.

통합 뒤 같은 날 사용자가 2.068에서 추가된 `Full Scan Day` 코드 조각을 주었다(Q-12). 조각과 대조해 Codex 판이 잘못된 값에 남기던 별도 경고 로그를 뺐고, 시험 환경에 2.0672 사본에 조각을 넣은 2.068 재구성판과 실행 8을 더했다. 결과는 통과 42, 실패 0이다(`evidence/merge/parity-cifs-2068.txt`, [cifs/06](../cifs/06-test-report.md) T-CIFS-P20~P33).
