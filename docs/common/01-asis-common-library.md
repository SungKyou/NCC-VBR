# 공통 라이브러리 `bs_common` 현행 분석서

기준일: 2026-11-17 / 원본: `cifs/bs_common.c`(1,501줄), `remove/bs_common.c`(1,506줄), `agent/bs_common.c`(1,448줄)와 각 `bs_common.h` / 읽은 범위: cifs 판 전체, 나머지 두 판은 cifs 판과의 차이 전체 / 양식: T02

## 1. 개요

세 서비스가 각자 폴더에 복사본을 갖고 있는 공통 라이브러리다. 설정 읽기, 로그, TCP 통신, 데몬화, 시각 문자열, 문자열 처리를 제공한다. 공유 라이브러리가 아니라 소스 복사라서 판마다 조금씩 다르다.

## 2. 세 판의 차이

| 항목 | agent | cifs | remove |
|---|---|---|---|
| 소스 인코딩 | CP949 | UTF-8 | UTF-8 |
| `MAX_CIFS_THREAD` | 20 | 50 | 50 |
| `MAX_FILE_LEN` | 1,024 | 10,240 | 10,240 |
| `BS_Tsyslog2`(`server2.log`) | 없음 | 있음(호출은 중지) | 없음 |
| `BS_Delete`, `BS_Delete_dir`, `BS_WriteShellQuoted` | 없음 | 없음 | 있음 |
| `BS_NetworkListen`의 close-on-exec 설정 | `fcntl(F_SETFD)`를 호출한다 | 값만 계산하고 설정하지 않는다 | cifs와 같음 |
| `ethernet_mactoa` | `static` | `extern` | `extern` |
| 로그 교체 메시지 | 파일 이름 없음 | 파일 이름 포함 | cifs와 같음 |
| `TRAN_LOG3`의 교체 대상 | `transaction`(이름이 틀림) | `transaction.log` | cifs와 같음 |

차이는 `diff`로 확인했다(줄 끝 문자와 인코딩을 맞춘 뒤 비교). 위 표 밖의 내용은 세 판이 같다.

## 3. 기능 목록

"쓰는 곳"은 각 서비스의 본체 소스에서 호출을 찾은 결과다.

| 번호 | 기능 | 함수(cifs 판 줄) | 설명 | cifs | remove | agent |
|---|---|---|---|---|---|---|
| F-COM-01 | 설정 읽기 | `BS_GetProfileString`(1010), `seekApp`(1087), `getNextKey`(1124), `trim`(1217), `isBlank`(1210) | 호출할 때마다 `./node_info.cfg`를 열어 섹션과 키로 값을 찾는다 | 사용 | 사용 | 사용 |
| F-COM-02 | 서버 로그 | `BS_Tsyslog`(468) | `./server.log`에 `[날짜 시각]메시지` 추가. 크기 초과 시 `.old`로 교체 | 사용 | 사용 | 사용 |
| F-COM-03 | 거래 로그(배치용) | `TRAN_LOG3`(841) | `./transaction.log`에 NUL + `[날짜 시각:밀리초]메시지` | 사용 | - | - |
| F-COM-04 | 거래 로그(전문용) | `TRAN_LOG2`(772), `TRAN_LOG`(709) | `./transaction-YYYYMM.log`에 전문 단위로 기록. 파일 이름의 연월은 전문의 일련번호에서 가져온다 | - | - | 사용 |
| F-COM-05 | 접속 로그 | `XD_Tsyslog`(418) | `./client_connect.log` | - | - | 사용 |
| F-COM-06 | 백업·복구 로그 | `BACKUP_LOG`(576), `IMPORT_LOG`(646) | `./backup.log`에 단계(스냅샷, 내보내기, 가져오기 등)별 기록 | - | - | 사용 |
| F-COM-07 | 데몬화 | `BS_Daemonize`(977) | `fork`, 부모 종료, `setsid`, `umask(0)` | 사용 | 사용 | 사용 |
| F-COM-08 | TCP 수신 준비 | `BS_NetworkListen`(31) | 포트를 설정에서 읽어 수신. 같은 이름의 프로세스가 있으면 중복 실행으로 판단. 포트가 점유되어 있으면 `fuser -k`로 점유 프로세스를 죽이고 3회까지 재시도 | - | - | 사용 |
| F-COM-09 | TCP 접속 수락 | `BS_NetworkAccept`(222) | 접속을 받고 상대 주소를 돌려준다 | - | - | 사용 |
| F-COM-10 | TCP 접속 | `BS_Connect`(150), `BS_NetworkConnect`(252), `BS_GetIpByName`(947) | 이름 또는 주소로 접속 | - | - | 확인 필요 |
| F-COM-11 | 정해진 길이 송수신 | `BS_NetworkReadN`(315), `BS_NetworkWriteN`(366) | 지정한 byte 수만큼 반복해 읽고 쓴다 | - | - | 확인 필요 |
| F-COM-12 | 시각 문자열 | `BS_date_time`(896), `BS_date_time_no_delimiter`(913), `BS_date_time_no_delimiter2`(930) | `YYYY-MM-DD HH:MM:SS`, `YYYYMMDD-HHMMSS`, `YYYYMMDDHHMMSS` | 사용 | - | 사용 |
| F-COM-13 | 초기화 | `BS_initialize`(1348) | 로그 최대 크기 기본값 50,000,000 | 사용 | 사용 | 사용 |
| F-COM-14 | 삭제 명령 기록 | `BS_Delete`, `BS_WriteShellQuoted`(remove 판 520~559) | `./delete.log`에 `rm -f -- '경로'` | - | 사용 | - |
| F-COM-15 | 처리 시간 로그 | `BS_SubLog`(5) | 함수 시작과 끝, 소요 시간 | - | - | 확인 필요 |
| F-COM-16 | MAC 주소 조회 | `BS_EthernetMac`(1362), `ethernet_mactoa`(1335) | ARP 표에서 주소 조회 | - | - | 확인 필요 |
| F-COM-17 | 16진수 출력 | `HexPrint`(1238) | 전문을 16진수로 로그에 출력 | - | - | 확인 필요 |
| F-COM-18 | 문자열 처리 | `text_cut`(1414), `find_str`(1429), `msg_cut`(1475) | 구분자·줄 단위 자르기 | - | - | 확인 필요 |

"확인 필요"는 agent 본체를 줄 단위로 읽지 않아 호출 여부를 확정하지 못한 항목이다. agent 전환 1단계에서 채운다.

## 4. 설정 파일 읽기 규칙 (F-COM-01)

1. 호출할 때마다 파일을 처음부터 읽는다.
2. 한 줄을 255자까지 읽는다. 넘는 부분은 다음 줄로 취급된다.
3. `[섹션명]`으로 시작하는 줄을 찾는다. 줄의 첫 글자가 `#`이면 건너뛴다.
4. 그 뒤 줄을 차례로 읽는다. 앞뒤 공백(공백, 탭, CR, LF)을 없앤 뒤 빈 줄과 `#` 줄은 건너뛴다. `[`로 시작하면 섹션이 끝난다.
5. 첫 `=` 앞을 키, 뒤를 값으로 본다. 양쪽 공백을 없앤다. `=`가 없으면 줄 전체가 키이고 값은 빈 문자열이다.
6. 키가 정확히 같으면(대소문자 구분) 값을 돌려준다. 값은 255자까지다.

형식 명세는 [03-config-spec.md](03-config-spec.md)에 있다.

## 5. 로그 쓰기 규칙 (F-COM-02~06)

1. 쓸 때마다 파일을 추가 모드로 열고, 쓰고, 닫는다.
2. 메시지는 300,000 byte 지역 버퍼에 `vsprintf`로 만든다. 길이를 검사하지 않는다.
3. 쓴 뒤 파일 크기가 최대 크기를 넘으면 `<파일>.old`로 이름을 바꾼다. 이전 `.old`는 덮어쓴다. 교체 결과를 `server.log`에 쓴다.
4. 잠금이 없다. 여러 스레드가 동시에 쓴다.
5. 줄바꿈은 호출하는 쪽이 메시지 끝에 넣는다(`BS_Tsyslog`). `TRAN_LOG3`은 자동으로 붙인다.

## 6. 운영체제·환경 의존

| 항목 | 쓰는 곳 | 비고 |
|---|---|---|
| 실행 디렉터리 기준 상대 경로 | 설정과 모든 로그 | 실행한 디렉터리가 달라지면 다른 파일을 쓴다 |
| `fork`, `setsid`, `umask` | 데몬화 | |
| `popen("ps -ef\|grep …")`, `system("fuser -k -n tcp …")` | 수신 준비 | 프로세스 이름 문자열로 중복 실행을 판단한다. 포트를 쓰는 다른 프로세스를 죽인다 |
| `ioctl(SIOCGARP)`, `net/if_arp.h`, `linux/sockios.h` | MAC 조회 | Linux 전용 |
| `sys/io.h` | 헤더 포함만 | x86 Linux 전용 헤더 |
| `gethostbyname`, `inet_aton` | 이름 풀이 | IPv4만 |
| 전역 변수를 헤더에 정의 | `bs_common.h` 67~74 | 최신 gcc는 `-fcommon`이 있어야 링크된다 |

## 7. 결함과 특이 동작

| 번호 | 내용 | 영향 | 원본 위치(cifs 판) | 전환본 처리 |
|---|---|---|---|---|
| D-COM-01 | 설정 한 줄 255자 제한 | 긴 값이 잘린다 | 1023, 1141 | 제한 없음 |
| D-COM-02 | 로그 메시지 길이를 검사하지 않는다 | 300,000 byte를 넘는 메시지는 스택을 덮어쓴다. 로그 한 번에 스레드 스택 300KB를 쓴다 | 475, 495 | 길이 제한 없는 문자열 |
| D-COM-03 | 로그 쓰기에 잠금이 없다 | 교체 시점에 두 스레드가 겹치면 한쪽 줄이 `.old`로 가거나 교체 메시지가 두 번 나올 수 있다 | 468~520 | 잠금 |
| D-COM-04 | `BS_Tsyslog2`가 문자열 상수에 쓴다(`sprintf(LOG_FILE2, …)`) | 호출되면 비정상 종료한다. 호출이 중지되어 드러나지 않는다 | 563 | 함수 삭제 |
| D-COM-05 | 값을 돌려준다고 선언한 함수가 돌려주지 않는다(`BS_date_time` 등, `BS_initialize`, `text_cut`) | 반환값을 쓰는 곳이 없어 문제가 없다 | 896~945, 1348, 1414 | 해당 없음 |
| D-COM-06 | 수신 준비에서 포트를 쓰는 프로세스를 무조건 죽인다 | 같은 포트를 쓰는 다른 서비스가 죽는다 | 119~131 | agent 전환에서 결정(Q-A05) |
| D-COM-07 | 중복 실행 판단이 `ps` 출력의 문자열 검색이다 | 이름이 비슷한 다른 프로세스나 편집기가 떠 있으면 실행되지 않는다 | 108~116 | agent 전환에서 결정(Q-A05) |
| D-COM-08 | cifs·remove 판은 수신 소켓의 close-on-exec를 설정하지 않는다 | 이 두 서비스는 수신 기능을 쓰지 않아 영향이 없다 | 69~75 | 해당 없음 |
| D-COM-09 | agent 판 `TRAN_LOG3`이 교체할 파일 이름을 잘못 쓴다 | agent는 `TRAN_LOG3`을 쓰지 않는 것으로 보인다 | agent 판 830 | 해당 없음 |

## 8. 전환 범위

cifs와 remove 전환에서 Go로 옮긴 것은 F-COM-01, 02, 03, 07, 12, 13, 14다. 나머지(F-COM-04~06, 08~11, 15~18)는 agent만 쓰므로 agent 전환에서 옮긴다. 대응은 [02-tobe-standard-structure.md](02-tobe-standard-structure.md) 4장에 있다.
