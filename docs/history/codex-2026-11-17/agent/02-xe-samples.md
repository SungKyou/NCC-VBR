# XenServer 9 dom0 조회 표본 12개

2026-11-17 보존 기록. 사용자가 조회 명령과 결과를 전달했다. 현재는 [수신 자료 목록](03-received-evidence.md)을 참조한다. 이 명령 목록은 추후 표본의 출처를 대조하는 데 사용한다.

시험 풀 호스트 콘솔에서 순서대로 실행한다. `HOST_UUID`, `VM_UUID`는 2·3번 출력의 시험 호스트와 비운영 VM 값으로 대체하고, `VDI_UUID`는 6번 disk 값으로 대체한다. UUID를 바꿔 공유하는 경우 같은 객체는 모든 출력에서 같은 값으로 바꾼다. 이름과 주소는 마스킹할 수 있으며 필드명, 정렬, 공백, 빈 값, 줄바꿈, 종료 코드, stderr는 유지한다. 실행 중 출력이 없으면 빈 출력임을 기록한다.

```sh
# 1. INFO: 풀의 식별과 이름
xe pool-list params=uuid,name-label

# 2. INFO: 호스트와 주소
xe host-list params=uuid,name-label,address

# 3. INFO: 일반 VM 목록
xe vm-list params=uuid,name-label,resident-on,networks,os-version,power-state is-a-template=false is-control-domain=false

# 4. SR-INFO: 기본 SR
xe pool-list params=uuid,default-SR

# 5. SR-INFO: SR별 물리 사용량과 종류
xe sr-list params=uuid,host,name-label,physical-utilisation,physical-size,type

# 6. LUN-INFO: VM의 디스크 슬롯과 VDI
xe vbd-list vm-uuid=VM_UUID type=Disk params=uuid,userdevice,vdi-uuid

# 7. LUN-INFO: VDI 이름과 가상 크기
xe vdi-list uuid=VDI_UUID params=name-label,virtual-size

# 8. XS-INFO: 버전, CPU, 메모리 및 에디션
xe host-list params=name-label,software-version,cpu_info,address,memory-total,memory-free,edition uuid=HOST_UUID

# 9. VM-INFO: 상세 VM
xe vm-list params=name-label,power-state,memory-actual,VCPUs-max,install-time,networks,os-version uuid=VM_UUID

# 10. RES-VM-INFO: 대상 호스트에 상주하는 VM
xe vm-list params=uuid,name-label is-a-template=false is-control-domain=false resident-on=HOST_UUID

# 11. 백업/정리 계획용: 시험 VM의 기존 스냅샷
xe snapshot-list snapshot-of=VM_UUID

# 12. 결과 조회/취소 계획용: 서버 Task 상태
xe task-list params=uuid,name-label,status,progress
```

1–10번은 원본 `agent/vbr_agent.c`의 1890, 1924, 1976, 2146, 2179, 2303, 2333, 2489, 2586, 2766행에 대응한다. 11–12번은 스냅샷·작업 수명 확인용이다. 10번의 정지 VM 분기도 수집할 경우 `resident-on=HOST_UUID` 대신 `power-state=halted`를 사용한다. 위 목록은 조회 명령이다. [CLI 명령 및 필드](https://docs.xenserver.com/en-us/xenserver/9/command-line-interface.html), [스냅샷 조회 예시](https://docs.xenserver.com/en-us/xenserver/9/dr/snapshots)
