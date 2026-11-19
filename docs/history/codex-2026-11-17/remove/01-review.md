# Remove 요건·현행·전환

## 요구사항과 추적

| ID | 업무 규칙 | 원본과 Go | 검증 |
|---|---|---|---|
| RM-01 | NAS를 재귀 순회하고 이전 버전 파일의 보존 일수를 적용한다. | `remove/vbr_remove.c main:89, cifs_root:317, cifs_sub:549, check_rm_date:684`. Go `internal/remove/remove.go Run`, `internal/walk/walk.go Run`. | 정상 마운트/해제, 제외·2단 분할, 경계 전·동일·후 |
| RM-02 | 현재 백업과 일반 파일 보존. 파일명에 유효한 `-vbr-YYYYMMDDHHMMSS.zip` 형식이 있는 기존 버전만 후보로 삼는다. 현재 판정의 유효성은 숫자 14자리이며 달력 유효성을 따로 검증하지 않는다. | `file_delete:471`과 Go `IsExpiredVersion`. 이름 앞부분 판단에서 전체 경로 대신 파일명만 사용하도록 보완. | 경계 파일, 일반 zip, 앞부분이 없는 중첩 파일 |
| RM-03 | 설정 키 `Delete Mode=DELETE`일 때만 대상 파일을 지운다. 그 외에는 `delete.log`에 기록한다. | C `file_delete`: 모드 9 또는 `BS_Delete`. Go `deleteMode`, `remover.File`, `appendDeleteLog`. | 격리 디렉터리에서 LOG/DELETE 차이, 동일 이름 충돌, 파일 잠금 |
| RM-04 | 계산 가능한 보존 일수만 수용한다. | Go `ParseDays`, `Run`에 오버플로 방지. 기존의 0일과 설정 방식은 유지한다. | 음수, 비숫자, 106752일, 정상 입력, 직접 API 범위 초과 |
| RM-05 | CIFS의 원본 삭제 30일 추적은 이력 파일 만료와 분리한다. | 원본 대조나 30일 삭제 장부는 현재 Remove에 없다. 새 장부 도입 전 Delete 대상은 이력에 한정한다. | 원본에 없는 본체 백업이 기존 Remove에서 계속 보존되는지 확인 |

## 운영 및 인수

Linux 우선. Windows는 로컬 단위 시험과 컴파일 호환을 별도로 확인한다. Windows에서 `delete.log`의 POSIX 셸 구문을 직접 실행하는 절차는 규정하지 않는다. 실제 NAS에서는 원본과 Go를 각각 별도 작업 디렉터리에서 LOG 모드로 실행하고, 명령 순서가 달라도 상대경로 집합과 기준 시각·오류를 비교한다. `delete.log`는 실행 시작 시 교체되므로 파일을 실행 간 공유하지 않는다. DELETE 시험은 테스트 전용 공유의 합성 이력에만 적용한다. 새 바이너리 배치와 되돌리기는 스케줄러 교체 및 이전 바이너리·설정 경로 복구로 수행한다. 이미 삭제한 파일은 바이너리를 되돌려도 돌아오지 않으므로 별도의 복구 수단을 확인한 후 시험한다.
