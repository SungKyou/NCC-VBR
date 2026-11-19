# agent 전환 계획과 운영 안내

기준일: 2026-11-17 / 양식: T07 / 상태: 초안. 시험 풀 통합 시험(06 문서 5장) 뒤 확정한다.

## 1. 바뀌는 운영 모습

| 항목 | 지금(원본 1.602) | 전환 뒤 |
|---|---|---|
| 설치 위치 | 풀의 호스트마다 dom0 | agent 서버(Linux) 한 대. 풀마다 프로세스 하나 |
| 호스트 업데이트 | 업데이트 뒤 dom0 에 다시 설치 | 영향 없음 |
| 관리 서버 설정 | 호스트 IP:포트 | agent 서버 IP:포트(같은 풀의 호스트는 같은 포트) |
| 백업 경로 | dom0 에 마운트한 `/BACKUP` | agent 서버에 같은 NAS 를 `/BACKUP` 으로 마운트 |
| 자료 흐름 | dom0 → NAS | 풀 마스터 → (HTTPS) → agent 서버 → NAS. 시간이 더 걸릴 수 있다(DEC-AG-06) |
| 필요한 것 | 없음(dom0 root) | 풀 계정·암호(암호화 보관) |
| 지원 요청 | S, A, Q, C, T, I | A(BACKUP, PARTIAL, RESTORE), Q, C. 나머지는 결과 코드 9 |

## 2. 설치

```sh
# 1) 파일 (go\build.ps1 linux 의 산출물 bin/linux-amd64/)
mkdir -p /opt/ncc-vbr/agent && cd /opt/ncc-vbr/agent
cp vbr_agent vbr_secret .
./vbr_agent -version            # "Xenserver Agent for NCC-VBR version = 3.000" 이 나오는지(운영 OS 에서 실행 확인)

# 2) 암호
./vbr_secret keygen /opt/ncc-vbr/agent/vbr_agent.keyring     # 권한 600 으로 만든다. 다른 곳에 백업해 둔다
echo '<풀 root 암호>' | ./vbr_secret encrypt /opt/ncc-vbr/agent/vbr_agent.keyring
#   → ENC[v1:aes-256-gcm:k1:...] 를 설정 파일에 넣는다. 명령 기록(history)에 암호가 남지 않게 주의

# 3) 설정: node_info.cfg (09-config-spec.md)
# 4) NAS: /BACKUP 에 운영과 같은 NAS 를 마운트하고 쓰기 확인
# 5) 기동
./vbr_agent -home /opt/ncc-vbr/agent       # 백그라운드로 분리된다. 앞에서 보려면 -foreground
```

키링 파일을 잃으면 암호를 풀 수 없다. 키링과 설정 파일을 함께 보관한다.

## 3. 전환 절차(풀 하나 기준)

| 순서 | 할 일 | 확인 | 되돌리기 |
|---|---|---|---|
| 1 | 시험 풀에서 06 문서 5장 PT-01~04, IT-01~07 통과 | 시험 결과 문서 | - |
| 2 | 운영 풀용 agent 서버 설치(2장), 다른 포트로 기동 | `server.log` 에 "XAPI login success"(첫 작업 때), 수신 포트 열림 | 프로세스 종료 |
| 3 | 원본 agent 에 진행 중인 작업이 없을 때(조회로 확인) 관리 서버 설정을 agent 서버 IP:포트로 바꾼다 | 관리 서버에서 시험 백업 1건 | 관리 서버 설정을 원래 호스트로 되돌린다 |
| 4 | 하루 이상 운영 백업을 받는다. 원본 agent 는 dom0 에 그대로 둔다(병행 대기) | 결과 조회 0 비율, `backup.log`, NAS 파일 크기 | 3번의 되돌리기 |
| 5 | 원본 agent 중지(dom0) | - | dom0 에서 다시 기동 |

결과 조회는 transaction 로그로 답한다(K-AG-01). 전환 전에 접수한 작업의 결과는 원본 agent(dom0)의 로그에 있으므로, 전환 직전 작업이 끝났는지 원본 쪽에서 확인한다.

## 4. 운영

| 일 | 방법 |
|---|---|
| 중지 | `kill -TERM <pid>`. 처리 중이던 작업은 XAPI task 를 취소하고, 다음 기동 때 그 작업의 조회 결과가 6 이 된다 |
| 재기동 | 2장 5) 와 같다. 대기 중이던 작업은 이어서 처리한다 |
| 중복 기동 | 포트를 쓰고 있으면 "listen failed" 로 끝난다(원본처럼 다른 프로세스를 죽이지 않는다) |
| 로그 | `server.log`(동작), `transaction-YYYYMM.log`(전문, 결과 조회가 읽는다. 지우면 그 달 조회가 9 가 된다), `backup.log`(단계) |
| 로그 정리 | `transaction-YYYYMM.log` 는 지난 달 것만 옮긴다. 이번 달 것을 옮기면 진행 중 작업을 조회할 수 없다 |
| 암호 변경 | 2장 2) 의 encrypt 로 새 값을 만들어 설정을 바꾸고 재기동 |
| 상태 파일 | `vbr_agent.state`(처리 중·대기 작업). 지우지 않는다 |
| 복구 preserve=true | MAC(과 VM uuid)을 보존한다. 원본 VM 이 아직 있는 풀에서는 XenServer 가 `DUPLICATE_MAC_SEED` 로 거부한다(2026-11-18 실기 확인, 원본 `xe vm-import preserve=true` 도 같다고 봄). 원본 VM 을 지운 뒤나 다른 풀로 옮길 때 쓴다. 같은 풀에서 사본이 필요하면 `false` |
| 복구 VM uuid | preserve=true 면 백업 때의 스냅샷 uuid 가 된다. 같은 백업 파일을 같은 풀에 true 로 두 번 복구할 수 없다(uuid 중복). VM 이름은 백업 요청의 스냅샷 이름 |
| 백업 크기·시간 | VDI `physical-utilisation` 으로 가늠할 수 없다(스냅샷 체인이 있으면 맨 끝만 센다). 실기: 체인 데이터 약 5.6 GiB → 압축 2.59 GB, 원격 약 4분 30초(PC 1대, NFS SR) |
| 암호 입력(Windows) | PowerShell 파이프로 `vbr_secret encrypt` 에 넘기면 UTF-8 BOM 이 붙을 수 있다. 2026-11-18 이후 판은 뗀다. 이전 판으로 만든 값은 다시 만든다 |

## 5. 장애 대응

| 증상 | 확인 | 조치 |
|---|---|---|
| 모든 백업이 `[ERROR]…` | `server.log` 의 XAPI 오류 | 풀 마스터 주소·계정·암호, 443 포트, 마스터 전환 |
| `[ERROR]Export File Name is not allowed.` | 요청의 파일 경로 | `Backup Root` 설정 또는 관리 서버의 경로 |
| 백업 파일이 `.partial` 로 남음 | 내보내기 중 중단 | 다음 같은 이름 백업이 지운다. 수동 삭제 가능 |
| 복구가 `[ERROR]XAPI task failure: DUPLICATE_MAC_SEED: …` | 같은 mac_seed 의 VM(대개 원본 VM)이 풀에 있음 | preserve=false 로 복구하거나 원본 VM 정리 뒤 다시 |
| agent 강제 종료 뒤 스냅샷이 남음 | `xe snapshot-list snapshot-of=<vm>` 에 요청 이름의 스냅샷, `xe task-list` 에 `failure` export task | 스냅샷은 수동 삭제(Q-A17). task 기록은 자원을 쓰지 않으며 세션 만료 때 정리된다 |
| SR 에 스냅샷 디스크가 남음 | `xe vdi-list is-a-snapshot=true` | 정리 실패 기록(`backup.log` 의 `SNAPSHOT destroy … failed`) 확인 뒤 수동 삭제 |
| 접속이 바로 끊김 | `server.log` "Connection refused (not in Allow Client)" | `Allow Client` 에 관리 서버 IP 추가 |

## 6. ncc-xen 변경 추적

복사한 코드는 ncc-xen 이 고쳐도 따라오지 않는다([08 문서](08-copied-code-register.md)). 분기마다 한 번:

```sh
git -C /c/dev/ncc-xen log --oneline 043c6d5..HEAD -- backend/pkg/xapi backend/internal/job/vm_backup_export.go backend/internal/job/vm_backup_restore.go stdcrypto/secret
```

해당 변경이 있으면 08 문서 4장에 적고 반영 여부를 정한다.
