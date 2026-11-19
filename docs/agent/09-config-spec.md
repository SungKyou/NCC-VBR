# agent 설정 명세

기준일: 2026-11-17 / 파일: 작업 디렉터리의 `node_info.cfg`(다른 파일은 `-config`) / 섹션: `[VBR Agent for Xenserver]`(원본과 같다)
형식: 공통 [03-config-spec.md](../common/03-config-spec.md). `키 = 값`, `#` 주석, 한 줄 길이 제한 없음.

| 키 | 필수 | 기본값 | 원본 | 설명 |
|---|---|---|---|---|
| `PORT-NO` | 예 | - | 같다 | 관리 서버가 접속하는 수신 포트. 풀마다 agent 하나를 띄우면 풀마다 다른 포트(Q-A16) |
| `MAX Log size` | 아니오 | 50000000 | 같다 | `server.log`, `backup.log` 최대 크기(넘으면 `.old`). `transaction-YYYYMM.log` 는 바꾸지 않는다 |
| `XAPI Address` | 예 | - | 새 키 | 풀 마스터 주소. `https://` 가 없으면 붙인다. 마스터가 바뀌면 HOST_IS_SLAVE 응답을 따라간다 |
| `XAPI User` | 아니오 | root | 새 키 | XAPI 계정 |
| `XAPI Password` | 예 | - | 새 키 | `vbr_secret encrypt` 로 만든 `ENC[v1:aes-256-gcm:…]`. 평문은 거부한다(Q-A14). `vbr_secret` 은 표준 입력 첫 줄을 쓰고 줄 끝 CR·LF 와 앞의 UTF-8 BOM 을 뗀다(2026-11-18, PowerShell 파이프 대응) |
| `XAPI Keyring` | 아니오 | `vbr_agent.keyring` | 새 키 | 암호를 푸는 키링 파일. Linux 에서는 권한 600 이어야 한다 |
| `XAPI Verify SSL` | 아니오 | N | 새 키 | `Y` 면 XAPI 인증서를 검증한다. XenServer 기본 인증서는 자체 서명이라 기본은 N(R-AG-24) |
| `Backup Root` | 예 | - | 새 키 | 백업·복구 파일을 둘 수 있는 디렉터리(공백으로 여러 개). 운영 경로 `/BACKUP` 과 같게 마운트한다(DEC-AG-05). 밖의 경로는 거부(Q-A15) |
| `Allow Client` | 아니오(권장) | 모두 허용 + 경고 | 새 키 | 접속을 받을 관리 서버 IP(공백으로 여러 개)(Q-A06) |
| `Async Thread` | 아니오 | 3 | 원본 상수 DEFAULT_ASYNC_THREAD_CNT | 동시에 처리할 백업·복구 수. 2~10 |

원본에 있었으나 전환본이 읽지 않는 키: `PROGRAM`(중복 실행 검사, C-AG-06), `Interval Time`, `Retry Count`, `Mount Point`, `CPU Limit`(상태 감시, C-AG-15), `WEB server(IP)`, `WEB server(PORT)`(포털 이벤트, C-AG-15). 남아 있어도 오류가 아니다.

## 예

```
[VBR Agent for Xenserver]
PORT-NO          = 10003
MAX Log size     = 100000000
XAPI Address     = https://192.168.102.53
XAPI User        = root
XAPI Password    = ENC[v1:aes-256-gcm:k1:...]
XAPI Keyring     = /opt/ncc-vbr/agent/vbr_agent.keyring
XAPI Verify SSL  = N
Backup Root      = /BACKUP
Allow Client     = 10.10.1.20
Async Thread     = 3
```
