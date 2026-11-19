"""vbr_vm Job Type 표준 명세서(docx) 생성기.

원문 tools/spec/vm_spec_source.md 를 NCC-Xen Job Type 표준 명세서와 같은 서식의 Word 파일로 만든다.
서식(스타일, 머리말·꼬리말, 테마)은 tools/spec/template/ 에 있다. NCC-Xen
docs/NCC-XEN_JobType_표준명세서_v3.36.docx(@043c6d5)에서 복사했다(docs/vm/08 CP-VM-14).
표지·표·코드 상자의 글꼴·색·여백은 같은 문서의 본문 XML 값을 그대로 쓴다.

사용(저장소 최상위에서):  python -X utf8 tools/spec/build_vm_spec.py
필요한 것: Python 3 표준 라이브러리만.

원문 문법(마크다운의 일부):
  첫머리 --- … ---   표지 값(product, title, subtitle, version, date, isodate, header, footer, output)
  # / ## / ###       제목 1·2·3
  | … |              표(첫 줄은 머리행, 둘째 줄은 구분선). 칸 안의 \\| 는 글자 |
  ```                코드 상자(회색, Consolas)
  빈 줄로 나눈 줄     문단.  `코드` 와 **굵게** 를 쓸 수 있다
  첫 번째 표(제목 앞)  표지 아래 요약 표
"""
import os
import re
import sys
import zipfile
from xml.sax.saxutils import escape

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
HERE = os.path.dirname(os.path.abspath(__file__))
TEMPLATE = os.path.join(HERE, "template")
SOURCE = os.path.join(HERE, "vm_spec_source.md")

TABLE_W = 9360  # NCC-Xen 본문 표 너비(dxa)
NS = ('xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" '
      'xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" '
      'xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml" '
      'xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" mc:Ignorable="w14"')


def rpr(color="222222", size=None, bold=False, mono=False):
    s = "<w:rPr>"
    if mono:
        s += '<w:rFonts w:ascii="Consolas" w:eastAsia="맑은 고딕" w:hAnsi="Consolas" w:cs="Consolas"/>'
    else:
        s += '<w:rFonts w:eastAsia="Arial"/>'
    if bold:
        s += "<w:b/><w:bCs/>"
    if color:
        s += f'<w:color w:val="{color}"/>'
    if size:
        s += f'<w:sz w:val="{size}"/><w:szCs w:val="{size}"/>'
    return s + "</w:rPr>"


def run(text, **kw):
    return f'<w:r>{rpr(**kw)}<w:t xml:space="preserve">{escape(text)}</w:t></w:r>'


def inline(text, color="222222", size=None, bold=False):
    """`코드` 와 **굵게** 를 런으로 나눈다."""
    out = []
    for part in re.split(r"(`[^`]+`|\*\*[^*]+\*\*)", text):
        if not part:
            continue
        if part.startswith("`") and part.endswith("`"):
            out.append(run(part[1:-1], color=color, size=size, bold=bold, mono=True))
        elif part.startswith("**") and part.endswith("**"):
            out.append(run(part[2:-2], color=color, size=size, bold=True))
        else:
            out.append(run(part, color=color, size=size, bold=bold))
    return "".join(out)


def para(text, before=60, after=60):
    return f'<w:p><w:pPr><w:spacing w:before="{before}" w:after="{after}"/></w:pPr>{inline(text)}</w:p>'


def spacer(before):
    return f'<w:p><w:pPr><w:spacing w:before="{before}"/></w:pPr></w:p>'


def heading(level, text, page_break=False):
    pb = '<w:pageBreakBefore/>' if page_break else ''
    return (f'<w:p><w:pPr><w:pStyle w:val="{level}"/><w:keepNext/>{pb}</w:pPr>'
            f'<w:r><w:t xml:space="preserve">{escape(text)}</w:t></w:r></w:p>')


def cell(text, width, header=False):
    borders = ''.join(f'<w:{s} w:val="single" w:sz="1" w:space="0" w:color="AAAAAA"/>' for s in ("top", "left", "bottom", "right"))
    fill = "2E75B6" if header else "FFFFFF"
    tcpr = (f'<w:tcPr><w:tcW w:w="{width}" w:type="dxa"/><w:tcBorders>{borders}</w:tcBorders>'
            f'<w:shd w:val="clear" w:color="auto" w:fill="{fill}"/>'
            '<w:tcMar><w:top w:w="80" w:type="dxa"/><w:left w:w="120" w:type="dxa"/>'
            '<w:bottom w:w="80" w:type="dxa"/><w:right w:w="120" w:type="dxa"/></w:tcMar>'
            '<w:vAlign w:val="center"/></w:tcPr>')
    if header:
        body = f'<w:p><w:pPr><w:jc w:val="center"/></w:pPr>{run(text, color="FFFFFF", size=18, bold=True)}</w:p>'
    else:
        body = f'<w:p>{inline(text, size=18)}</w:p>'
    return f"<w:tc>{tcpr}{body}</w:tc>"


def disp(s):
    """표시 폭: 한글·전각은 2, 나머지는 1."""
    return sum(2 if ord(ch) > 0x2E80 else 1 for ch in s)


def col_widths(rows):
    """열 너비(dxa). 최소 너비는 칸 안의 가장 긴 낱말이 접히지 않을 만큼(최대 2200),
    남는 너비는 칸 내용 길이(80 백분위, 긴 칸 몇 개에 끌려가지 않게)에 비례해 나눈다."""
    n = len(rows[0])
    unit = 95  # 9pt 글자 폭 1 당 dxa(근사)
    pad = 300  # 칸 여백
    mins, weight = [], []
    for c in range(n):
        col = [r[c] if c < len(r) else "" for r in rows]
        word = max((disp(w) for t in col for w in re.split(r"[\s/,]+", t) if w), default=2)
        mins.append(min(max(word * unit + pad, 700), 2200))
        lens = sorted(disp(t) for t in col)
        weight.append(max(lens[int(len(lens) * 0.8)], 4) ** 1.4)  # 긴 문장 열에 더 준다
    if sum(mins) > TABLE_W:  # 최소 너비만으로 넘치면 비율로 줄인다
        k = TABLE_W / sum(mins)
        mins = [int(m * k) for m in mins]
    extra = TABLE_W - sum(mins)
    total = sum(weight)
    w = [m + int(extra * x / total) for m, x in zip(mins, weight)]
    w[-1] += TABLE_W - sum(w)
    return w


def table(rows):
    widths = col_widths(rows)
    borders = ''.join(f'<w:{s} w:val="single" w:sz="4" w:space="0" w:color="auto"/>'
                      for s in ("top", "left", "bottom", "right", "insideH", "insideV"))
    out = [f'<w:tbl><w:tblPr><w:tblW w:w="{TABLE_W}" w:type="dxa"/><w:tblBorders>{borders}</w:tblBorders>'
           '<w:tblCellMar><w:left w:w="10" w:type="dxa"/><w:right w:w="10" w:type="dxa"/></w:tblCellMar>'
           '<w:tblLook w:val="0000" w:firstRow="0" w:lastRow="0" w:firstColumn="0" w:lastColumn="0" w:noHBand="0" w:noVBand="0"/>'
           '</w:tblPr><w:tblGrid>' + ''.join(f'<w:gridCol w:w="{x}"/>' for x in widths) + '</w:tblGrid>']
    for i, r in enumerate(rows):
        r = r + [""] * (len(widths) - len(r))
        trpr = '<w:trPr><w:tblHeader/></w:trPr>' if i == 0 else ''
        out.append(f'<w:tr>{trpr}' + ''.join(cell(t, widths[j], header=(i == 0)) for j, t in enumerate(r)) + '</w:tr>')
    out.append('</w:tbl>')
    return ''.join(out)


def code_box(lines):
    ps = ''.join(f'<w:p><w:pPr><w:spacing w:before="0" w:after="0"/></w:pPr>{run(l, size=16, mono=True)}</w:p>' for l in lines)
    return (f'<w:tbl><w:tblPr><w:tblW w:w="{TABLE_W}" w:type="dxa"/><w:tblLook w:val="0000"/></w:tblPr>'
            f'<w:tblGrid><w:gridCol w:w="{TABLE_W}"/></w:tblGrid><w:tr><w:tc><w:tcPr><w:tcW w:w="{TABLE_W}" w:type="dxa"/>'
            '<w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/><w:tcMar><w:top w:w="80" w:type="dxa"/>'
            '<w:left w:w="160" w:type="dxa"/><w:bottom w:w="80" w:type="dxa"/><w:right w:w="120" w:type="dxa"/></w:tcMar>'
            f'</w:tcPr>{ps}</w:tc></w:tr></w:tbl>')


def split_row(line):
    line = line.strip()
    if line.startswith("|"):
        line = line[1:]
    if line.endswith("|") and not line.endswith("\\|"):
        line = line[:-1]
    cells = re.split(r"(?<!\\)\|", line)
    return [c.strip().replace("\\|", "|") for c in cells]


def cover(meta):
    def cp(text, size, color, bold, after=None, before=None):
        sp = ''
        if before or after:
            sp = '<w:spacing' + (f' w:before="{before}"' if before else '') + (f' w:after="{after}"' if after else '') + '/>'
        return f'<w:p><w:pPr>{sp}<w:jc w:val="center"/></w:pPr>{run(text, color=color, size=size, bold=bold)}</w:p>'
    return ''.join([
        spacer(2400),
        cp(meta["product"], 48, "1F4E79", True, after=120),
        cp(meta["title"], 40, "1F4E79", True, after=120),
        cp(meta["subtitle"], 24, "555555", False, before=60, after=60),
        spacer(160),
        cp(meta["version"], 56, "2E75B6", True),
        spacer(120),
        cp(meta["date"], 22, "555555", False),
        spacer(300),
    ])


def parse(src):
    m = re.match(r"---\n(.*?)\n---\n", src, re.S)
    meta = dict(l.split(":", 1) for l in m.group(1).splitlines() if ":" in l)
    meta = {k.strip(): v.strip() for k, v in meta.items()}
    lines = src[m.end():].splitlines()
    body, i, buf = [], 0, []

    def flush():
        if buf:
            body.append(para(" ".join(x.strip() for x in buf)))
            buf.clear()

    while i < len(lines):
        l = lines[i]
        if l.startswith("```"):
            flush()
            j = i + 1
            code = []
            while not lines[j].startswith("```"):
                code.append(lines[j])
                j += 1
            body.append(code_box(code))
            body.append(spacer(80))
            i = j + 1
            continue
        hm = re.match(r"(#{1,3}) (.*)", l)
        if hm:
            flush()
            first_h1 = len(hm.group(1)) == 1 and not any('w:pStyle w:val="1"' in b for b in body)
            body.append(heading(len(hm.group(1)), hm.group(2).strip(), page_break=first_h1))  # 표지는 한 쪽
            i += 1
            continue
        if l.strip().startswith("|"):
            flush()
            rows = []
            while i < len(lines) and lines[i].strip().startswith("|"):
                if not re.match(r"^\s*\|[\s:|-]+\|\s*$", lines[i]):
                    rows.append(split_row(lines[i]))
                i += 1
            body.append(table(rows))
            body.append(spacer(80))
            continue
        if not l.strip():
            flush()
        else:
            buf.append(l)
        i += 1
    flush()
    return meta, body


def build():
    src = open(SOURCE, encoding="utf-8").read().replace("\r\n", "\n")
    meta, body = parse(src)
    sect = ('<w:sectPr><w:headerReference w:type="default" r:id="rId11"/><w:footerReference w:type="default" r:id="rId12"/>'
            '<w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1080" w:right="1080" w:bottom="1080" w:left="1080" '
            'w:header="708" w:footer="708" w:gutter="0"/><w:cols w:space="720"/><w:docGrid w:linePitch="360"/></w:sectPr>')
    doc = (f'<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n<w:document {NS}><w:body>'
           + cover(meta) + ''.join(body) + sect + '</w:body></w:document>')
    out = os.path.join(ROOT, meta["output"])
    os.makedirs(os.path.dirname(out), exist_ok=True)
    fill = {"{{HEADER}}": escape(meta["header"]), "{{FOOTER}}": escape(meta["footer"]),
            "{{TITLE}}": escape(meta["product"] + " " + meta["title"] + " " + meta["version"]),
            "{{DATE}}": meta["isodate"]}
    with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
        # [Content_Types].xml 이 맨 앞에 와야 한다
        names = []
        for base, _, files in os.walk(TEMPLATE):
            for f in files:
                names.append(os.path.relpath(os.path.join(base, f), TEMPLATE).replace(os.sep, "/"))
        names.sort(key=lambda n: (n != "[Content_Types].xml", n))
        for n in names:
            data = open(os.path.join(TEMPLATE, n), encoding="utf-8").read()
            for k, v in fill.items():
                data = data.replace(k, v)
            z.writestr(n, data)
        z.writestr("word/document.xml", doc)
    print(f"wrote {out} ({len(body)} blocks)")


if __name__ == "__main__":
    sys.exit(build())
