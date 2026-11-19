# vbr_vm 설치와 운영 안내

기준일: 2026-11-19 / 양식: T07 / 대상: `vbr_vm` 4.000 / 상태: **개발 검증 통과, 실기 전**. 운영 반영은 [06](06-test-plan-report.md)의 V2·V3 뒤에 한다.

## 1. 전환 전제

- [x] 개발 검증 통과(V0·V1·데모 자동 모드)
- [ ] 실기 시험 I01~I10 통과(06 6장)
- [ ] 관리 서버 또는 middle 이 새 API(05 문서)로 호출하도록 바뀜. `vbr_agent`의 문자열 전문은 받지 않는다
- [ ] 되돌리기(5장)를 시험 환경에서 한 번 실행

## 2. 배포물

| 파일 | 설치 위치(예) | 비고 |
|---|---|---|
| `vbr_vm` | `/opt/ncc-vbr/vm/vbr_vm` | `go\build.ps1 linux` 산출물 `go/bin/linux-amd64/vbr_vm`. 외부 라이브러리를 넣어 빌드한 실행 파일 하나(CGO 없음) |
| `vbr_secret` | `/opt/ncc-vbr/vm/vbr_secret` | 키링 만들기, 암호·토큰 암호화. 1단계와 같은 도구 |
| `sysconfig.yaml` | `/opt/ncc-vbr/vm/sysconfig.yaml` | [05](05-interface-spec.md) 10장 예를 고쳐 쓴다 |
| `key.yaml` | `/opt/ncc-vbr/vm/key.yaml` | `vbr_secret keygen`으로 만든다. 권한 600 |
| `ncc-vbr-vm.service` | `/etc/systemd/system/` | 아래 3장 |

## 3. 설치

```sh
# 1) 파일과 디렉터리
install -d -m 755 /opt/ncc-vbr/vm /var/lib/ncc-vbr/vm/tasks /var/log/ncc-vbr
install -m 755 vbr_vm vbr_secret /opt/ncc-vbr/vm/
cd /opt/ncc-vbr/vm

# 2) 키링과 토큰(평문이 명령 기록에 남지 않게 화면 입력으로 넘긴다)
./vbr_secret keygen key.yaml
head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' > /tmp/tok
./vbr_secret encrypt key.yaml sysconfig.api_auth.VBR_VM_API_TOKEN < /tmp/tok   # → ENC[...] 를 api_auth.VBR_VM_API_TOKEN 에
# 토큰 평문(/tmp/tok)은 관리 서버에 전달한 뒤 지운다:  shred -u /tmp/tok

# 3) sysconfig.yaml 작성(05 문서 10장). 확인할 것:
#    api_auth.enabled: true, crypto.enabled: true, backup.base_path: /BACKUP(NAS 마운트), task_store.dir, logging.*

# 4) NAS: /BACKUP 에 운영 NAS 를 마운트하고 쓰기 확인

# 5) 버전 확인과 기동
./vbr_vm -version
systemctl enable --now ncc-vbr-vm
curl -s http://127.0.0.1:8090/health
```

용도 문자열: `vbr_secret encrypt <키링>`은 용도를 빼면 XAPI 암호용(`ncc-vbr/xapi-password`, 호출자가 요청에 넣는 암호)이다. 설정 토큰은 셋째 인자로 `sysconfig.api_auth.VBR_VM_API_TOKEN` 또는 `sysconfig.api_auth.GATEWAY_INTERNAL_TOKEN`을 준다(2026-11-19 `vbr_secret`에 추가, 2단계 확인: 암호화한 토큰으로 기동·인증 성공). 환경 변수 `VBR_VM_API_TOKEN`(systemd `EnvironmentFile`, 권한 600)이 있으면 설정보다 우선한다.

`/etc/systemd/system/ncc-vbr-vm.service`:

```ini
[Unit]
Description=NCC-VBR VM Backup Service (vbr_vm)
After=network-online.target remote-fs.target
Wants=network-online.target

[Service]
WorkingDirectory=/opt/ncc-vbr/vm
ExecStart=/opt/ncc-vbr/vm/vbr_vm -config /opt/ncc-vbr/vm/sysconfig.yaml
EnvironmentFile=-/opt/ncc-vbr/vm/vbr_vm.env
Restart=on-failure
# 종료 때 처리 중 작업 정리를 최대 30초 기다린다(03 설계 6장)
TimeoutStopSec=60
KillSignal=SIGTERM

[Install]
WantedBy=multi-user.target
```

상대 경로(`./key.yaml`, `./data/tasks` 등)는 `WorkingDirectory` 기준이다.

## 4. 기존 vbr_agent 와 함께 운영

| 항목 | 내용 |
|---|---|
| 방식 | 같은 서버에서 다른 포트로 함께 돌릴 수 있다(DEC-VM-02). `vbr_agent`는 문자열 전문, `vbr_vm`은 JSON API |
| 겹치지 않는 것 | 포트, 설정 파일(`node_info.cfg` / `sysconfig.yaml`), 로그, 작업 기록 |
| 같이 쓰는 것 | `/BACKUP` NAS, 키링 형식(`vbr_secret`) |
| 주의 | 같은 VM 을 두 서비스가 동시에 백업해도 XenServer 는 허용한다(1단계 실기 E-03). 같은 파일 이름만 피한다(`vbr_vm`은 기본으로 덮어쓰지 않는다) |
| 기존과 같은 결과 | 요청에 `compression: "gzip"`, `snapshot_mode: "always"`를 넣는다(기본은 압축 없음, Halted VM 은 스냅샷 없이 내보냄. 04 C-VM-11·19) |

## 5. 되돌리기

| 되돌리는 조건 | 절차 | 되돌린 뒤 확인할 것 |
|---|---|---|
| `vbr_vm`이 백업·복구를 못 한다 | `systemctl stop ncc-vbr-vm`. 호출을 `vbr_agent`로 되돌린다 | `vbr_agent`가 받는지(1단계 07 문서) |
| 처리 중이던 작업 | 멈추면 `interrupted`로 남는다. 임시 스냅샷이 남을 수 있다 | `GET /api/v1/jobs?status=interrupted`의 `error.detail`, dom0 `xe snapshot-list snapshot-of=<vm>` |

`vbr_vm`이 만든 XVA 는 `vbr_agent`·XenCenter 로도 복구할 수 있다. 단 **zstd(`.xva.zst`)는 `vbr_vm`으로만 복구된다**(XenServer 가 zstd 를 모른다).

## 6. 운영

| 일 | 방법 |
|---|---|
| 상태 | `GET /health`(인증 없음): 판, 처리 중·대기 수, 인증·암호화 스위치 |
| 지금 도는 작업 | `GET /api/v1/jobs?status=running,queued` |
| 작업 하나 | `GET /api/v1/jobs/{taskId}`: 상태, 진행률, 단계, 결과, 오류 원문 |
| 취소 | `POST /api/v1/jobs/{taskId}/cancel`, 대기 중 전부는 `POST /api/v1/jobs/cancel-queued` |
| 설정 바꾸기 | `sysconfig.yaml`을 고쳐 저장하면 바로 반영(05 10장 "반영" 열). `server.port`, `server.listen`, `task_store.dir`은 재기동해야 반영되고 로그에 경고가 남는다. 잘못 고치면 이전 값을 그대로 쓰고 오류 로그를 남긴다 |
| 로그 수준 바꾸기 | `logging.level: debug`로 저장 → 바로 자세한 로그. 문제를 본 뒤 되돌린다 |
| 언어 | `locale: ko / en / ja`. 일본어는 네이티브 검수 전 |
| 작업 기록 | `task_store.dir/jobs.jsonl`. 1시간마다 정리, 끝난 지 `retention_days`(30일)가 지난 것은 지운다. 지우면 그 taskId 는 조회되지 않고 같은 taskId 가 다시 오면 새 작업으로 받는다 |
| 결과 보고 | `result_report.url`을 정하면 끝날 때 ncc-xen 규약 2 로 보낸다. 실패는 `outbox_pending.jsonl`에서 다시 보내고, 포기한 것은 `outbox_deadletter.jsonl`(30일 보관) |
| 암호 바꾸기 | 호출자가 새 암호를 `vbr_secret encrypt`로 만들어 보낸다. 서비스 재기동은 필요 없다 |
| 키링 바꾸기 | 키를 더한 `key.yaml`을 저장하면 다음 복호화부터 쓴다 |

## 7. 문제가 생겼을 때

| 증상 | 확인할 것 | 조치 |
|---|---|---|
| 기동하지 않음 `logging.time_format is required` | `logging` 절 | `level`을 적었으면 나머지 키도 다 적는다(05 10장 예) |
| 기동하지 않음 `VBR_VM_API_TOKEN is empty` | `api_auth.enabled: true`인데 토큰 없음 | 토큰을 넣거나(시험만) `enabled: false` |
| 기동하지 않음 `keyring load failed` | `crypto.keyring_file`, 권한 600 | 경로·권한 |
| 401 | `X-Internal-Token` | 호출자 토큰 |
| 400 `PLAINTEXT_PASSWORD_REJECTED` | 요청 암호가 평문 | `vbr_secret encrypt`로 ENC 값 |
| `CREDENTIAL_DECRYPT_FAILED` | 호출자가 다른 키링으로 암호화 | 같은 `key.yaml` |
| `XAPI_AUTH_FAILED` | 계정·암호 | 1단계 실기 R1 처럼 PowerShell 파이프 BOM 은 `vbr_secret`이 뗀다 |
| `XAPI_ERROR … DUPLICATE_MAC_SEED` | 원본 VM 이 같은 풀에 있는데 `restore_original_uuid: true` | `false`로 복구(1단계 실기 R4) |
| `FILE_EXISTS` | 같은 이름 파일 | 다른 이름 또는 `overwrite: true` |
| 완료됐는데 `snapshot_cleanup_failed` | 임시 스냅샷을 못 지움 | dom0 에서 그 uuid 를 확인하고 `xe snapshot-uninstall` |
| 재기동 뒤 `interrupted` | `error.detail`의 단계 | 다시 보낼지 정한다(자동 재개 없음). 남은 스냅샷 확인 |
| zstd 백업을 XenCenter 가 못 읽음 | 확장자 `.xva.zst` | `vbr_vm` restore 로 복구 |

## 8. 남은 일

| 일 | 조건 |
|---|---|
| 실기 시험 I01~I10, L01 | 06 6장 |
| Linux 에서 실행 파일 기동 확인 | Linux 서버 단계 |
| `auto`로 Halted VM 을 내보내는 동안 VM 시작 가능 여부(I10) | 실기 |

## 9. 전환 기록

| 일시 | 한 일 | 결과 | 누가 |
|---|---|---|---|
