# cifs 시험 계획과 결과

기준일: 2026-11-17 / 대상: `go/cmd/vbr_cifs`, `go/internal/cifs`와 공통 패키지 / 양식: T06

## 1. 판정

| 수준 | 판정 | 실행일 | 증거 |
|---|---|---|---|
| V0 정적 검사 | 통과 | 2026-11-17 | `evidence/unit-windows.txt` 앞부분 |
| V1 단위 시험 | 통과(Linux 통과 12, Windows 통과 9·건너뜀 3) | 2026-11-17 | `evidence/unit-linux.txt`, `evidence/unit-windows.txt` |
| V2 원본 비교 시험 | 통과(19개 항목) | 2026-11-17 | `evidence/parity-final.txt` |
| V3 통합 시험 | 미실행(실제 NAS 마운트와 포털 필요) | - | - |
| V4 운영 규모 시험 | 미실행(운영 규모 자료 필요) | - | - |
| 통합 뒤 V0~V2 재실행 | 통과(단위 시험은 U13·U14 포함, 원본 비교 19개 항목) | 2026-11-18 | `evidence/merge/`, [00-process/05-merge-record.md](../00-process/05-merge-record.md) 5장 |
| V2 2.068 `Full Scan Day` | 통과(실행 8, 14개 항목. 원본은 2.068 재구성판) | 2026-11-18 | `evidence/merge/parity-cifs-2068.txt` |

전체 판정: **개발 검증 통과**. 운영 전환 가능 판정은 V3, V4와 결정 대기 항목(Q-01~Q-05, Q-08~Q-11) 처리 뒤에 한다. 원본 비교 실행 1~7의 기준은 2.0672, 실행 8은 2.0672에 사용자가 준 2.068 `Full Scan Day` 조각을 넣은 재구성판이다(Q-12 해결).

## 2. 시험 환경

| 항목 | 값 |
|---|---|
| 개발 PC | Windows 11 Pro, Go 1.26.0 windows/amd64 |
| Linux 단위 시험 | WSL2 Ubuntu, 커널 6.6.87. Windows에서 만든 Linux용 시험 실행 파일을 `/tmp`에 복사해 실행 |
| 원본 비교 시험 | Docker의 alpine:3.20 컨테이너. gcc로 원본을 컴파일. 파일 시스템은 컨테이너의 로컬 디스크 |
| 원본 컴파일 방법 | `cifs/c1`과 같은 옵션에 `-fcommon`(전역 변수 중복 정의 허용)과 `-Wl,-z,stack-size=8388608`(스레드 스택 8MB)을 더함. 소스는 고치지 않음 |
| 2.068 재구성판 | 컨테이너 안에서 2.0672 사본에 사용자가 준 2.068 `Full Scan Day` 조각을 그대로 넣어(`go/test/parity/make-cifs-2068.sh`) 같은 옵션으로 컴파일. 전역 변수는 `backup_time` 선언 뒤, 설정 읽기와 요일 비교는 `lt_today = localtime(...)` 직후(조각이 `lt_today`를 쓰므로). 저장소의 원본은 고치지 않음 |

원본 비교 시험 환경은 운영 환경(CentOS, glibc, 실제 CIFS 마운트)과 다르다. 같은 것은 원본 소스와 Linux 커널의 파일·메시지 큐 동작이다.

## 3. 시험 목록과 결과

### 단위 시험 (V1)

시험 코드는 `go/internal/cifs/cifs_test.go`다.

| 번호 | 확인하는 것 | 시험 이름 | 관련 | Linux | Windows |
|---|---|---|---|---|---|
| T-CIFS-U01 | 첫 실행에서 모든 파일과 디렉터리를 복사한다. 이름에 공백이 있어도 된다. 스냅샷 디렉터리는 건너뛴다. `history` 디렉터리가 없어도 기준 시각을 쓴다 | `TestFirstRunCopiesEverything` | REQ-05, 07, 11, C-07 | 통과 | 통과 |
| T-CIFS-U02 | 변경 없이 다시 실행하면 아무것도 복사하지 않고, 기준 시각을 파일에서 읽는다 | `TestSecondRunCopiesNothing` | REQ-04, 09 | 통과 | 통과 |
| T-CIFS-U03 | 바뀐 파일은 덮어쓰고, 이전 내용은 `-vbr-시각.zip`으로 남는다. 풀면 이전 내용과 같다 | `TestChangedFileKeepsOldVersion` | REQ-09, 10 | 통과 | 통과 |
| T-CIFS-U04 | 대상에서 사라진 파일은 원본 시각과 관계없이 다시 복사한다 | `TestMissingTargetFileIsCopiedAgain` | REQ-07 | 통과 | 통과 |
| T-CIFS-U05 | 원본 파일이 기준 시각 이후 건드려지지 않았으면 대상 크기가 달라도 복사하지 않는다 | `TestUntouchedSourceIsNotCompared` | REQ-08, K-03 | 통과 | 건너뜀(Linux 시각 규칙) |
| T-CIFS-U06 | 제외 디렉터리는 복사하지 않는다. 2단계 디렉터리는 그 아래 디렉터리가 각각 작업이 되고, 바로 아래 파일도 복사된다 | `TestExclusionAndSecondLevel` | REQ-12, 13, 14, C-02, C-17 | 통과 | 통과 |
| T-CIFS-U07 | 원본에서 `team`이 `dis_team`으로 바뀌면 대상도 이름만 바뀌고 다시 복사하지 않는다 | `TestDisabledFolderRenamesTarget` | REQ-15 | 통과 | 통과 |
| T-CIFS-U08 | 대상의 `dis_team`에 내용이 있으면 이름을 바꾸지 않고 기존 자료를 둔다 | `TestDisabledFolderKeepsExistingData` | REQ-15, N04 | 통과 | 통과 |
| T-CIFS-U09 | 복사할 수 없는 파일이 있으면 오류로 세고 기준 시각을 쓰지 않는다 | `TestErrorKeepsHistoryUnchanged` | C-05, C-06, C-11 | 통과 | 통과 |
| T-CIFS-U10 | 대상 디렉터리가 없으면 시작하지 않는다 | `TestMissingTargetRootStops` | C-10 | 통과 | 통과 |
| T-CIFS-U11 | 심볼릭 링크는 복사하지 않는다 | `TestSymlinkIsSkipped` | REQ-06 | 통과 | 건너뜀(권한 필요) |
| T-CIFS-U12 | 기준 시각 파일 이름이 원본 규칙과 같다 | `TestHistoryFileName` | REQ-04, N02 | 통과 | 건너뜀(Linux 경로 규칙) |
| T-CIFS-U13 | `Full Scan Day` 요일에는 기준 시각보다 오래된 파일도 비교해 크기가 다르면 복사한다. 다른 요일, 키가 없을 때(일요일), 잘못된 값일 때도 확인한다. 2026-11-18 잘못된 값의 별도 경고 로그를 2.068에 맞춰 뺀 뒤 다시 통과(Windows, Linux) | `TestFullScanDayComparesFilesOlderThanHistory` | C-21(2026-11-18 통합) | 통과 | 통과 |
| T-CIFS-U14 | 원본에 `X`와 `dis_X`가 같이 있으면 대상의 `X`를 바꾸지 않는다 | `TestDisabledFolderPreservesActiveSourceBackup` | C-22(2026-11-18 통합) | 통과 | 통과 |

공통 패키지의 단위 시험(설정 3, 로그 3, 포털 2)은 [common/02-tobe-standard-structure.md](../common/02-tobe-standard-structure.md) 8장에 있다. Linux에서 설정 시험 1건은 건너뜀이다(개발 환경 설정 파일을 상대 경로로 찾는 시험이라 `/tmp`에서는 파일이 없다. Windows에서는 통과).

### 원본 비교 시험 (V2)

시험 스크립트는 `go/test/parity/run.sh`다. 원본과 전환본을 각자의 작업 디렉터리에서 같은 입력으로 실행하고 결과를 비교한다. 두 프로그램 모두 백그라운드 분리 방식으로 실행한다.

입력: 최상위 파일 1개, 디렉터리 `a`(파일 2개, 2MB 난수 파일 1개, 하위 디렉터리 1개, 심볼릭 링크 1개), `b`(이름에 공백이 있는 디렉터리와 파일), `skip`(제외 대상), `.snapshot`, `empty`(빈 디렉터리). 작업자 2개. `EXCLUSION DIR`에 `skip`.

| 번호 | 실행 | 확인하는 것 | 결과 |
|---|---|---|---|
| T-CIFS-P01 | 1. 첫 실행 | 대상 디렉터리 구조가 같다 | 통과 |
| T-CIFS-P02 | | 복사된 파일 내용(체크섬)이 같다 | 통과 |
| T-CIFS-P03 | | 합계(파일, 디렉터리, 변경)가 같다 | 통과 |
| T-CIFS-P04 | | 제외 디렉터리가 복사되지 않는다 | 통과 |
| T-CIFS-P05 | | 스냅샷 디렉터리가 복사되지 않는다 | 통과 |
| T-CIFS-P06 | | 심볼릭 링크가 복사되지 않는다 | 통과 |
| T-CIFS-P07 | | 기준 시각 파일 이름이 같다 | 통과 |
| T-CIFS-P08 | 2. 파일 1개 변경, 파일 1개와 디렉터리 2개 추가 후 실행 | 대상 디렉터리 구조가 같다(이전 버전 파일 포함) | 통과 |
| T-CIFS-P09 | | 복사된 파일 내용이 같다 | 통과 |
| T-CIFS-P10 | | 이전 버전 파일을 풀어낸 내용이 같다 | 통과 |
| T-CIFS-P11 | | 합계가 같다 | 통과 |
| T-CIFS-P12 | 3. 변경 없이 실행 | 대상 디렉터리 구조가 같다 | 통과 |
| T-CIFS-P13 | | 합계가 같다(변경 0건) | 통과 |
| T-CIFS-P14 | 7. 파일 1개 변경 후, 원본의 작업 디렉터리에서 전환본을, 전환본의 작업 디렉터리에서 원본을 실행 | 대상 디렉터리 구조가 같다 | 통과 |
| T-CIFS-P15 | | 복사된 파일 내용이 같다 | 통과 |
| T-CIFS-P16 | | 이전 버전 파일을 풀어낸 내용이 같다 | 통과 |
| T-CIFS-P17 | | 합계가 같다 | 통과 |
| T-CIFS-P18 | | 전환본이 원본의 기준 시각 파일과 대상을 이어받아 1건만 변경한다 | 통과 |
| T-CIFS-P19 | | 원본이 전환본의 기준 시각 파일과 대상을 이어받아 1건만 변경한다 | 통과 |
| T-CIFS-P20 | 8-a. 원본을 2.068 재구성판으로 바꾸고, 대상의 `root.txt`만 크기를 바꾼 뒤 `Full Scan Day`를 오늘이 아닌 요일로 두고 실행 | 합계가 같다 | 통과 |
| T-CIFS-P21 | | 원본은 기준 시각 필터로 0건 변경(K-CIFS-03) | 통과 |
| T-CIFS-P22 | | 전환본도 0건 변경 | 통과 |
| T-CIFS-P23 | | 원본 로그에 `Full search mode enable`이 없다 | 통과 |
| T-CIFS-P24 | | 전환본 로그에도 없다 | 통과 |
| T-CIFS-P25 | 8-b. `Full Scan Day`를 오늘 요일(`date +%w`)로 두고 실행 | 대상 디렉터리 구조가 같다 | 통과 |
| T-CIFS-P26 | | 복사된 파일 내용이 같다 | 통과 |
| T-CIFS-P27 | | 이전 버전 파일을 풀어낸 내용이 같다 | 통과 |
| T-CIFS-P28 | | 합계가 같다 | 통과 |
| T-CIFS-P29 | | 원본은 손상된 대상 1건을 다시 복사한다 | 통과 |
| T-CIFS-P30 | | 전환본도 1건을 다시 복사한다 | 통과 |
| T-CIFS-P31 | | 원본 로그 `Full search mode enable.[요일]` | 통과 |
| T-CIFS-P32 | | 전환본 로그도 같다 | 통과 |
| T-CIFS-P33 | | 두 쪽 모두 `Backup base time  = [0]`으로 시작 | 통과 |

실행 8은 시험한 날(2026-11-18, 수요일 3)의 요일 값 하나와 다른 요일 하나만 확인한다. 다른 요일 값의 동작은 T-CIFS-U13이 맡는다.

실행 번호 4~6은 remove의 시험이다([remove/06-test-report.md](../remove/06-test-report.md)).

비교에서 뺀 것

- `2ND SEARCH DIR`: 원본은 결함으로 동작하지 않아(D-CIFS-01) 비교할 수 없다. 전환본의 동작은 T-CIFS-U06으로 확인했다.
- 오류 상황(대상 없음, 쓰기 실패): 전환본이 일부러 다르게 동작한다(C-CIFS-05, 06, 10). 단위 시험으로 확인했다.
- 로그 문구: 합계 숫자만 비교했다. 문구 차이는 C-CIFS-19에 적었다.
- 포털 송신: 시험 설정에 포털 주소를 넣지 않았다.

### 명령행 확인

| 번호 | 확인하는 것 | 방법 | 결과 | 증거 |
|---|---|---|---|---|
| T-CIFS-I01 | `-version`, `-ver`, 인자 개수 오류(255), `-foreground` 실행(0), 백그라운드 분리 실행(부모 0), 대상 없음(자식이 로그에 오류) | Windows 실행 파일을 임시 디렉터리에서 실행 | 통과 | `evidence/cli-windows.txt` |

### 통합 시험 (V3)과 운영 규모 시험 (V4)

| 번호 | 확인하는 것 | 결과 |
|---|---|---|
| T-CIFS-I02 | 실제 CIFS 마운트에서 백업한다. 접근 시각 갱신 방식(마운트 옵션)에 따른 변경 판단이 원본과 같다 | 미실행 |
| T-CIFS-I03 | 실제 포털이 완료 이벤트를 받아들인다 | 미실행 |
| T-CIFS-I04 | 실제 호출자(관리 프로그램 또는 스케줄러)가 전환본을 실행한다 | 미실행 |
| T-CIFS-I05 | 한글 이름 파일과 디렉터리를 백업한다 | 미실행 |
| T-CIFS-L01 | 운영 규모 자료에서 실행 시간이 원본보다 길지 않다 | 미실행 |
| T-CIFS-L02 | 최상위 디렉터리가 많을 때(원본이 멈출 수 있는 조건. D-CIFS-04) 전환본이 끝까지 처리한다 | 미실행 |
| T-CIFS-L03 | 실행 중 메모리 사용이 받아들일 만하다(작업자 50개, 복사 버퍼 640KB씩) | 미실행 |

절차는 7장에 있다.

## 4. 추적표

| 요건 | 원본 기능 | 전환본 위치 | 시험 | 결과 |
|---|---|---|---|---|
| REQ-CIFS-01, 02 | F-01 | `boot.parse` | T-CIFS-I01 | 통과 |
| REQ-CIFS-03 | F-18 | `daemon.Detach` | T-CIFS-P 전체, I01 | 통과 |
| REQ-CIFS-04 | F-01 | `cifs.readHistory`, `saveHistory`, `historyPath` | U02, U12, P07, P18, P19 | 통과 |
| REQ-CIFS-05 | F-04, 06 | `walk.readDir`, `walk.IsSnapshotName` | U01, P05 | 통과 |
| REQ-CIFS-06 | F-04, 06 | `walk.scanRoot`, `walkDir` | U11, P06 | 통과 |
| REQ-CIFS-07 | F-07 | `cifs.copier.File` | U01, U04, P01, P02 | 통과 |
| REQ-CIFS-08 | F-07 | `fsx.Times.NotAfter` | U05, P13 | 통과(Linux) |
| REQ-CIFS-09 | F-07 | `cifs.changed` | U02, U03, P11 | 통과 |
| REQ-CIFS-10 | F-08, 09 | `cifs.copier.keepOldVersion`, `copyFile` | U03, P10 | 통과 |
| REQ-CIFS-11 | F-10 | `cifs.copier.EnterDir` | U01, P01, P08 | 통과 |
| REQ-CIFS-12 | F-03, 04, 05 | `walk.runWorkers`, `work` | U06, P03 | 통과 |
| REQ-CIFS-13 | F-13 | `walk.dispatchDir`, `matchAny` | U06, P04 | 통과 |
| REQ-CIFS-14 | F-12 | `walk.scanSecondLevel` | U06 | 통과(원본 비교 없음) |
| REQ-CIFS-15 | F-11 | `cifs.copier.TopDir` | U07, U08 | 통과(원본 비교 없음) |
| REQ-CIFS-16 | F-01 | `cifs.finish` | P03, P11, P13 | 통과 |
| REQ-CIFS-17 | F-14 | `portal.Encode`, `Client.Send`, `cifs.sendFinishEvent` | `portal` 단위 시험 2건 | 통과(전문 만들기와 TCP 송수신). 실제 포털은 미실행(I03) |
| REQ-CIFS-18 | F-16 | `logx.rotateLocked` | `logx` 단위 시험 | 통과 |
| REQ-CIFS-N01 | - | 전체 | V0, V1을 두 운영체제에서 | 통과 |
| REQ-CIFS-N02, N03 | - | - | P07, P10, P14~P19 | 통과 |
| REQ-CIFS-N04 | - | `cifs.copier` | U08, 정적 확인(원본 디렉터리에 쓰는 호출 없음) | 통과 |
| REQ-CIFS-N05 | - | - | L01 | 미실행 |
| REQ-CIFS-N06 | - | 빌드 | 실행 파일 하나, 외부 라이브러리 없음(`go.mod`에 의존 없음) | 정적 확인 |

원본 비교가 없는 요건은 REQ-14(원본이 동작하지 않음)와 REQ-15(`dis_` 처리)다. REQ-15는 비교 시험에 넣을 수 있는데 이번에는 넣지 않았다. 다음에 비교 시험을 고칠 때 더한다.

## 5. 실행 기록

| 일시 | 수준 | 명령 | 결과 | 실패 원인 | 조치 | 증거 |
|---|---|---|---|---|---|---|
| 2026-11-17 | V1 Windows | `go test ./...` | 실패 1(포털 전문) | 전환본. JSON 변환이 `>`를 `>`로 바꿈 | `portal.quote`에서 HTML 이스케이프를 껐다 | 없음(화면 출력만) |
| 2026-11-17 | V1 Windows | 같음 | 통과 | | | 최종 실행으로 대체 |
| 2026-11-17 | V1 Linux | `run-linux-tests.sh` | 통과 25, 건너뜀 1 | | | 최종 실행으로 대체 |
| 2026-11-17 | V2 1차 | `docker run ncc-vbr-parity` | 실패. 원본이 멈춤 | 환경. 컨테이너의 C 라이브러리(musl)는 스레드 스택 기본값이 작다. 원본은 스레드 스택에 300KB 메시지와 640KB 버퍼를 잡아 스택이 넘쳤다 | 링크 옵션으로 스택을 운영 환경 기본값(8MB)에 맞췄다. 소스는 고치지 않았다 | `parity-run-1.txt` |
| 2026-11-17 | V2 2차 | 같음 | 실패 4 | 시험. 난수 파일을 원본용과 전환본용으로 따로 만들었다. 삭제 목록에 실행 시각이 든 파일 이름을 그대로 비교했다 | 난수 파일을 한 번만 만든다. 시각 부분을 가리고 비교한다 | `parity-run-2.txt` |
| 2026-11-17 | V2 3차 | 같음 | 통과 22 | | | 보관하지 않음 |
| 2026-11-17 | V2 4차 | 같음(이어받기 시험 6개 추가) | 통과 28(cifs 19, remove 9) | | | 보관하지 않음 |
| 2026-11-17 | 명령행 | Windows 실행 파일 수동 실행 | 잘못된 보존 일수(remove)가 분리 뒤에야 거부됨 | 전환본 | 검사를 분리 전으로 옮김. 아래는 그 뒤의 재실행 | |
| 2026-11-17 | V0, V1 Windows | `gofmt -l .`, `go vet`, `go test -count=1 -v ./...` | 통과 23, 건너뜀 3 | | | `unit-windows.txt` |
| 2026-11-17 | V1 Linux | `run-linux-tests.sh` | 통과 25, 건너뜀 1 | | | `unit-linux.txt` |
| 2026-11-17 | V2 최종 | `docker run ncc-vbr-parity` | 통과 28(cifs 19, remove 9) | | | `parity-final.txt` |
| 2026-11-17 | 명령행 | Windows 실행 파일 수동 실행 | 통과 | | | `cli-windows.txt` |
| 2026-11-18 | 소스 변경 | `fullScanEnabled`에서 잘못된 값 경고 로그 삭제(2.068 조각과 대조) | - | | | |
| 2026-11-18 | V0, V1 Windows(cifs) | `gofmt -l`, `go vet ./internal/cifs/`, `go test ./internal/cifs/` | 통과 | | | 화면 출력만 |
| 2026-11-18 | V1 Linux(전체) | `run-linux-tests.sh` | 통과 49, 건너뜀 1, 실패 0 | | | 화면 출력만 |
| 2026-11-18 | 사용자 Windows 확인 | [06-windows-test-guide.md](../00-process/06-windows-test-guide.md) W-CIFS-01~09, 사용자 PC `C:\vbrtest` | 정상(사용자 확인) | 첫 시도는 `$B`·`$W` 변수를 정하지 않아 경로 오류(절차 보완). 이전 버전 파일 수는 변경 명령을 다시 실행해 예상보다 많았고 동작 차이는 아님 | 절차서 0장에 변수 확인, 1.2를 전체 명령 블록으로 바꿈. W-CIFS-07 설정 바꾸기 명령을 탭 없이도 맞게 고침 | 사용자 화면(대화 기록) |
| 2026-11-18 | V2 | `docker run ncc-vbr-go-parity`(실행 8 추가) | 통과 42(cifs 33, remove 9), 실패 0 | | | `merge/parity-cifs-2068.txt` |

V2 1차에서 알게 된 것: 원본은 스레드 스택을 1MB 가까이 쓴다. 스택 기본값이 작은 환경에서는 실행되지 않는다. 운영 환경(glibc, 기본 8MB)에서는 문제가 없다.

## 6. 실행하지 못한 시험

| 번호 | 사유 | 재개 조건 | 누가 |
|---|---|---|---|
| T-CIFS-I02~I05 | 개발 PC에 NAS 마운트, 포털, 호출 프로그램이 없다 | 시험 서버와 접속 정보 | 사용자 |
| T-CIFS-L01~L03 | 운영 규모 자료가 없다 | 운영 자료의 사본 또는 비슷한 규모의 시험 자료 | 사용자 |
| C-CIFS-16(종료 신호) | 자동 시험을 만들지 않았다 | `-foreground`로 실행 중 Ctrl+C를 눌러 기준 시각 파일이 바뀌지 않는지 확인 | 사용자 또는 전환 수행자 |

## 7. 사용자 실행 시험의 절차

### T-CIFS-I02 실제 NAS에서 원본과 비교

1. 준비: 시험 서버에 작업 디렉터리 두 개(`/vbr/asis`, `/vbr/tobe`)를 만들고 각각 `node_info.cfg`와 `history` 디렉터리를 둔다. 같은 NAS 원본 디렉터리를 가리키고 대상 디렉터리는 서로 다르게 한다.
2. 실행: `cd /vbr/asis && ./vbr_cifs <원본> <대상A> 1`, 끝난 뒤 `cd /vbr/tobe && ./vbr_cifs <원본> <대상B> 1`. 끝났는지는 `server.log`의 `VBR CIFS backup finished`로 본다.
3. 확인: `diff -r <대상A> <대상B>`가 차이를 내지 않는다. 두 `server.log`의 `Total File` 줄 숫자가 같다.
4. 원본 디렉터리의 파일 몇 개를 고친 뒤 2~3을 되풀이한다. 이전 버전 파일은 이름의 시각만 다르고 `gzip -dc`로 푼 내용이 같아야 한다.
5. 기록: 실행일, 파일 수, 두 합계 줄, `diff` 결과를 5장 표에 더한다.

### T-CIFS-I03 포털 이벤트

1. 준비: `node_info.cfg`의 `WEB server(IP)`, `WEB server(PORT)` 주석을 풀고 시험 포털 주소를 넣는다.
2. 실행: 작은 디렉터리로 백업을 한 번 실행한다.
3. 확인: `server.log`에 `Send to VBR WEB Portal success`와 응답이 있다. 포털 화면에 `CIFSFIN00` 이벤트가 보인다. 원본을 실행했을 때의 이벤트와 내용이 같다.

### T-CIFS-L01 실행 시간

1. 준비: 운영과 비슷한 규모의 원본 디렉터리, 같은 `MAX Thread cnt`.
2. 실행: 원본과 전환본을 각각 빈 대상에 첫 실행, 이어서 변경 없이 한 번 더 실행한다.
3. 확인: `server.log`의 `BEGIN`과 `backup finished` 줄의 시각 차이를 비교한다.
4. 기록: 파일 수, 전체 크기, 네 번의 소요 시간.

## 8. 알려진 한계

- 원본 비교 시험의 파일 시스템은 컨테이너의 로컬 디스크다. CIFS 마운트에서의 시각 갱신, 권한, 긴 경로, 접속 끊김은 확인하지 못했다.
- 큰 파일(수 GB)과 많은 파일(수십만 개)은 시험하지 않았다. 시험 자료는 파일 10개 안팎, 최대 2MB다.
- 한글 등 ASCII가 아닌 이름은 시험하지 않았다. 전환본은 이름을 byte 그대로 다루므로 Linux에서는 원본과 같을 것으로 보지만 확인한 것은 아니다.
- 동시에 같은 원본·대상 쌍을 실행하는 경우는 시험하지 않았다. 원본에도 막는 장치가 없다.
- Windows에서는 단위 시험과 명령행 확인만 했다. Windows에서 원본과 비교할 방법은 없다(원본이 Linux 전용이다).
