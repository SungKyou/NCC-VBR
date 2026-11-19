# remove 시험 계획과 결과

기준일: 2026-11-17 / 대상: `go/cmd/vbr_remove`, `go/internal/remove`와 공통 패키지 / 양식: T06

## 1. 판정

| 수준 | 판정 | 실행일 | 증거 |
|---|---|---|---|
| V0 정적 검사 | 통과 | 2026-11-17 | `evidence/unit-windows.txt` 앞부분 |
| V1 단위 시험 | 통과(Linux 6, Windows 6) | 2026-11-17 | `evidence/unit-linux.txt`, `evidence/unit-windows.txt` |
| V2 원본 비교 시험 | 통과(9개 항목) | 2026-11-17 | `evidence/parity-final.txt` |
| V3 통합 시험 | 미실행(실제 NAS 필요) | - | - |
| V4 운영 규모 시험 | 미실행(운영 규모 자료 필요) | - | - |
| 통합 뒤 V0~V2 재실행 | 통과(단위 시험은 U07과 C-RM-17 판단 기준 포함, 원본 비교 9개 항목) | 2026-11-18 | `evidence/merge/`, [00-process/05-merge-record.md](../00-process/05-merge-record.md) 5장 |

전체 판정: **개발 검증 통과**. 운영 전환 가능 판정은 V3, V4와 결정 대기 항목(Q-06~Q-11) 처리 뒤에 한다.

## 2. 시험 환경

cifs와 같다([cifs/06-test-report.md](../cifs/06-test-report.md) 2장). 원본은 `remove/c1`과 같은 옵션에 `-fcommon`과 스택 크기 옵션을 더해 컴파일했다.

## 3. 시험 목록과 결과

### 단위 시험 (V1)

시험 코드는 `go/internal/remove/remove_test.go`다.

| 번호 | 확인하는 것 | 시험 이름 | 관련 | Linux | Windows |
|---|---|---|---|---|---|
| T-RM-U01 | 삭제 대상 판단 표 10건(인터페이스 명세 5장) | `TestIsExpiredVersion` | REQ-05, N03, C-07 | 통과 | 통과 |
| T-RM-U02 | 보존 일수가 숫자가 아니거나 음수이거나 비어 있으면 거부한다 | `TestParseDays` | N03, C-05 | 통과 | 통과 |
| T-RM-U03 | 경로의 작은따옴표를 셸에서 안전하게 감싼다 | `TestShellQuote` | REQ-07 | 통과 | 통과 |
| T-RM-U04 | LOG 모드는 아무것도 지우지 않고 `delete.log`에 대상만 적는다. 이전 `delete.log`는 지운다 | `TestLogModeDeletesNothing` | REQ-07, 08, 09 | 통과 | 통과 |
| T-RM-U05 | DELETE 모드는 기준 시각 이전의 이전 버전 파일만 지운다. 기준 이후 버전, 일반 파일, 제외 디렉터리의 파일은 남는다. 기준 시각이 30일 전으로 계산된다 | `TestDeleteModeRemovesOnlyExpiredVersions` | REQ-04, 05, 06, 11, N03 | 통과 | 통과 |
| T-RM-U06 | 디렉터리가 없으면 실패로 끝난다 | `TestMissingDirectoryFails` | 오류 처리 | 통과 | 통과 |
| T-RM-U07 | 보존 일수가 날짜 계산 범위(106,751일)를 넘으면 순회 전에 거부한다 | `TestRunRejectsOverflowingDaysBeforeTraversing` | C-05(2026-11-18 통합) | 통과 | 통과 |

### 원본 비교 시험 (V2)

cifs의 비교 시험(실행 1~3)이 끝난 대상 디렉터리에 이어서 실행한다. 이전 버전 파일이 1개 있는 상태다.

| 번호 | 실행 | 확인하는 것 | 결과 |
|---|---|---|---|
| T-RM-P01 | 4. LOG 모드, 보존 0일 | `delete.log`의 삭제 명령 목록이 같다 | 통과 |
| T-RM-P02 | | 파일이 지워지지 않는다(구조가 같다) | 통과 |
| T-RM-P03 | | 합계(파일, 디렉터리, 삭제)가 같다 | 통과 |
| T-RM-P04 | 5. LOG 모드, 보존 30일 | 원본에서 삭제 대상이 없다 | 통과 |
| T-RM-P05 | | 전환본에서 삭제 대상이 없다 | 통과 |
| T-RM-P06 | 6. DELETE 모드, 보존 0일 | 대상 디렉터리 구조가 같다 | 통과 |
| T-RM-P07 | | 남은 파일 내용이 같다 | 통과 |
| T-RM-P08 | | 이전 버전 파일이 남지 않는다 | 통과 |
| T-RM-P09 | | 합계가 같다 | 통과 |

비교에서 뺀 것

- `2ND SEARCH DIR`: 원본이 동작하지 않는다(D-RM-01).
- 잘못된 보존 일수: 전환본이 일부러 거부한다(C-RM-05).
- 설정에 `QUEUE ID`가 없는 경우: 원본의 동작이 시스템 상태에 따라 달라진다(D-RM-06). 시험 설정에는 `QUEUE ID = 30077`을 넣었다.
- 건너뛴 수: 시험 입력의 대상 디렉터리에 기타 유형 항목이 없다. 합계의 앞 세 숫자만 비교했다.

### 명령행 확인

| 번호 | 확인하는 것 | 결과 | 증거 |
|---|---|---|---|
| T-RM-I01 | `-version`(0), 잘못된 보존 일수(255, 분리 전에 거부), 백그라운드 분리 실행(부모 0) | 통과 | `evidence/cli-windows.txt` |

### 통합 시험 (V3)과 운영 규모 시험 (V4)

| 번호 | 확인하는 것 | 결과 |
|---|---|---|
| T-RM-I02 | 실제 NAS의 백업 대상 디렉터리에서 LOG 모드로 실행해 원본의 `delete.log`와 비교한다 | 미실행 |
| T-RM-I03 | 실제 호출자가 전환본을 실행한다 | 미실행 |
| T-RM-L01 | 운영 규모 디렉터리에서 실행 시간이 원본보다 길지 않다 | 미실행 |

## 4. 추적표

| 요건 | 원본 기능 | 전환본 위치 | 시험 | 결과 |
|---|---|---|---|---|
| REQ-RM-01, 02 | F-01 | `boot.parse` | I01 | 통과 |
| REQ-RM-03 | F-11 | `daemon.Detach` | P 전체, I01 | 통과 |
| REQ-RM-04 | F-08 | `remove.Run` | U05, P04, P05 | 통과 |
| REQ-RM-05 | F-07 | `remove.IsExpiredVersion` | U01, U05, P01 | 통과 |
| REQ-RM-06 | F-07 | `remove.remover.File` | U05, P06~P09 | 통과 |
| REQ-RM-07 | F-10 | `remove.remover.appendDeleteLog`, `shellQuote` | U03, U04, P01~P03 | 통과 |
| REQ-RM-08 | F-01 | `remove.Run` | U04 | 통과 |
| REQ-RM-09 | F-04, 06 | `walk` | U04, P02 | 통과 |
| REQ-RM-10 | F-03, 05 | `walk.runWorkers` | P03 | 통과 |
| REQ-RM-11 | F-09 | `walk.dispatchDir` | U05 | 통과(원본 비교 없음) |
| REQ-RM-12 | F-09 | `walk.scanSecondLevel` | T-CIFS-U06(같은 엔진) | 통과(remove 설정으로 직접 시험하지 않음) |
| REQ-RM-13 | F-01 | `remove.Summary.fill` | P03, P09 | 통과(건너뛴 수는 비교하지 않음) |
| REQ-RM-N01 | - | 전체 | V0, V1을 두 운영체제에서 | 통과 |
| REQ-RM-N02 | - | - | P01 | 통과 |
| REQ-RM-N03 | - | `remove.IsExpiredVersion`, `ParseDays` | U01, U02, U05 | 통과 |
| REQ-RM-N04 | - | 빌드 | 실행 파일 하나 | 정적 확인 |

## 5. 실행 기록

cifs와 같은 실행에서 함께 확인했다. 전체 기록은 [cifs/06-test-report.md](../cifs/06-test-report.md) 5장에 있다. remove에만 해당하는 것은 다음과 같다.

| 일시 | 수준 | 내용 | 결과 | 실패 원인 | 조치 |
|---|---|---|---|---|---|
| 2026-11-17 | V2 2차 | 삭제 명령 목록 비교 실패 | 실패 | 시험. 원본과 전환본이 각자 다른 시각에 만든 이전 버전 파일의 이름을 그대로 비교했다 | 이름의 시각 부분을 가리고 비교 |
| 2026-11-18 | 사용자 Windows 확인 | [06-windows-test-guide.md](../00-process/06-windows-test-guide.md) W-RM-01~07 | 정상(사용자 확인). W-RM-04는 이전 버전 파일 5개(정상 형식 전부)가 `delete.log`에 들어가고 형식이 틀린 2개는 빠짐. 절차서 예상 3건보다 많은 것은 cifs 시험에서 이전 버전이 더 만들어졌기 때문 | 통과 | 없음 | 절차서를 전체 명령 블록으로 바꿈 |
| 2026-11-17 | 명령행 | 잘못된 보존 일수를 주면 부모는 0으로 끝나고 자식이 로그에 오류를 남겼다 | 설계 보완 | 전환본. 검사가 백그라운드 분리 뒤에 있었다 | 검사를 분리 전으로 옮겼다(`boot.Spec.Check`). 이후 모든 시험을 다시 실행 |

## 6. 실행하지 못한 시험

| 번호 | 사유 | 재개 조건 | 누가 |
|---|---|---|---|
| T-RM-I02, I03 | 개발 PC에 NAS와 호출 프로그램이 없다 | 시험 서버 | 사용자 |
| T-RM-L01 | 운영 규모 자료가 없다 | 운영 자료의 사본 | 사용자 |

## 7. 사용자 실행 시험의 절차

### T-RM-I02 실제 NAS에서 LOG 모드 비교

지우지 않는 LOG 모드로만 비교하므로 운영 자료에 실행해도 자료가 바뀌지 않는다.

1. 준비: 작업 디렉터리 두 개(`/vbr/rm-asis`, `/vbr/rm-tobe`)에 같은 `node_info.cfg`를 둔다. `Delete Mode` 줄을 넣지 않는다(LOG 모드). 원본용에는 다른 프로그램과 겹치지 않는 `QUEUE ID`를 넣는다.
2. 실행: 같은 디렉터리와 같은 보존 일수로 원본과 전환본을 이어서 실행한다. 끝났는지는 `server.log`의 `NCC-VBR batch finished`로 본다.
3. 확인: `sort /vbr/rm-asis/delete.log > a; sort /vbr/rm-tobe/delete.log > b; diff a b`가 차이를 내지 않는다. 두 합계 줄의 숫자가 같다.
4. 기록: 실행일, 디렉터리, 보존 일수, 대상 수, `diff` 결과.

DELETE 모드는 운영 자료의 사본에서만 비교한다.

## 8. 알려진 한계

- 시험 자료의 이전 버전 파일은 1~3개다. 많은 대상이 있을 때의 `delete.log` 쓰기와 삭제 속도는 확인하지 못했다.
- 한글 이름, 작은따옴표가 든 이름은 단위 시험(`TestShellQuote`)으로만 확인했다. 원본과 비교하지 않았다.
- 원본의 공유 메시지 큐 문제(D-RM-03)는 재현하지 않았다. 전환본은 큐를 쓰지 않으므로 해당 사항이 없다.
