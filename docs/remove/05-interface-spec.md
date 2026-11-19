# remove 인터페이스 명세

기준일: 2026-11-17 / 양식: T05

## 1. 목록

| 번호 | 종류 | 이름 | 방향 | 상대 |
|---|---|---|---|---|
| IF-RM-01 | 명령행 | `vbr_remove` | 입력 | 호출자 |
| IF-RM-02 | 설정 파일 | `node_info.cfg`의 `[File Remove (BATCH) for NCC-VBR]` | 입력 | 운영자 |
| IF-RM-03 | 파일 | `delete.log` | 출력 | 운영자 |
| IF-RM-04 | 파일 | `server.log` | 출력 | 운영자 |
| IF-RM-05 | 파일 시스템 | 삭제 대상 파일 | 삭제 | `vbr_cifs`가 만든 파일 |
| IF-RM-06 | 종료 코드 | | 출력 | 호출자 |

## 2. 명령행 (IF-RM-01)

```
vbr_remove [-home dir] [-config file] [-foreground] delete-dir days
vbr_remove -version
```

| 인자 | 뜻 | 필수 | 검사 | 원본과의 차이 |
|---|---|---|---|---|
| `delete-dir` | 찾을 디렉터리 | 예 | 열 수 없으면 종료 | 없음 |
| `days` | 보존 일수 | 예 | 0 이상의 정수. 아니면 실행하지 않음 | 원본은 검사하지 않음(C-RM-05) |
| `-version`, `-ver` | 버전 출력 | | | 버전 값과 종료 코드가 다름(C-RM-16) |
| `-home`, `-config`, `-foreground`(`-fg`) | cifs와 같음 | 아니오 | | 원본에 없음(C-RM-10) |

`days`가 0이면 지금까지의 모든 이전 버전 파일이 대상이다.

## 3. 설정 (IF-RM-02)

섹션은 `[File Remove (BATCH) for NCC-VBR]`다. 개발 환경 `node_info.cfg`에는 이 섹션이 없다.

| 키 | 형식 | 기본값 | 뜻 | 원본과의 차이 |
|---|---|---|---|---|
| `MAX Log size` | 정수(byte) | 50000000 | 로그 파일 교체 기준 | 없음 |
| `Delete Mode` | 문자열 | 없음(LOG 모드) | 값이 정확히 `DELETE`이면 실제로 지운다. 그 밖의 값은 LOG 모드 | 없음 |
| `MAX Thread cnt` | 정수 | 50 | 작업자 수. 50보다 크면 50 | 1보다 작으면 1 |
| `2ND SEARCH DIR` | 전체 경로를 공백으로 구분. 최대 10개 | 없음 | 2단계 분할 디렉터리 | 원본은 동작하지 않음(C-RM-02) |
| `EXCLUSION DIR` | 전체 경로를 공백으로 구분. 최대 10개 | 없음 | 찾지 않을 디렉터리 | 끝 구분자 무시(C-RM-12) |
| `QUEUE ID` | 정수 | - | 원본의 메시지 큐 키 | 전환본은 읽지 않는다(C-RM-01). 있어도 문제없다 |

설정 예

```ini
[File Remove (BATCH) for NCC-VBR]
MAX Log size   = 100000000
MAX Thread cnt = 10
Delete Mode    = DELETE
EXCLUSION DIR  = /backup/target/keep
```

## 4. 파일

### delete.log (IF-RM-03)

- 경로: `<작업 디렉터리>/delete.log`
- 만드는 시점: LOG 모드에서 삭제 대상을 찾을 때마다 한 줄 추가한다. 실행을 시작할 때 이전 파일을 지운다. 대상이 없으면 파일이 생기지 않는다.
- 형식: 한 줄에 셸 명령 하나. 경로는 작은따옴표로 감싸고, 경로 안의 작은따옴표는 `'\''`로 바꾼다.

```
rm -f -- '<경로>'
```

- 예:

```
rm -f -- '/backup/target/a/one.txt-vbr-20261117035235.zip'
rm -f -- '/backup/target/b/it'\''s.txt-vbr-20261001000000.zip'
```

- 쓰는 방법: 운영자가 내용을 검토한 뒤 `sh delete.log`로 실행한다.
- 줄 순서: 작업자가 찾은 순서라 실행마다 다를 수 있다.
- 원본과의 차이: 없음. 같은 입력에서 줄의 집합이 같다(T-RM-P01).

### server.log (IF-RM-04)

형식과 교체 규칙은 cifs와 같다([cifs/05-interface-spec.md](../cifs/05-interface-spec.md) 4장). 주요 문구:

```
[2026-11-17 13:02:59]File Remove (BATCH) for NCC-VBR start.  delete dir = [<디렉터리>], base(delete) days = [<일수>]
[2026-11-17 13:02:59]Current base date = 2026-11-17 13:02:59
[2026-11-17 13:02:59]Delete base date = 2026-10-18 13:02:59
[2026-11-17 13:02:59]File Remove (BATCH) for NCC-VBR TASK registered=>[<디렉터리>]
[2026-11-17 13:02:59]File Name [<경로>] : Delete finished.
[2026-11-17 13:02:59]NCC-VBR batch finished.  Directory = [<디렉터리>], delete base time = [YYYYMMDDHHMMSS]
[2026-11-17 13:02:59]Total File = [n], Total Dir = [n], Delete files = [n] Skip files = [n], Error = [n]
```

- `File Name … Delete finished.`는 DELETE 모드에서 지운 파일마다 한 줄이다.
- 원본과의 차이: 합계 줄 끝의 `, Error = [n]`, 설정 관련 안내 문구가 영문(C-RM-15).

## 5. 삭제 대상 (IF-RM-05)

일반 파일이고 전체 경로가 다음 모양으로 끝나야 한다.

```
<한 글자 이상>-vbr-<숫자 14자리>.zip
```

그리고 숫자 14자리(`YYYYMMDDHHMMSS`)가 삭제 기준 시각 이하여야 한다.

| 경로 | 기준 시각 `20261101000000`일 때 |
|---|---|
| `/nas/a/report.xlsx-vbr-20261031235959.zip` | 대상 |
| `/nas/a/report.xlsx-vbr-20261101000000.zip` | 대상(같으면 대상) |
| `/nas/a/report.xlsx-vbr-20261101000001.zip` | 대상 아님 |
| `/nas/a/report.xlsx` | 대상 아님 |
| `/nas/a/archive.zip` | 대상 아님 |
| `/nas/a/report.xlsx-vbr-20261031235959.zip.bak` | 대상 아님 |
| `/nas/a/report.xlsx-VBR-20261031235959.zip` | 대상 아님(대소문자 구분) |
| `/nas/a/report.xlsx-vbr-2026103123595X.zip` | 대상 아님(원본은 대상. C-RM-07) |

이 표는 단위 시험 `TestIsExpiredVersion`의 입력과 같다.

찾지 않는 곳: 스냅샷 디렉터리(이름의 첫 글자를 뺀 나머지가 `snapshot`), 제외 디렉터리, 심볼릭 링크가 가리키는 곳.

## 6. 종료 코드 (IF-RM-06)

| 코드 | 뜻 | 원본과의 차이 |
|---|---|---|
| 0 | 정상. 버전 출력. 백그라운드 분리에 성공한 부모 | 원본은 버전 출력 뒤 1 |
| 1 | 디렉터리 열기 실패, 중단 | 원본은 255 |
| 2 | 끝까지 처리했으나 오류가 있음 | 원본은 0 |
| 255 | 인자 개수 오류, 보존 일수 오류 | 보존 일수 오류는 원본에 없음 |
