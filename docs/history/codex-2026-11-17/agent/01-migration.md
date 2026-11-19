# XenServer Agent 전환 검토와 계획

2026-11-17 범위 결정에 따라 이번 전환을 취소했다. 이 문서의 설계와 단계표는 결정 이전의 검토 기록이다. CIFS와 Remove 전환 이후 Agent 전환 여부를 별도로 판단한다.

## 요구사항 및 위험

환경은 사용자 확인 기준 XenServer 9 시험 풀 `192.168.102.53`이며 배포는 dom0 로컬 프로세스로 잡는다. `ncc-xen` Go 백엔드의 `pkg/xapi/client.go Login/Call`, `pkg/xapi/mixins/vm.go Snapshot/TaskGetStatus/TaskCreate`, `internal/job/vm_backup_export.go runExport`, `vm_backup_restore.go runImport`는 XAPI와 작업 상태 처리의 참고 근거다. 백업 경로와 실행 호스트가 달라질 수 있는 원격 XAPI 방식은 별도 변경으로 둔다. Windows에서는 순수 Go 전문·파서 테스트를 지원하고 운영 Agent 검증은 dom0에서 한다. [XenServer 9 CLI](https://docs.xenserver.com/en-us/xenserver/9/command-line-interface.html)

| ID | 현행 근거 | 목표와 검증 관문 |
|---|---|---|
| AG-01 전문 | `agent/vbr_agent.c BACKUP_INFO:28–41, RealTimeMain:592–795, MessageProcessing:796–873` | 길이·헤더 byte 동등성, 분할·연속 전문, 길이 초과, 응답 인코딩과 연결 수명 |
| AG-02 수락·완료·조회 | `backup_receiver:1047`, `backup_async_sender:4216`, `mgt_processing:1368`, `query_task:3376` | ACK와 작업 완료·결과 전달을 각각 저장한다. 재접속 조회·중복 sequence·작업 재시작은 ASP 계약을 확인한다. |
| AG-03 조회 | `inquire_task:1560–2860` | TASK, STATUS, INFO, SR-INFO, LUN-INFO, DIR-INFO, XS-INFO, VM-INFO, RES-VM-INFO, EVENT-INFO를 각각 정상·빈 결과·실패 fixture와 비교한다. |
| AG-04 백업 | `async_processing:3608`, `backup_vm:3784`, `partial_backup_remove:3983`, `remove_snapshot_vm:4187` | snapshot → template 속성 변경 → 부분 백업 디스크 처리 → XVA export → 소유 스냅샷 정리. 부분 백업의 VDI 삭제는 원본 디스크와 스냅샷 귀속 검증이 필요하다. |
| AG-05 복구 | `import_vm:4119`, `validate_xva:5231` | SR과 파일 경계를 검사하고 정상·절단·손상 XVA, import 취소, 완료 후 VM 검증, 실패 정리를 확인한다. |
| AG-06 관리 | `health_check_processing:875`, `cancel_async_task:2862`, `cancel_task:2918`, `cancel_task_detail:2975`, `thread_task:3110` | 건강 상태·스레드 조정·취소 의미를 보존한다. Linux 자식 프로세스 중단과 Xen 서버 Task 취소를 구분한다. 상관 키와 소유가 확인된 작업만 직접 제어한다. |
| AG-07 실행 환경 | `env_initialize:382`, `dummy_call:4667`, `dummy2.c`, `agent/c1.sh`, `agent/bs_common.c` | 셸 없이 토큰화된 인자, 경로 제한, 포트 충돌 시 시작 실패, System V 공유 메모리 소비자 조사. 원본의 임의 셸 실행 경로를 그대로 열지 않는다. |

백업·복구는 시간이 많이 걸리는 핵심 구간이다. 스냅샷/VDI 귀속, XVA 출력 형식과 정리, 프로세스와 XAPI Task 취소, ASP 연결 유지가 서로 다른 계약이다. 디스크 삭제가 있는 PARTIAL과 snapshot uninstall은 모의 시험을 통과하고 격리 풀에서만 작업 ID 및 객체 소유를 확인한다. 시험 풀 접근 수단, 시험 VM·비운영 SR·쓰기 가능한 시험 XVA 폴더, 공유 메모리 외부 소비 여부는 아직 확인이 필요하다.

## 운영 표본과 전문

TCP는 7 ASCII 숫자의 본문 byte 길이 + 본문이다. 본문 고정 헤더 23 bytes는 `B(1) / sequence(17) / 처리구분·결과코드(1) / timeout(3) / priority(1)`이고 그 뒤가 payload다. 수신 분기는 큐에서 S(동기), A(비동기), Q/C/T/I(관리)로 이어진다. 성공 ACK는 25 bytes 본문에 결과 코드 `0`과 `OK`를 담는다. ACK는 원본 코드에서 확인했고 제공 로그에는 직접 캡처가 없다. 응답 전문에는 sequence로 상관관계를 남긴다.

운영 표본 `B20261117001001001A0035BACKUP …`은 A, timeout `003`, priority `5`이며 작업 성공 본문 헤더는 `B2026111700100100100035`다. 00:25:55.708의 `ASYNC Queue result send finished`는 내부 결과 큐에 넣은 성공, 다음 밀리초의 `ASYNC RESULT send error`는 TCP write 결과가 예상 길이와 달랐다는 뜻이다. 연결 종료, 부분 쓰기 등 정확한 원인은 로그만으로 확정할 수 없다. `003`은 전문 필드이며 이 비동기 작업이 3초 안에 끝난다는 계약은 관측되지 않는다. 표시된 `^@`와 줄 접힘은 로그 표현으로 취급하고 실제 wire에 요구되는지 ASP byte 표본으로 확인한다.

BACKUP 로그에 이어 XenServer 9 조회 출력과 ASP 전문 예시 Excel 경로를 받았다. [수신 자료 목록](03-received-evidence.md)에 확인 범위를 기록했다. 실제 TCP의 ACK·결과·재접속과 비 ASCII byte 계약을 검증하지 않은 상태다.

## 단계와 산출물

| 단계 | 전환 단위 | 통과 기준 |
|---|---|---|
| A1 기준선 | `docs_code/agent`에 본 문서 및 Xen 조회 표본 보강 | 상이한 전문/CLI 결과를 원본 근거와 대조 |
| A2 전문 | `go/internal/agent/protocol` | read fragmentation/coalescing, ACK/result fixture, 범위, raw bytes. 본 단계 구현 |
| A3 기동·큐 | 연결별 데드라인·send 직렬화, 실행/전달 상태, 영속 상태와 QUERY | 같은 sequence에서 재실행 중복 방지, ASP 재접속 확인, 포트 점유 오류 |
| A4 조회와 관리 | dom0 로컬 `xe` 명령 실행기와 파서, 나머지 조회/관리 | 샘플별 fixture, ARG·타임아웃, fake `xe`에서 원본 비교 |
| A5 XVA 업무 | 백업·부분 백업·복구·취소와 정리 저널 | 각 단계 fault injection, 소유 보장, 실제 XVA 복원 후 VM 확인 |
| A6 배포 | 비운영 풀과 ASP 테스트 인스턴스 | 원본과 결과 비교, 중복 실행 차단, 롤백과 잔여 객체 확인 |

이전 검토에서 작성한 전문 코드는 당시 산출물에 남아 있다. TCP 서버, 작업 큐, `xe` 연동 및 백업·복구 구현은 추후 별도로 전환을 결정할 때 검토한다. [XenServer 9 VM 가져오기/내보내기](https://docs.xenserver.com/en-us/xenserver/9/vms/import-export.html)
