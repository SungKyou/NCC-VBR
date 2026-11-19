# 검증 결과 및 미완료 항목

기준일 2026-11-17. 이번 검증 범위는 CIFS와 Remove다. 기존 검증 이력은 원본 프로젝트 `docs_code/evidence`와 서비스별 `06-test-report.md`에 있다.

## CIFS 2.068 요일별 전체 비교 추가 검증

| 환경 | 실행 | 결과 |
|---|---|---|
| Windows amd64 및 Linux amd64 교차 빌드 | `go build -p 1 ./...` | 모두 통과 |
| Windows amd64 | `go test -p 1 -count=1 -v ./internal/cifs` | 최상위 11건 통과, 3건 건너뜀, 0건 실패 |
| WSL Ubuntu, Linux amd64 | `go test -p 1 -c`로 교차 컴파일한 CIFS 테스트 바이너리를 `-test.v`로 실행 | 최상위 14건 통과, 0건 건너뜀, 0건 실패 |

신규 회귀시험은 화요일 설정, 지정 요일 외 실행, 미설정 시 일요일, 잘못된 설정의 네 사례를 확인했다. 기준 시각보다 오래된 원본 파일은 지정 요일에만 대상과 비교되어 크기가 다른 파일을 복사했고, 성공 뒤에는 실제 실행 시작 시각이 기록됐다. NetApp·Dell EMC 시험 공유에서의 연동 검증은 미실행이다.

2026-11-17 사용자 보고. `Full Scan Day` 추가 기능을 포함해 테스트 검증을 수행했다. 시험 환경, 항목별 결과와 로그는 공유되지 않아 NAS 연동 시험의 판정에는 반영하지 않았다.

## 프로젝트 경로에 저장한 뒤 재검증

실행 경로는 `C:\dev\ncc-vbr(codex)\go`다. Windows와 Linux amd64 교차 빌드에서 `go build -p 1 ./...`가 통과했다.

| 환경 | 실행 | 결과 |
|---|---|---|
| Windows amd64 | `go test -p 1 -count=1 -v ./...` | 25건 통과, 3건 건너뜀, 0건 실패. 포털 루프백과 개발 설정 확인도 통과했다. 건너뛴 항목은 Linux 시각·경로 규칙과 심볼릭 링크 시험이다. |
| WSL Ubuntu, Linux amd64 | Windows Go에서 `go test -p 1 -c`로 Linux 테스트 바이너리를 만든 뒤 Ubuntu에서 패키지 디렉터리별로 `-test.v` 실행 | 28건 통과, 0건 건너뜀, 0건 실패. 포털 루프백과 Linux 파일 동작을 포함했다. |

다음은 시험 NAS에서 CIFS의 최초·증분 백업, `dis_` 이동·충돌 및 복원을 확인하고, Remove를 LOG 모드로 실행해 삭제 후보를 비교하는 단계다. 실제 삭제 시험은 격리된 합성 데이터에서 수행한다.

## 사본에서 수행한 이전 검증

| ID | 명령·환경 | 결과 |
|---|---|---|
| V0-WIN | Go 1.26.0, Windows amd64, CGO=0, `go build -p 1 ./...` | 통과 |
| V0-LNX | Windows에서 linux/amd64, CGO=0, `go build -p 1 ./...` | 교차 컴파일 통과. Linux 실행 검증은 별도 |
| V1-CIFS | `go test -p 1 -count=1 -v ./internal/cifs -run '^TestDisabledFolder'` | 3/3 통과. 대상 재사용, 사용 중인 대상 보호, 원본 두 이름 공존 |
| V1-RM | `go test -p 1 -count=1 -v ./internal/remove` | 7/7 통과. 기간 경계·오버플로, 이름, LOG/DELETE 격리 자료 포함 |
| V1-ALL | Windows, 짧은 TMP/TEMP, `go test -p 1 -count=1 -v -skip '^TestSendAndReceive$' ./...` | 두 서비스와 공통 코드 기준 23건 통과, 0건 실패, 4건 건너뜀, 포털 소켓 시험 1건 제외 |
| V2 | C 원본/Go 변경본 비교 | 이번 세션 미실행. 다음은 Linux 격리 fixture에서 기존 parity 재실행 및 신규 사례 비교 |
| V3 | NetApp/Dell EMC, CIFS 완료 이벤트 수신 | 미실행. NAS 시험 공유와 포털 연동 환경 필요 |
| V4 | 파일 1억 개 내외 | 미실행. 동일 자료 및 측정 기준 확보 후 기준선과 비교 |

초기 Windows 실행에서는 긴 기본 임시 경로에서 CIFS 기준 시각 관련 두 시험이 실패했다. 짧은 임시 경로에서 같은 코드를 다시 실행하면 통과했다. 기존 상태 파일명이 두 루트 경로를 이어 붙이는 방식이라 실제 운영에서도 파일명 길이를 확인한다. 일부 임시 디렉터리 정리 실패도 초기 실행에서 관찰됐다. 루프백 시험 `TestSendAndReceive`는 127.0.0.1 연결 권한 오류로 제외했다. 나머지 건너뜀은 Linux 시각 규칙, 심볼릭 링크 권한, Linux 경로 규칙, 사본에 넣지 않은 개발 설정 파일이다. 개발 설정 확인 시험은 원본 경로에서 별도로 통과했다.

## 기능 진행과 재개 조건

| 항목 | 상태 | 재개 조건 |
|---|---|---|
| CIFS `dis_` | Go 보완과 로컬 검증 완료 | 운영과 동일한 테스트 공유에서 rename·충돌·복구를 확인 |
| 일반 이름 변경 감지 및 30일 보존 장부 | 요건/설계 | NetApp 식별자 안정성과 Dell EMC 이동/충돌 동작을 수집한 뒤 읽기 전용 관측을 구현 |
| Remove 기존 버전 만료 | Go 보완과 로컬 검증 완료 | 시험 NAS LOG 대조 및 격리 버전 파일 DELETE 검증 |
| Windows 운영 | 로컬 단위 시험·빌드 | 실제 SMB 경로, ACL 및 실행 스케줄러 확인 |
| 원본 설치 위치 | 다른 쓰기 가능 경로에 준비 | 이 세션의 쓰기 허용 경로가 프로젝트를 포함하도록 열거나 산출물의 `docs`, `go`를 복사 |

운영 NAS·XenServer는 이 세션에서 접속하거나 변경하지 않았다. 기존 C 소스, `go_code`, `docs_code`, Git 상태도 건드리지 않았다.
