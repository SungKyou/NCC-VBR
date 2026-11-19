# vbr_vm 변경·결정 대장

기준일: 2026-11-19 / 양식: T04(유형 B: 2장 기능 단위 변경 + 5장 결정) / 기준(원본): 기존 서비스 `vbr_agent` 3.000

1장(원본 결함)은 1단계 [agent/04](../agent/04-change-register.md)에 있고, 원본에서 새로 찾은 결함은 없다. 2단계 구현의 결함(실기에서 찾음)은 6장 D-VM-01~03. 3장(유지한 특이 동작)은 해당 없음. 1단계에서 유지했던 특이 동작(K-AG-01 실패에도 조회 코드 0 등)은 새 인터페이스에서 이어 가지 않는다(아래 C-VM-05).

## 2. 기존 대비 변경 (기능 단위)

| 번호 | 분류 | 기존(vbr_agent 3.000) | vbr_vm | 이유 | 밖에서 보이는가 | 승인 | 시험 |
|---|---|---|---|---|---|---|---|
| C-VM-01 | 구조 | 고정 길이 문자열 전문(TCP) | JSON API(HTTP), 대응은 [05](05-interface-spec.md) 11장 | 지시 1 | 예 | 사용자 2026-11-19 | T-VM-U01~ |
| C-VM-02 | 구조 | `node_info.cfg`, 기동 때만 읽음 | `sysconfig.yaml`, 즉시 반영 | 지시 6, DEC-VM-17 | 예 | 사용자 2026-11-19 | T-VM-U71 |
| C-VM-03 | 구조 | `server.log`, `backup.log`, `transaction-YYYYMM.log` | stdlogger 로그 1개 + 작업 기록(JSON Lines) | DEC-VM-06·15 | 예 | 사용자 2026-11-19 | T-VM-U73 |
| C-VM-04 | 부가 | 결과는 조회(Q)로만 | 조회 API + 결과 보고(설정 시) | 지시 5·9 | 예 | 사용자 2026-11-19 | T-VM-U50, U60 |
| C-VM-05 | 결함 수정 | 백업이 실패해도 조회 결과 코드 0, 본문 `[ERROR]…`(K-AG-01) | `status: failed` + `error.code` | 호출자가 실패를 본문 문자열로 알아내야 했다 | 예 | 사용자 2026-11-19(DEC-VM-06) | T-VM-U10 |
| C-VM-06 | 부가 | 처리 중 취소 거부 | XAPI task 취소 + 정리 | DEC-VM-09 | 예 | 사용자 2026-11-19 | T-VM-U51, I07 |
| C-VM-07 | 구조 | 재기동 때 대기 중이던 작업은 이어서 처리 | 대기·처리 중 모두 `interrupted`, 자동 재개 없음 | DEC-VM-22 | 예 | 수행자(위임: ncc-xen 우선) | T-VM-U53 |
| C-VM-08 | 삭제 | 우선순위 0~9 | 없음(접수 순) | DEC-VM-11 | 예 | 사용자 2026-11-19 | - |
| C-VM-09 | 구조 | `Allow Client` 주소 제한 | 토큰 인증, 주소 제한 없음 | DEC-VM-08·23 | 예 | 사용자 2026-11-19 / 수행자 | T-VM-U80 |
| C-VM-10 | 구조 | `Backup Root` 밖 경로 항상 거부 | `sandbox_mode`일 때만 거부(기본 꺼짐) | ncc-xen 규칙, 지시 4(검증하지 않음) | 예 | 수행자(위임: ncc-xen 우선) | T-VM-U25 |
| C-VM-11 | 부가 | 압축은 항상 gzip | 요청으로 none / gzip / zstd 선택, 없으면 none. **기존 호출을 그대로 옮기면 압축이 꺼지므로 호출자가 `compression: "gzip"`을 보내야 기존과 같다** | ncc-xen 17.8, Q-VM-02 | 예 | 사용자 2026-11-19 | T-VM-U23 |
| C-VM-12 | 부가 | 없음 | 내보내는 중 SHA256, 결과에 파일 크기·걸린 시간 | ncc-xen 17.2 | 예 | 수행자(위임) | T-VM-U20 |
| C-VM-13 | 부가 | XVA 검증 없음(1단계에서 보류) | verify action, 백업 뒤 검증 옵션 | 지시 11, DEC-VM-10 | 예 | 사용자 2026-11-19 | T-VM-U40, U41 |
| C-VM-14 | 결함 수정 | 복구 뒤 템플릿 플래그만 끔 | 플래그 끄기 + `VM.update_allowed_operations` | ncc-xen 17.8 ⑤: 플래그만 끄면 XenCenter Start가 비활성일 수 있다 | 예 | 수행자(위임) | T-VM-U30 |
| C-VM-15 | 부가 | 복구 옵션은 preserve와 SR뿐 | `name_label`, `home_server_uuid`, `force` | ncc-xen 17.4 | 예 | 수행자(위임) | T-VM-U32 |
| C-VM-16 | 구조 | 백업 파일 경로를 전체 경로 하나로 | `export_path` + `file_name`, 확장자 자동 | ncc-xen 17.1 | 예 | 수행자(위임) | T-VM-U24 |
| C-VM-17 | 구조 | 같은 이름 파일을 덮어씀 | `overwrite=true`일 때만 | DEC-VM-21 | 예 | 수행자(위임: ncc-xen 우선) | T-VM-U24 |
| C-VM-18 | 구조 | 접속 정보(마스터·계정·암호)를 설정 파일에. agent 하나가 풀 하나 | 요청마다 받음. 서비스 하나가 여러 풀 | 지시 4 | 예 | 사용자 2026-11-19 | T-VM-I01 |
| C-VM-19 | 구조 | Halted VM도 항상 임시 스냅샷 | 기본 `auto`: Halted·Suspended는 스냅샷 없이 내보냄. 요청으로 `always` | Q-VM-01 | 예 | 사용자 2026-11-19 | T-VM-U26, I10 |
| C-VM-20 | 구조 | 백그라운드 분리, `-foreground` | 항상 앞에서 실행 | DEC-VM-24 | 예 | 수행자(위임) | - |

## 4. 삭제한 기능

| 기존 기능 | 삭제 이유 | 승인 |
|---|---|---|
| 우선순위 | C-VM-08 | 사용자 2026-11-19 |
| 전문 `S`·`I`(상태·조회) | 1단계에서도 미지원. 지시 10 | 사용자 2026-11-19 |

## 5. 설계 결정 기록

DEC-VM-01~18은 [착수서](00-charter.md) 3장에 있다. 아래는 문서 작성 중에 정한 것이다.

| 번호 | 결정할 것 | 선택지 | 결정 | 근거 | 결정자·일자 | 영향 | 상태 |
|---|---|---|---|---|---|---|---|
| DEC-VM-19 | 접속 정보를 둘 곳 | `payload.options` 안 / `payload.credentials` / 헤더 | `payload.credentials{username, password}` | `options`는 ncc-xen에서 작업별 파라미터라 섞지 않는다. 헤더는 풀마다 다른 값을 싣기 어색하다 | 수행자(위임: ncc-xen 우선) 2026-11-19 | 05 8.3 | 확정 |
| DEC-VM-20 | 풀 식별 | ncc-xen처럼 `masterIp` 또는 `poolUUID` / `masterIp` 필수 | `masterIp` 필수, `poolUUID`는 기록만 | Redis가 없어 poolUUID로 마스터를 찾을 수 없다. 지시 4(검증하지 않음) | 수행자(위임) 2026-11-19 | 05 8.4 | 확정 |
| DEC-VM-21 | 같은 이름 파일 | 덮어씀(기존) / 실패(ncc-xen 기본) | ncc-xen: `overwrite` 기본 false | 지시 8. 백업 파일을 실수로 덮어쓰지 않는다 | 수행자(위임) 2026-11-19 | C-VM-17 | 확정 |
| DEC-VM-22 | 재기동 때 대기 중이던 작업 | 이어서 처리(기존) / `interrupted`(ncc-xen) | ncc-xen | 지시 8. 호출자가 다시 보낼지 정한다. 오래 기다린 작업이 재기동 뒤 갑자기 도는 일을 막는다 | 수행자(위임) 2026-11-19 | C-VM-07 | 확정 |
| DEC-VM-23 | 접속 주소 제한 | `Allow Client` 유지 / 토큰만 | 토큰만 | ncc-xen에 없다. 방화벽으로 막을 수 있다 | 수행자(위임) 2026-11-19 | C-VM-09 | 확정 |
| DEC-VM-24 | 실행 방식 | 백그라운드 분리(1단계 표준) / 앞에서 실행 | 앞에서 실행 | ncc-xen과 같다. systemd가 감시한다 | 수행자(위임) 2026-11-19 | 05 2장 | 확정 |
| DEC-VM-25 | 기본 포트 | - | 8090 | ncc-xen backend 기본(8080대)과 겹치지 않고 1단계 agent(10003 등)와도 다르다. 설정으로 바꾼다 | 수행자 2026-11-19 | 05 10장 | 확정 |
| DEC-VM-26 | 조회 API 모양 | 상태만 / 규약 2와 같은 payload를 포함 | 규약 2 payload 포함 + `status`·`percent`·`step` | 나중에 결과 보고(밀기)와 조회(당기기)를 같은 구조로 처리할 수 있다 | 수행자 2026-11-19 | 05 API-VM-02 | 확정 |
| DEC-VM-27 | 요청 암호의 복호화 용도 | 새 용도 / 1단계와 같은 `ncc-vbr/xapi-password` | 1단계와 같음 | 1단계 `vbr_secret encrypt`로 만든 값을 그대로 쓴다. 호출자가 같은 키링으로 암호화해 보낸다 | 수행자 2026-11-19 | 03 8장 | 확정 |
| DEC-VM-28 | 보안 권고와 30일 기준이 맞부딪칠 때 | 30일 기준 판 유지 / 권고를 고친 판 | **권고를 고친 가장 이른 판**. govulncheck(2026-11-19)가 호출되는 취약점 6건을 찾음: Go 표준 라이브러리 `net/http` GO-2026-6859(go1.27.3 → 고친 판 go1.27.4, 2026-11-10 출시로 30일 전), x/net 4건(v0.51.0 → 30일 기준 최신 v0.61.0이 고친 판 이상), quic-go GO-2026-5676(v0.59.0 → 30일 기준 최신 v0.64.1). 결과 toolchain go1.27.4, `go` 줄 1.26.0(x/net v0.61.0 의 요구). 다시 검사해 호출되는 취약점 0건 | 표준 구조 5장 12번의 "보안 권고가 없어야 한다" 조건 | 수행자(위임: 원칙 적용) 2026-11-19 | 4개 서비스 모두 go1.27.4 로 빌드. 회귀 시험 재실행 통과(06 5장) | 확정. 표준 구조 5장 12번에 우선순위를 적었다 |
| DEC-VM-29 | 내보내기 오류 두 가지 | - | ① 스냅샷 uuid 를 대상으로 `disks`를 주면 `INVALID_FIELD`(작업 단계) — 사용자 스냅샷의 디스크를 지우지 않으려고. ② `auto`에서 Running·Halted·Suspended 가 아닌 전원 상태(Paused 등)는 `VM_STATE_UNSUPPORTED`(ncc-xen 도 실패) | 자료 보호(01 N05), ncc-xen 17.2 | 수행자 2026-11-19 | 05 9장 | 확정 |
| DEC-VM-30 | 복사한 stdlogger 의 필수 키 | 기본값을 채우게 고침 / 그대로 | 그대로. `logging.level`이 있으면 나머지 키도 다 적어야 한다(ncc-xen 과 같은 운영 규칙) | ncc-xen 과 같은 동작 유지 | 수행자(위임: ncc-xen 우선) 2026-11-19 | 05 10장 예, 데모 설정, 07 운영 | 확정 |
| DEC-VM-31 | locale 감시 | i18n `watch.go` 복사(viper 감시 하나 더) / vmconfig 가 SetLocale | vmconfig 가 설정 변경을 받아 `i18n.SetLocale`. stdlogger 는 원본처럼 자기 감시를 쓴다 | 감시를 줄이고 설정 해석을 한곳에 | 수행자 2026-11-19 | 08 CP-VM-10 | 확정 |
| DEC-VM-32 | 결과 보고 실패 본문 | ncc-xen sendFailure(message 에 오류) / 필드 | `payload.options`에 `error_code`, `error_detail` 을 더하고 `message`는 locale 문구 | 호출자가 코드로 분기한다(05 9장) | 수행자 2026-11-19 | 05 API-VM-07 | 확정 |
| DEC-VM-33 | 설정 토큰 암호화 도구 | ncc-xen `ncc-crypto` 복사 / 1단계 `vbr_secret`에 용도 인자 | `vbr_secret encrypt <키링> [용도]`. 용도는 목록(`ncc-vbr/xapi-password` 기본, `sysconfig.api_auth.VBR_VM_API_TOKEN`, `sysconfig.api_auth.GATEWAY_INTERNAL_TOKEN`) 밖이면 거부. 인자 두 개짜리 기존 사용법은 그대로 | 운영 안내를 쓰다 발견: 토큰을 ENC 로 만들 방법이 없었다. ncc-crypto 는 대화형 메뉴·sysconfig 직접 수정까지 해 범위가 크다 | 수행자 2026-11-19 | `go/cmd/vbr_secret`(4개 서비스 공용), 07 3장. 시험: `TestReadPassword` 통과, 암호화 토큰으로 vbr_vm 기동·인증 확인 | 확정 |
| Q-VM-01 | export 스냅샷 정책 기본값 | `auto`(ncc-xen: Running만 스냅샷, Halted는 그대로 내보냄) / `always`(기존: 항상 스냅샷) | **`auto`**. 요청의 `options.snapshot_mode`로 `always`를 고를 수 있다. 디스크 선택 백업은 항상 스냅샷. 설정 기본값 키는 두지 않는다(ncc-xen에도 없음) | 아래 판단 자료 | 사용자 2026-11-19 | 05 export options, 03 3장, C-VM-19 | 확정 |
| Q-VM-02 | export 압축 기본값 | `none`(ncc-xen 기본) / `gzip`(기존, XenCenter 호환) / `zstd`(빠름, XenCenter 비호환) | **요청의 `options.compression`을 따르고, 없으면 `none`**(ncc-xen과 같음). 설정 기본값 키는 두지 않는다 | 사용자 답변 "요청에 따릅니다. 요청시 백업 방식을 받고, 없는 경우는 none" | 사용자 2026-11-19 | 05 export options·10장, C-VM-11 | 확정 |
| Q-VM-03 | 진행 중 진행률 계산(실기 I06 에서 발견) | A. 그대로: XAPI export task progress(ncc-xen 같은 `progress*98+1`) / B. 받은 바이트 ÷ VM 디스크 사용량 합계(압축이면 근사) | **결정 대기**. 수행자 권장 B, 실기 시험을 마친 뒤 고친다 | 실기에서 none 백업이 25~26% 까지만 오르다 끝에서 99% 로 건너뛰었다. 조회 API 의 percent 가 실제 진행을 나타내지 못한다 | 사용자 | 05 API-VM-02 percent, xva `pollTask` | 대기 |

결정 전 판단 자료(2026-11-19 기록, 보존). 수행자 권장은 `always`·`gzip`이었고 사용자가 `auto`와 "요청에 따름, 없으면 none"으로 정했다. `auto`에서 내보내는 동안 VM을 켤 수 있는지는 실기 시험 I10으로 확인해 운영 안내에 적는다.

Q-VM-01 판단 자료

| | `auto` | `always` |
|---|---|---|
| Halted VM 백업 | 스냅샷 없이 바로 내보냄. 스냅샷 생성·삭제 시간이 없고, SR에 임시 디스크가 생기지 않는다 | 기존과 같이 스냅샷을 만들고 지운다 |
| 내보내는 동안 VM | 내보내기 작업이 VM을 잡고 있어 그동안 VM 시작이 막힐 것으로 본다(**추정, 미확인** — 실기 시험 I10으로 확인). 백업 중 VM을 켜야 하는 운영이면 불편 | 스냅샷을 내보내므로 VM은 자유롭다(1단계 실기) |
| MAC 보존 복구 VM 이름·uuid | 원본 VM의 이름·uuid가 된다 | 스냅샷 이름·uuid가 된다(실기 PT-02) |
| 특징 | ncc-xen 방식(지시 8). 기존 운영과 다르다 | 기존 운영 방식. 1단계 실기로 확인됨 |
| **수행자 권장** | | **`always`**. 백업 중에도 VM을 켤 수 있어야 하는 운영에서 안전하고, `auto`의 VM 잠김 여부가 아직 확인되지 않았다(I10). 요청마다 `auto`를 고를 수 있다 |

Q-VM-02 판단 자료

| | none | gzip | zstd |
|---|---|---|---|
| 파일 크기(이 VM, 실 데이터 약 5.6 GiB) | 약 6.0 GB | 2.59 GB(1단계 실기) | ncc-xen 실측 gzip보다 약 2% 작음 |
| 시간 | 네트워크만 | 마스터 CPU가 압축(1단계 실기 4분 30초, ncc-xen 실측 241초) | 이 서비스의 CPU가 압축(ncc-xen 실측 72초, 3.35배 빠름. 동시 압축으로 바꾼 뒤 재측정 전) |
| XenCenter로 복구 | 됨 | 됨 | **안 됨**(이 서비스로만) |
| 맞는 경우 | 디스크가 넉넉할 때 | 다른 도구(XenCenter)로도 복구해야 할 때 | 속도가 중요하고 복구를 이 서비스로만 할 때 |
| **수행자 권장** | | **`gzip`**. 기존과 같고, 이 서비스가 없어도 XenCenter로 복구할 수 있어 재해 복구에 안전하다. 요청마다 `zstd`를 고를 수 있다 | |

## 6. 실기에서 찾은 전환본 결함 (2026-11-19)

1단계 대장의 1장(원본 결함)과 달리, 2단계 구현의 결함이다. 모두 실기 시험 중 찾아 고쳤고 회귀 시험을 더했다. 근거는 [06](06-test-plan-report.md) 5장·5.1.

| 번호 | 시험 | 증상 | 원인 | 조치 | 회귀 시험 |
|---|---|---|---|---|---|
| D-VM-01 | 실기 준비 | Windows 에서 `logging.time_zone: "Asia/Seoul"`로 기동 실패 `invalid logging.time_zone` | Windows 에 IANA 시간대 자료가 없고 실행 파일이 Go 의 `zoneinfo.zip`도 찾지 못한다. 데모 자동 모드 설정은 `local`이라 잡지 못했다 | `cmd/vbr_vm/main.go`에 `_ "time/tzdata"`(실행 파일 약 450 KB 증가). tzdata 가 없는 Linux 에도 같은 효과 | 같은 설정으로 기동·`/health` 확인(수동) |
| D-VM-02 | I07 | 처리 중 취소는 `canceled`인데 임시 스냅샷이 풀에 남음. `VDI.destroy failed: VDI_IN_USE` | `task.cancel` 직후 바로 지웠다. XenServer 는 내보내기를 멈추는 동안 스냅샷 디스크를 더 잡고 있다. 가짜 XAPI 는 이 상태를 흉내 내지 않았다 | `xva.settleIfCanceled`: 취소된 export·import 는 XAPI task 가 pending·cancelling 을 벗어날 때까지(최대 60초) 기다린다. `vm.uninstall`: `VDI_IN_USE`·`OTHER_OPERATION_IN_PROGRESS`면 2초 간격, 최대 2분 다시 지운다. 공통 `xapi` 패키지(1단계 agent 와 공유)는 바꾸지 않았다 | `TestCancelSnapshotInUse`(가짜 XAPI `Cancelling`·`VDIInUse`). 실기 I07 2차 통과 |
| D-VM-03 | I09 | 강제 종료 뒤 `interrupted` 결과에 남은 임시 스냅샷 uuid 가 없음 | REQ-VM-17·05 9장(INTERRUPTED detail 에 남은 스냅샷)과 05 API-VM-02 `snapshot_left`를 구현하지 않았다 | 임시 스냅샷을 만들면 바로 작업 기록 `result.snapshot_left`에 uuid 를 적는다. 재기동 때 남아 있으면 detail 에 `; temporary snapshot may remain: <uuid>`. 정상·실패·취소로 끝나면 최종 결과가 덮어써 사라진다 | `TestReopenMarksInterrupted`, `TestCancelSnapshotInUse`(처리 중 조회에 `snapshot_left`, 끝나면 없음) |

시험 스크립트(`demo.ps1`) 결함도 실기 중 고쳤다: 키링 경로 기본값·검사 없음(빈 암호를 보냄), 복구·검증 `file_path` 기본값 없음, 메뉴 6 이 압축·스냅샷을 묻지 않음, 메뉴 8 없는 SR 케이스에 없는 파일을 넣음. 06 5장 실행 기록.
