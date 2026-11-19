# NCC-VBR Go 전환

2026-11-17 · 이번 Go 전환 범위는 CIFS 백업과 Remove(이전 버전 정리)다. 독립 배치 두 개를 기존 순서대로 CIFS → Remove로 전환한다. 기존 `docs_code`의 절차·시험 기록과 `go_code` 구현을 기준선으로 삼는다.

Agent의 이번 전환은 취소했다. XenServer dom0에서 VM 백업·복구를 수행하는 별도 서비스이며, CIFS와 Remove 이후 전환 여부를 결정한다. [Agent 검토 기록](agent/01-migration.md)과 [수신 자료 목록](agent/03-received-evidence.md)은 그 결정에 참고한다.

| 순서 | 업무 | 현재 산출물 | 다음 검증 |
|---|---|---|---|
| 1 | CIFS | 기존 Go 흐름과 `dis_` 재사용을 유지하고 원본 두 이름 공존 보호 추가 | NetApp/Dell EMC 시험 공유에서 rename, 복구, 부하 측정 |
| 2 | Remove | 기존 이력 정리 검토 및 기간 계산 보완 | 실제 NAS에서 LOG 모드 비교, 삭제는 격리 자료로 시험 |

- [CIFS 요건·현행·목표·운영 설계](cifs/01-analysis-and-design.md)
- [Remove 요건·전환·검증](remove/01-review.md)
- [재사용 절차·산출물 양식](templates/01-migration-workbook.md)
- [배치 교체와 운영 검증](cutover.md)
- [검증 실행 기록](verification.md)

원본은 `C:\dev\ncc-vbr(codex)`에 있다. 현재 실행 환경의 쓰기 경로 제한 때문에 수정 자료는 별도 산출물에 보관한다. 이번 배포에는 CIFS와 Remove의 Go 소스·빌드 스크립트만 포함한다. NAS 연동·운영 검증은 두 배치 각각 수행한다.
