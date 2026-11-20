# NCC-VBR 개발 인계서

개발팀에 전달하는 서비스별 인계서와 공통 소스 설명이다. 기준일 2026-11-20, 기준 커밋 9deb702.

| 문서 | 대상 | Word |
|---|---|---|
| [01 CIFS 서비스](01-cifs.md) | vbr_cifs 3.0000 — NAS 증분 백업(배치) | [word/01_CIFS_서비스_개발인계서.docx](word/01_CIFS_서비스_개발인계서.docx) |
| [02 Remove 서비스](02-remove.md) | vbr_remove 2.000 — 이전 버전 정리(배치) | [word/02_Remove_서비스_개발인계서.docx](word/02_Remove_서비스_개발인계서.docx) |
| [03 Agent 서비스](03-agent.md) | vbr_agent 3.000 — VM 백업 · 복구(TCP 전문, 1단계) | [word/03_Agent_서비스_개발인계서.docx](word/03_Agent_서비스_개발인계서.docx) |
| [04 VM 서비스](04-vm.md) | vbr_vm 4.000 — VM 백업 · 검증 · 복구(JSON API, 2단계) | [word/04_VM_서비스_개발인계서.docx](word/04_VM_서비스_개발인계서.docx) |
| [05 공통 구성 · 소스](05-common-source.md) | 저장소 폴더, 공통 패키지, 설정 · 로그 · 암호화, 빌드 · 시험, 이전 안내 | [word/05_공통구성_소스설명서.docx](word/05_공통구성_소스설명서.docx) |

각 서비스 인계서는 같은 다섯 장으로 되어 있다.

1. 서비스 구성과 흐름(흐름도)
2. 설정 · 키 · 로그 · 암호화(sysconfig / key.yaml / stdlogger 또는 node_info.cfg / logx)와 사용 방법
3. 기능별 호출(API · 명령행 · 전문) 상세, 호출 예, 결과 분석 방법
4. 폴더와 소스 파일별 설명
5. 소스 이전 시 분석에 도움이 되는 설명

vbr_vm 의 외부 계약(API) 정식 명세는 [NCC-VBR_VM_JobType_표준명세서_v1.00.docx](../vm/NCC-VBR_VM_JobType_표준명세서_v1.00.docx)다.

## 다시 만들기

원문은 이 폴더의 Markdown 이고 Word 는 생성물이다. 내용은 Markdown 에서 고친다.

```powershell
python -X utf8 tools\handoff\diagrams.py                     # 흐름도 PNG(img/)
python -X utf8 tools\spec\build_vm_spec.py docs\handoff\01-cifs.md docs\handoff\02-remove.md docs\handoff\03-agent.md docs\handoff\04-vm.md docs\handoff\05-common-source.md
```