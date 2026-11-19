# NCC-VBR 서비스 전환 문서

C로 개발된 NCC-VBR 서비스 3종을 Go로 전환하는 작업의 문서 모음이다.
이 전환은 서비스 교체와 함께, 앞으로 있을 비슷한 전환 프로젝트의 표준 모델을 만드는 것이 목적이다.
그래서 문서는 두 가지로 나뉜다. 어느 프로젝트에나 쓰는 절차·양식과, 이번 서비스에 그 양식을 채운 실제 문서다.

기준일: 2026-11-18

작업 위치는 `c:\dev\ncc-vbr-go`다.   세 서비스는 실행 파일과 버전을 따로 두고 따로 운영한다.  경위와 통합 뒤 검증은 [00-process/05-merge-record.md](00-process/05-merge-record.md)에 있다.

## 1. 현재 상태

| 서비스 | 역할 | 원본 | 전환 상태 | 검증 상태 |
|---|---|---|---|---|
| cifs | NAS 디렉터리 증분 백업 | `cifs/vbr_cifs.c` 2.0672 (1,250줄). 2.068에서 추가된 `Full Scan Day`도 반영(사용자가 준 코드 조각 기준) | Go 구현 완료 | 단위 시험 통과, 원본 비교 시험 통과(2.0672 기준, `Full Scan Day`는 2.068 재구성판과 비교). 실제 NAS·운영 규모 시험은 남음 |
| remove | 보존 기간이 지난 이전 버전 파일 삭제 | `remove/vbr_remove.c` 1.000 (721줄) | Go 구현 완료 | 단위 시험 통과, 원본 비교 시험 통과. 실제 NAS 시험은 남음 |
| agent | XenServer VM 백업·복구 | `agent/vbr_agent.c` 1.602 (5,275줄), `agent/dummy2.c` | Go 구현 완료. agentless(별도 Linux 서버, XAPI), 백업·부분 백업·복구·결과 조회·취소만. 조회(`I`)는 후순위 | 단위 시험 통과, 원본 비교 시험 통과(의도한 차이 22건은 변경 대장). 시험 풀·관리 서버 통합 시험은 남음 |
| 공통 | 설정·로그·통신 라이브러리 | `*/bs_common.c` 3벌 (각 약 1,500줄) | 세 서비스가 같이 쓰는 Go 패키지 한 벌(`boot`, `config`, `daemon`, `logx`) | 단위 시험 통과 |

agent는 원본을 그대로 옮기지 않고 범위를 줄여 다시 구성했다. 결정과 근거는 [agent/00-rescope-review.md](agent/00-rescope-review.md)에 있다. ncc-xen 코드를 복사해 고친 내역은 [agent/08-copied-code-register.md](agent/08-copied-code-register.md)에 있다.

사용자 확인이 필요한 결정은 [00-process/04-open-decisions.md](00-process/04-open-decisions.md)에 모았다.

## 2. 읽는 순서

처음 보는 사람은 다음 순서로 읽는다.

1. [00-process/01-migration-process.md](00-process/01-migration-process.md): 전환을 어떤 단계로 진행하고 단계마다 무엇을 만들고 무엇으로 통과를 판정하는지
2. [00-process/02-verification-procedure.md](00-process/02-verification-procedure.md): 검증을 어떤 수준으로 나누고 어떻게 실행하고 기록하는지
3. [00-process/03-progress.md](00-process/03-progress.md): 이번 프로젝트의 단계별 진행 현황과 증거 위치
4. 서비스별 문서(아래 3장)

## 3. 문서 지도

### 절차와 현황 (`00-process/`)

| 문서 | 내용 |
|---|---|
| [01-migration-process.md](00-process/01-migration-process.md) | 전환 절차. 단계, 관문, 산출물, 역할 |
| [02-verification-procedure.md](00-process/02-verification-procedure.md) | 검증 절차. 검증 수준, 실행 방법, 판정 표현, 증거 보관 |
| [03-progress.md](00-process/03-progress.md) | 진행 현황판. 단계별 완료 여부와 증거 |
| [04-open-decisions.md](00-process/04-open-decisions.md) | 사용자 결정이 필요한 항목 |
| [05-merge-record.md](00-process/05-merge-record.md) | 두 작업 폴더를 하나로 합친 기록. 출처, 파일별 선택, 통합 뒤 검증 |
| [06-windows-test-guide.md](00-process/06-windows-test-guide.md) | 사용자가 Windows에서 실행하는 cifs → remove → agent 확인 시험의 명령과 예상 결과. agent는 가짜 XAPI와 PowerShell 전문 송신 함수(`go/test/agent/vbr-send.ps1`)를 쓴다 |

### 양식 (`templates/`)

다음 전환 프로젝트에서 복사해 쓰는 빈 양식이다. 번호는 작성 순서다.

| 양식 | 용도 | 이번 프로젝트의 작성본 |
|---|---|---|
| [T01-requirements.md](templates/T01-requirements.md) | 요건 정의 | `cifs/01`, `remove/01` |
| [T02-asis-analysis.md](templates/T02-asis-analysis.md) | 현행(As-Is) 분석 | `cifs/02`, `remove/02`, `agent/01`, `common/01` |
| [T03-tobe-design.md](templates/T03-tobe-design.md) | 목표(To-Be) 설계 | `cifs/03`, `remove/03`, `common/02` |
| [T04-change-register.md](templates/T04-change-register.md) | 원본 대비 변경·결함 대장 | `cifs/04`, `remove/04` |
| [T05-interface-spec.md](templates/T05-interface-spec.md) | 외부 인터페이스 명세 | `cifs/05`, `remove/05`, `common/03` |
| [T06-test-plan-report.md](templates/T06-test-plan-report.md) | 시험 계획과 결과 | `cifs/06`, `remove/06` |
| [T07-cutover-operations.md](templates/T07-cutover-operations.md) | 전환(교체) 계획과 운영 안내 | `cifs/07`, `remove/07` |
| [T08-risk-register.md](templates/T08-risk-register.md) | 위험·사전 확인 사항 | `agent/02` |
| [T09-traceability.md](templates/T09-traceability.md) | 추적표(요건 → 원본 → 전환본 → 시험) | 각 서비스 `06`의 추적표 절 |
| [T10-conversion-plan.md](templates/T10-conversion-plan.md) | 서비스별 전환 계획 | `agent/03` |
| [T11-copied-code-register.md](templates/T11-copied-code-register.md) | 다른 프로젝트에서 복사해 고친 코드의 대장 | `agent/08` |
| [01-migration-workbook.md](templates/01-migration-workbook.md) | 통합 작업서. 작은 서비스는 T01~T10 대신 이 한 파일에 쓴다 | - |

양식은 세 가지 프로젝트 유형(A 전환, B 재설계, C 신규)에 쓴다. 유형별로 어느 절을 쓰는지는 [절차 0장](00-process/01-migration-process.md)에 있고, 양식을 고친 기록은 같은 문서 8장에 있다. 유형 B는 2단계 `vbr_vm`이 첫 적용 사례다.

범위를 줄이거나 방향을 바꾸는 결정은 양식 없이 검토서 한 편으로 남긴다. 작성 예는 `agent/00`(결정 → 기능별 유지·삭제 → 원본 동작 대응 → 가져올 코드 → 남은 결정)이다.

### 공통 (`common/`)

| 문서 | 내용 |
|---|---|
| [01-asis-common-library.md](common/01-asis-common-library.md) | 원본 공통 라이브러리 `bs_common` 분석 |
| [02-tobe-standard-structure.md](common/02-tobe-standard-structure.md) | Go 표준 구조. 폴더, 패키지, 코딩 규칙, OS 지원, Java 전환 대비 |
| [03-config-spec.md](common/03-config-spec.md) | 설정 파일 `node_info.cfg` 명세 |

### 서비스별

| 번호 | cifs | remove | agent |
|---|---|---|---|
| 00 | - | - | [재구성 검토서(agentless, 백업·복구 중심)](agent/00-rescope-review.md) — **agent는 이것부터 읽는다** |
| 01 | [요건 정의](cifs/01-requirements.md) | [요건 정의](remove/01-requirements.md) | [현행 사전 분석](agent/01-asis-preliminary.md) |
| 02 | [현행 분석](cifs/02-asis-analysis.md) | [현행 분석](remove/02-asis-analysis.md) | [위험과 사전 확인 사항](agent/02-risk-and-prerequisites.md) |
| 03 | [목표 설계](cifs/03-tobe-design.md) | [목표 설계](remove/03-tobe-design.md) | [전환 계획](agent/03-conversion-plan.md) |
| 04 | [변경·결함 대장](cifs/04-change-register.md) | [변경·결함 대장](remove/04-change-register.md) | [변경·결함 대장](agent/04-change-register.md) |
| 05 | [인터페이스 명세](cifs/05-interface-spec.md) | [인터페이스 명세](remove/05-interface-spec.md) | [인터페이스 명세(초안)](agent/05-interface-spec.md) |
| 06 | [시험 계획과 결과](cifs/06-test-report.md) | [시험 계획과 결과](remove/06-test-report.md) | [시험 계획과 결과](agent/06-test-report.md) |
| 07 | [전환 계획과 운영 안내](cifs/07-cutover-operations.md) | [전환 계획과 운영 안내](remove/07-cutover-operations.md) | [전환 계획과 운영 안내](agent/07-cutover-operations.md) |
| 08 | - | - | [복사 코드 대장(ncc-xen)](agent/08-copied-code-register.md) |
| 09 | (공통 [03](common/03-config-spec.md)) | (공통 [03](common/03-config-spec.md)) | [설정 명세](agent/09-config-spec.md) |

### 2단계: vbr_vm (`vm/`, 재설계 유형)

`vbr_agent`의 백업·복구 기능을 ncc-xen 규약의 JSON API 서비스로 새로 만든다(2026-11-19 착수). `vbr_agent`는 그대로 둔다.

| 번호 | 문서 | 상태 |
|---|---|---|
| 00 | [착수서](vm/00-charter.md): 목적, 범위, 착수 결정 DEC-VM-01~16, 문서 계획 | 작성 |
| 01 | [요건 정의서](vm/01-requirements.md) | 확인됨 |
| 02 | [기존·참조 분석서](vm/02-baseline-reference-analysis.md): 기존 기능, 실기 사실, ncc-xen 규약·복사 후보 | 확인됨 |
| 03 | [목표 설계서](vm/03-tobe-design.md) | 구현 반영 |
| 04 | [변경·결정 대장](vm/04-change-register.md): 변경 C-VM-01~20, 결정 DEC-VM-19~33, Q-VM-01·02 결정, Q-VM-03 대기, 실기 결함 D-VM-01~03 | 구현 반영 |
| 05 | [인터페이스 명세](vm/05-interface-spec.md): API, 오류 코드, `sysconfig.yaml` | 구현 반영 |
| 06 | [시험 계획과 결과](vm/06-test-plan-report.md): 개발 검증 통과(데모 자동 16/16, govulncheck 0), 2026-11-19 실기 I01~I03·I05~I10 통과, 실기 결함 3건 수정 뒤 Windows 77/4/0·Linux 76/1/0 | 남은 실기 I04·L01 등 |
| 07 | [설치와 운영 안내](vm/07-operations.md) | 작성(실기 전) |
| 08 | [복사 코드 대장](vm/08-copied-code-register.md) | 복사·수정 완료 |
| 명세서 | [NCC-VBR_VM_JobType_표준명세서_v1.00.docx](vm/NCC-VBR_VM_JobType_표준명세서_v1.00.docx): NCC-Xen Job Type 표준 명세서와 같은 서식의 개발팀 전달용 명세서. 원문은 `tools/spec/vm_spec_source.md`, 만들기는 [tools/spec/README.md](../tools/spec/README.md) | v1.00 (2026-11-19) |

### 증거 (`evidence/`)

시험을 실제로 실행한 출력이다. 문서의 "통과" 판정은 이 파일들을 근거로 한다.

| 파일 | 내용 |
|---|---|
| `unit-windows.txt` | Windows에서 실행한 단위 시험 출력 |
| `unit-linux.txt` | Linux(WSL Ubuntu)에서 실행한 단위 시험 출력 |
| `parity-final.txt` | 원본(C)과 전환본(Go) 비교 시험의 최종 출력. 28개 항목 통과(cifs 19, remove 9) |
| `cli-windows.txt` | Windows 실행 파일의 명령행 확인 출력(버전, 사용법, 인자 오류, 백그라운드 분리) |
| `parity-run-1.txt`, `parity-run-2.txt` | 비교 시험의 앞선 실행. 시험 환경과 시험 스크립트의 문제로 실패한 기록이다. 원인은 `cifs/06` 5장에 적었다 |
| `agent/message-samples-2015.xls`, `.txt` | 사용자가 준 agent 전문 표본(2015, v1.0)과 그 텍스트 변환본 |
| `agent/transaction-log-sample-20261117.txt` | 사용자가 준 운영 transaction 로그 1건(백업) |
| `agent/unit-windows.txt`, `agent/unit-linux.txt` | agent 구현 뒤 전체 저장소의 정적 검사와 단위 시험(Windows 43/4/0, Linux 46/1/0) |
| `agent/parity-final.txt` | agent 원본 비교 시험 최종(같음 96, 의도한 차이 22, 다름 0) |
| `agent/parity-run-1~5.txt` | agent 원본 비교 시험의 앞선 실행. 원인은 `agent/06` 3.4 |
| `agent/copied-code.diff` | ncc-xen 원본 대비 복사 코드의 실제 차이 |
| `parity-final-rerun-agent.txt` | 공통 `logx` 변경 뒤 cifs·remove 원본 비교 재실행 |
| `merge/unit-windows.txt`, `merge/unit-linux.txt` | 통합 뒤 전체 정적 검사와 단위 시험(Windows 46/4/0, Linux 49/1/0, agent 10회 반복) |
| `merge/parity-cifs-remove.txt`, `merge/parity-agent.txt` | 통합 뒤 원본 비교(cifs·remove 28 통과, agent 같음 97·의도한 차이 21·다름 0) |
| `vm/` | vbr_vm 개발 검증(단위 시험, govulncheck, 데모 자동, 회귀), `copied-code.diff`·`copied-original/`(ncc-xen 복사 코드) |
| `vm/windows-real-20261119/` | vbr_vm 실기(2026-11-19): 서비스 로그, 작업 기록 `jobs.jsonl`(암호·토큰 없음), 결함 수정 뒤 단위 시험(Windows, Linux) |

`history/codex-2026-11-17/`은 Codex 세션이 cifs·remove를 고치면서 쓴 문서 9편을 그대로 보관한 것이다. 내용은 서비스별 문서에 옮겨 적었다(05-merge-record 4장).

## 4. 폴더 구조

```
c:\dev\ncc-vbr-go
├─ agent\, cifs\, remove\     원본 C 소스. 고치지 않는다
├─ node_info.cfg              개발 환경 설정 파일(cifs·remove·agent)
├─ sysconfig.example.yaml     vbr_vm 개발 환경 설정 양식. 실제 sysconfig.yaml·key.yaml 은 여기 두고 커밋하지 않는다(.gitignore)
├─ docs\                      이 문서들(history\ 는 통합 전 Codex 세션 기록)
├─ tools\spec\                vbr_vm 표준 명세서(docx) 원문·생성기·양식
└─ go\                        전환본
   ├─ cmd\vbr_cifs, vbr_remove, vbr_agent, vbr_vm, vbr_secret   실행 파일의 시작점(vbr_vm 은 2단계)
   ├─ internal\                       업무 패키지(cifs, remove, agent, vm)와 공통 패키지(xapi, secret, stdlogger, i18n 등은 ncc-xen 에서 복사)
   ├─ test\                           Linux 시험 실행 스크립트, 원본 비교 시험(parity, agent-parity), vbr_vm 데모(vm\demo.ps1)
   ├─ build.ps1, build.sh             빌드 스크립트(원본 c1 대체)
   └─ bin\                            빌드 산출물
```
