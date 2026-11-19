# 명세서 문서 도구 (tools/spec)

vbr_vm Job Type 표준 명세서(Word)를 만든다. 서식은 NCC-Xen Job Type 표준 명세서 v3.36 과 같다.

| 파일 | 역할 |
|---|---|
| `vm_spec_source.md` | **원문. 명세 내용은 여기서만 고친다** |
| `build_vm_spec.py` | 원문 → `docs/vm/NCC-VBR_VM_JobType_표준명세서_v<판>.docx` |
| `template/` | 스타일·머리말·꼬리말·테마. NCC-Xen `docs/NCC-XEN_JobType_표준명세서_v3.36.docx`(@043c6d5)에서 복사(docs/vm/08 CP-VM-14). 주석 파트와 작성자 정보는 뺐다 |

## 만들기

저장소 최상위에서(Python 3 표준 라이브러리만 필요):

```powershell
python -X utf8 tools\spec\build_vm_spec.py
```

## 판을 올릴 때

1. `vm_spec_source.md` 첫머리의 `version`, `date`, `isodate`, `header`, `output`(파일 이름의 판)을 고친다.
2. 표지 요약 표의 버전·검증 상태, 12장 버전 이력, 바뀐 절을 함께 고친다.
3. 내용의 근거는 `docs/vm/05-interface-spec.md`(인터페이스)다. 05 를 먼저 고치고 명세서에 옮긴다.
4. 만든 뒤 Word 로 열어 확인한다(표 너비는 칸 내용 길이로 자동 계산한다).

## 원문 문법

마크다운의 일부만 쓴다: 첫머리 `--- … ---`(표지 값), `#`/`##`/`###` 제목, `| … |` 표(첫 줄 머리행, 칸 안의 글자 `|`는 `\|`), ```` ``` ```` 코드 상자, 빈 줄로 나눈 문단, 문단·표 안의 `` `코드` ``와 `**굵게**`. 첫 제목 앞의 표는 표지 아래 요약 표가 된다.
