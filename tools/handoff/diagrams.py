"""개발 인계 문서의 흐름도(PNG) 생성기.

사용(저장소 최상위에서):  python -X utf8 tools/handoff/diagrams.py
결과: docs/handoff/img/*.png  (Markdown 과 Word 문서가 같은 그림을 쓴다)
필요한 것: Python 3 + Pillow, 맑은 고딕(Windows 기본 글꼴). Graphviz 등 외부 도구는 쓰지 않는다.

그림은 격자(열·행) 좌표로 노드를 놓고 화살표로 잇는다. 그림을 고치려면 아래 각 함수의 node/edge 를 고친다.
노드 글자의 줄바꿈은 "|" 로 쓴다(첫 줄은 큰 글씨, 나머지는 작은 글씨).
"""
import math
import os

from PIL import Image, ImageDraw, ImageFont

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
OUT = os.path.join(ROOT, "docs", "handoff", "img")
FONT_DIR = os.path.join(os.environ.get("WINDIR", "C:/Windows"), "Fonts")
FONT = os.path.join(FONT_DIR, "malgun.ttf")
FONT_B = os.path.join(FONT_DIR, "malgunbd.ttf")

STYLE = {  # 채움, 테두리, 글자
    "box": ("#DEEAF6", "#2E75B6", "#1F1F1F"),
    "dec": ("#FFF2CC", "#BF9000", "#1F1F1F"),
    "ext": ("#E2EFDA", "#548235", "#1F1F1F"),
    "store": ("#F2F2F2", "#7F7F7F", "#1F1F1F"),
    "start": ("#1F4E79", "#1F4E79", "#FFFFFF"),
    "warn": ("#FBE5D6", "#C55A11", "#1F1F1F"),
}


class Diagram:
    def __init__(self, name, cols, rows, cw=250, rh=92, title=None, bw=200, bh=58):
        self.name, self.cols, self.rows, self.cw, self.rh = name, cols, rows, cw, rh
        self.bw, self.bh = bw, bh
        self.top = 56 if title else 16
        self.title = title
        self.nodes, self.edges = {}, []
        self.font = ImageFont.truetype(FONT, 15)
        self.small = ImageFont.truetype(FONT, 13)
        self.bold = ImageFont.truetype(FONT_B, 19)

    def cx(self, col):
        return 16 + self.cw * col + self.cw / 2

    def cy(self, row):
        return self.top + self.rh * row + self.rh / 2

    def node(self, nid, col, row, text, kind="box", span=1, w=None, h=None):
        width = w or (self.bw + self.cw * (span - 1))
        x = (self.cx(col) + self.cx(col + span - 1)) / 2
        self.nodes[nid] = dict(x=x, y=self.cy(row), w=width, h=h or self.bh, text=text, kind=kind)

    def edge(self, a, b, label="", via=None, side=None):
        self.edges.append((a, b, label, via, side))

    # ---- 그리기 ----
    @staticmethod
    def bounds(n):
        return n["x"] - n["w"] / 2, n["y"] - n["h"] / 2, n["x"] + n["w"] / 2, n["y"] + n["h"] / 2

    def anchor(self, n, toward):
        """노드 n 의 테두리에서 toward 점을 향한 연결점. 넓은 상자는 들어오는 선의 x(또는 y)를 살린다."""
        x0, y0, x1, y1 = self.bounds(n)
        tx, ty = toward
        dx, dy = tx - n["x"], ty - n["y"]
        if abs(dx) * n["h"] > abs(dy) * n["w"]:
            y = min(max(ty, y0 + 8), y1 - 8) if y0 < ty < y1 else n["y"]
            return (x1 if dx > 0 else x0, y)
        x = min(max(tx, x0 + 12), x1 - 12) if x0 < tx < x1 else n["x"]
        return (x, y1 if dy > 0 else y0)

    def render(self):
        W = int(32 + self.cw * self.cols)
        H = int(self.top + self.rh * self.rows + 16)
        img = Image.new("RGB", (W, H), "white")
        d = ImageDraw.Draw(img)
        if self.title:
            d.text((20, 14), self.title, font=self.bold, fill="#1F4E79")
        self.labels = []
        for a, b, label, via, side in self.edges:
            self.draw_edge(d, self.nodes[a], self.nodes[b], label, via, side)
        for n in self.nodes.values():
            self.draw_node(d, n)
        for lx, ly, tw, label in self.labels:  # 이름표는 상자 위에 그린다
            d.rectangle((lx - tw / 2 - 3, ly - 9, lx + tw / 2 + 3, ly + 9), fill="white")
            d.text((lx - tw / 2, ly - 9), label, font=self.small, fill="#C00000")
        os.makedirs(OUT, exist_ok=True)
        path = os.path.join(OUT, self.name + ".png")
        img.save(path)
        return path

    def draw_node(self, d, n):
        fill, line, color = STYLE[n["kind"]]
        x0, y0, x1, y1 = self.bounds(n)
        if n["kind"] == "dec":
            pts = [(n["x"], y0 - 6), (x1 + 8, n["y"]), (n["x"], y1 + 6), (x0 - 8, n["y"])]
            d.polygon(pts, fill=fill)
            d.line(pts + [pts[0]], fill=line, width=2)
        elif n["kind"] == "store":
            d.rectangle((x0, y0 + 6, x1, y1), fill=fill, outline=line, width=2)
            d.ellipse((x0, y0, x1, y0 + 14), fill=fill, outline=line, width=2)
        elif n["kind"] == "start":
            d.rounded_rectangle((x0, y0, x1, y1), int(n["h"] / 2), fill=fill, outline=line, width=2)
        else:
            d.rounded_rectangle((x0, y0, x1, y1), 8, fill=fill, outline=line, width=2)
        lines = n["text"].split("|")
        lh = 19
        ty = n["y"] - lh * len(lines) / 2 + 1
        for i, t in enumerate(lines):
            f = self.font if i == 0 else self.small
            tw = d.textlength(t, font=f)
            d.text((n["x"] - tw / 2, ty + i * lh), t, font=f, fill=color)

    def draw_edge(self, d, a, b, label, via, side):
        pts = [(self.cx(c), self.cy(r)) for c, r in via] if via else []
        ax0, ay0, ax1, ay1 = self.bounds(a)
        bx0, by0, bx1, by1 = self.bounds(b)
        if not via and not side and max(ax0, bx0) < min(ax1, bx1) and (ay1 < by0 or by1 < ay0):
            # 가로 범위가 겹치고 위아래로 떨어져 있으면 곧은 세로선(겹치는 구간 안에서 a 의 x 에 가깝게)
            x = min(max(a["x"], max(ax0, bx0) + 14), min(ax1, bx1) - 14)
            path = [(x, ay1 if by0 > ay1 else ay0), (x, by0 if by0 > ay1 else by1)]
            return self.finish_edge(d, path, label)
        if not via and not side and max(ay0, by0) < min(ay1, by1) and (ax1 < bx0 or bx1 < ax0):
            y = min(max(a["y"], max(ay0, by0) + 8), min(ay1, by1) - 8)
            path = [(ax1 if bx0 > ax1 else ax0, y), (bx0 if bx0 > ax1 else bx1, y)]
            return self.finish_edge(d, path, label)
        if not via and abs(a["x"] - b["x"]) > 1 and abs(a["y"] - b["y"]) > 1:
            # L 모양: 기본은 가로로 나간 뒤 세로로 들어간다. side="v" 면 세로 먼저
            pts = [(a["x"], b["y"])] if side == "v" else [(b["x"], a["y"])]
        start = self.anchor(a, pts[0] if pts else (b["x"], b["y"]))
        end = self.anchor(b, pts[-1] if pts else (a["x"], a["y"]))
        self.finish_edge(d, [start] + pts + [end], label)

    def finish_edge(self, d, path, label):
        d.line(path, fill="#404040", width=2)
        (px, py), (ex, ey) = path[-2], path[-1]
        ang = math.atan2(ey - py, ex - px)
        s = 10
        head = [(ex, ey), (ex - s * math.cos(ang - 0.4), ey - s * math.sin(ang - 0.4)),
                (ex - s * math.cos(ang + 0.4), ey - s * math.sin(ang + 0.4))]
        d.polygon(head, fill="#404040")
        if label:
            (sx, sy), (nx, ny) = path[0], path[1]
            lx, ly = (sx + nx) / 2, (sy + ny) / 2
            tw = d.textlength(label, font=self.small)
            if abs(sy - ny) < 1 and abs(sx - nx) < tw + 16:  # 짧은 가로선: 이름표를 선 위에
                ly -= 13
            self.labels.append((lx, ly, tw, label))


def overall():
    g = Diagram("00-overall", 5, 4, cw=225, rh=110, title="NCC-VBR 전체 구성 — 4개 실행 파일과 외부 시스템")
    g.node("cron", 1, 0, "스케줄러 / 운영자|(cron, vbr_mgr)", "ext", span=2)
    g.node("asp", 3, 0, "관리 서버(ASP)|TCP 문자열 전문", "ext")
    g.node("mw", 4, 0, "관리 서버 / Middleware|HTTP JSON", "ext")
    g.node("portal", 0, 1, "포털(TCP)|완료 이벤트 CIFSFIN00", "ext", w=170)
    g.node("cifs", 1, 1, "vbr_cifs 3.0000|NAS 증분 백업(배치)")
    g.node("remove", 2, 1, "vbr_remove 2.000|이전 버전 정리(배치)")
    g.node("agent", 3, 1, "vbr_agent 3.000|VM 백업·복구(상주, TCP)")
    g.node("vm", 4, 1, "vbr_vm 4.000|VM 백업·복구(상주, HTTP)")
    g.node("nas", 1, 2, "원본 NAS → 백업 NAS|(OS 마운트, 이전 버전 *-vbr-*.zip)", "store", span=2)
    g.node("xen", 3, 2, "XenServer 풀 마스터|XAPI 443 (XML-RPC, /export, /import)", "ext", span=2)
    g.node("bk", 3, 3, "/BACKUP (NAS)|XVA 파일", "store", span=2)
    g.edge("cron", "cifs", "명령행", via=[(1, 0.6)])
    g.edge("cron", "remove", "명령행", via=[(2, 0.6)])
    g.edge("asp", "agent", "TCP")
    g.edge("mw", "vm", "HTTP")
    g.edge("cifs", "portal")
    g.edge("cifs", "nas", "복사")
    g.edge("remove", "nas", "삭제")
    g.edge("agent", "xen")
    g.edge("vm", "xen")
    g.edge("xen", "bk", "XVA 스트림")
    return g.render()


def cifs():
    g = Diagram("01-cifs-flow", 3, 9, cw=270, rh=82, title="vbr_cifs 처리 흐름")
    g.node("s", 1, 0, "vbr_cifs source target seq", "start")
    g.node("boot", 1, 1, "boot.Start|인자·node_info.cfg·logx·백그라운드 분리", w=250)
    g.node("hist", 1, 2, "history/<원본>-<대상>.vbr 읽기|이전 성공 기준 시각", w=250)
    g.node("fsd", 1, 3, "오늘이 Full Scan Day?", "dec")
    g.node("zero", 0, 3, "기준 시각 = 0|(전체 비교)", "warn")
    g.node("walk", 1, 4, "walk.Run 작업자 N개|루트 분할·2ND SEARCH DIR·EXCLUSION", w=250)
    g.node("dis", 2, 4, "최상위 dis_<이름>|대상 폴더 이름 바꾸기")
    g.node("cmp", 1, 5, "대상 없음 · 크기 다름|· 원본이 최신?", "dec", w=230, h=62)
    g.node("copy", 1, 6, "기존 대상 → 파일명-vbr-시각.zip(gzip)|원본 복사", w=280)
    g.node("skip", 2, 5, "건너뜀", "store")
    g.node("save", 1, 7, "오류 0 → history 에 시작 시각 저장|오류 있음 → 기준 유지", w=280)
    g.node("end", 1, 8, "요약 로그 · 포털 CIFSFIN00 · 종료 코드 0/1/2", "start", w=330)
    g.edge("s", "boot"); g.edge("boot", "hist"); g.edge("hist", "fsd")
    g.edge("fsd", "zero", "예"); g.edge("fsd", "walk", "아니오")
    g.edge("zero", "walk", "", side="v")
    g.edge("walk", "dis", "dis_")
    g.edge("walk", "cmp", "파일마다"); g.edge("cmp", "copy", "예"); g.edge("cmp", "skip", "아니오")
    g.edge("copy", "save", "모든 파일 끝"); g.edge("save", "end")
    return g.render()


def remove():
    g = Diagram("02-remove-flow", 3, 8, cw=270, rh=82, title="vbr_remove 처리 흐름")
    g.node("s", 1, 0, "vbr_remove delete-dir days", "start")
    g.node("chk", 1, 1, "days 검사(0~106751 정수)|틀리면 사용법·종료 255")
    g.node("boot", 1, 2, "boot.Start|node_info.cfg [File Remove (BATCH) …]", w=250)
    g.node("cut", 1, 3, "기준 = 지금 - days×24h|YYYYMMDDHHMMSS 문자열")
    g.node("walk", 1, 4, "walk.Run 작업자 N개 — 백업 트리 순회", w=300)
    g.node("name", 1, 5, "이름이 <이름>-vbr-<14자리>.zip|이고 시각 ≤ 기준?", "dec", w=270, h=64)
    g.node("keep", 2, 5, "보존", "store")
    g.node("mode", 0, 6, "Delete Mode = DELETE?", "dec")
    g.node("log", 0, 7, "LOG(기본): delete.log 에|rm 명령 기록")
    g.node("del", 1, 6, "DELETE: 파일 삭제", "warn")
    g.node("end", 2, 7, "요약 로그 · 종료 0/1/2", "start")
    g.edge("s", "chk"); g.edge("chk", "boot"); g.edge("boot", "cut"); g.edge("cut", "walk"); g.edge("walk", "name", "파일마다")
    g.edge("name", "keep", "아니오"); g.edge("name", "mode", "예")
    g.edge("mode", "log", "아니오"); g.edge("mode", "del", "예")
    g.edge("del", "end", "", side="v"); g.edge("log", "end")
    return g.render()


def agent():
    g = Diagram("03-agent-flow", 4, 7, cw=250, rh=88, title="vbr_agent 구성과 처리 흐름 (TCP 문자열 전문)")
    g.node("asp", 0, 0, "관리 서버(ASP)", "ext")
    g.node("rx", 1, 0, "TCP 수신(PORT-NO)|Allow Client · 길이 7자리 프레임")
    g.node("kind", 1, 1, "작업구분", "dec")
    g.node("q", 0, 2, "Q: transaction 로그 조회|코드 0·2·3·6·7·C·9")
    g.node("c", 2, 2, "C: 대기 중 취소|(처리 중은 거부)")
    g.node("reg", 1, 2, "A: 등록부 저장 → 'OK' 응답|vbr_agent.state, 대기열 100")
    g.node("wk", 1, 3, "작업자(Async Thread 2~10)|BACKUP / PARTIAL / RESTORE")
    g.node("xen", 1, 4, "xen.go: XAPI 로그인|스냅샷 → (PARTIAL) 디스크 정리")
    g.node("tr", 1, 5, "transfer.go: /export → .partial|또는 /import")
    g.node("fin", 1, 6, "결과를 transaction-YYYYMM.log 에 기록|스냅샷 정리 · backup.log 단계", "start", w=320)
    g.node("xs", 3, 4, "XenServer 풀|XAPI 443", "ext")
    g.node("nas", 3, 5, "Backup Root|(/BACKUP)", "store")
    g.edge("asp", "rx", "B+seq+A/Q/C")
    g.edge("rx", "kind"); g.edge("kind", "q", "Q"); g.edge("kind", "c", "C"); g.edge("kind", "reg", "A")
    g.edge("reg", "wk"); g.edge("wk", "xen"); g.edge("xen", "tr"); g.edge("tr", "fin")
    g.edge("xen", "xs"); g.edge("tr", "nas")
    return g.render()


def vm_arch():
    g = Diagram("04-vm-flow", 4, 7, cw=250, rh=88, title="vbr_vm 구성과 요청 처리 흐름 (JSON HTTP)")
    g.node("c", 0, 0, "호출자|(관리 서버·Middleware·demo.ps1)", "ext")
    g.node("auth", 1, 0, "gin · apiauth|X-Internal-Token")
    g.node("val", 2, 0, "vm.Validate|필수 값·형식 → 400")
    g.node("st", 3, 0, "jobstore.Accept|jobs.jsonl: queued", "store")
    g.node("q", 3, 1, "대기열(max_queue)|→ 202 ACCEPTED")
    g.node("w", 2, 1, "작업자 max_jobs|설정 사본·timeout")
    g.node("dec", 2, 2, "action", "dec")
    g.node("ex", 1, 3, "export|스냅샷 정책 → /export → .partial")
    g.node("vf", 2, 3, "verify|tar·청크 SHA1·SHA256")
    g.node("rs", 3, 3, "restore|SR 확인 → (zstd 해제) → /import")
    g.node("xs", 0, 3, "XenServer 풀|XAPI 443", "ext")
    g.node("fin", 2, 4, "작업 기록: succeeded / failed|/ canceled / interrupted", "store", w=260)
    g.node("ob", 3, 5, "outbox(result_report.url)|재시도 · Dead Letter")
    g.node("get", 1, 5, "GET /api/v1/jobs/{taskId}|상태·percent·결과·오류")
    g.node("mw", 3, 6, "결과 수신(규약 2)", "ext")
    g.node("c2", 1, 6, "호출자 조회", "ext")
    g.edge("c", "auth", "POST"); g.edge("auth", "val"); g.edge("val", "st")
    g.edge("st", "q"); g.edge("q", "w"); g.edge("w", "dec")
    g.edge("dec", "ex", "export"); g.edge("dec", "vf", "verify"); g.edge("dec", "rs", "restore")
    g.edge("ex", "xs"); g.edge("ex", "fin", side="v"); g.edge("vf", "fin"); g.edge("rs", "fin", side="v")
    g.edge("fin", "ob"); g.edge("fin", "get"); g.edge("ob", "mw"); g.edge("c2", "get")
    return g.render()


def vm_states():
    g = Diagram("05-vm-states", 3, 4, cw=260, rh=92, title="vbr_vm 작업 상태 전이")
    g.node("ok", 2, 0, "succeeded|결과 payload.options", "start")
    g.node("q", 0, 1, "queued|접수(202 전 기록)")
    g.node("r", 1, 1, "running|login·snapshot·exporting…")
    g.node("f", 2, 1, "failed|error.code·detail", "warn")
    g.node("cn", 2, 2, "canceled|취소 API · 정리 후", "store")
    g.node("it", 0, 3, "interrupted|종료 신호·재기동(자동 재개 없음)", "dec", w=230, h=60)
    g.edge("q", "r", "작업자")
    g.edge("r", "ok", "", side="v")
    g.edge("r", "f")
    g.edge("r", "cn", "", side="v")
    g.edge("q", "cn", "대기 중 취소", via=[(0, 2)])
    g.edge("q", "it", "")
    g.edge("r", "it", "종료·재기동", via=[(1, 3)])
    return g.render()


def vm_export():
    g = Diagram("06-vm-export", 3, 8, cw=280, rh=80, title="vbr_vm export 상세 — 스냅샷 정책과 정리")
    g.node("s", 1, 0, "export 시작: 암호 복호화 · XAPI 로그인", "start", w=320)
    g.node("d", 1, 1, "임시 스냅샷이 필요한가?|(disks / always / Running)", "dec", w=250, h=64)
    g.node("no", 0, 2, "대상 그대로 내보냄|(Halted·Suspended, 스냅샷 uuid)")
    g.node("snap", 2, 2, "임시 스냅샷 생성|→ snapshot_left 기록")
    g.node("disk", 2, 3, "disks 지정 시|나머지 VBD·VDI 삭제")
    g.node("exp", 1, 4, "XAPI task + /export 스트림 → <파일>.partial|(zstd 면 받으며 압축, SHA256)", w=340)
    g.node("tk", 1, 5, "task 결과 success?", "dec")
    g.node("bad", 0, 5, "EXPORT_INCOMPLETE|.partial 삭제", "warn")
    g.node("ren", 1, 6, ".partial → 최종 이름|(.xva / .xva.zst)")
    g.node("cl", 2, 6, "임시 스냅샷 삭제|(사용 중이면 2초 간격 재시도)")
    g.node("end", 1, 7, "결과 필드 기록 · (verify_after_export)", "start", w=320)
    g.edge("s", "d"); g.edge("d", "no", "아니오"); g.edge("d", "snap", "예")
    g.edge("snap", "disk"); g.edge("no", "exp", "", side="v"); g.edge("disk", "exp", "", side="v")
    g.edge("exp", "tk"); g.edge("tk", "bad", "아니오"); g.edge("tk", "ren", "예"); g.edge("ren", "cl", "항상"); g.edge("ren", "end")
    return g.render()


def packages():
    g = Diagram("07-packages", 5, 4, cw=230, rh=100, title="go/ 패키지 의존 — 실행 파일 → 업무 → 공통")
    for i, (nid, t) in enumerate([("c1", "cmd/vbr_cifs"), ("c2", "cmd/vbr_remove"), ("c3", "cmd/vbr_agent"),
                                  ("c4", "cmd/vbr_vm"), ("c5", "cmd/vbr_secret")]):
        g.node(nid, i, 0, t, "start", w=170)
    g.node("cifs", 0, 1, "internal/cifs|+ portal")
    g.node("rm", 1, 1, "internal/remove")
    g.node("ag", 2, 1, "internal/agent")
    g.node("vm", 3, 1, "internal/vm, vm/api|xva, jobstore")
    g.node("b", 0, 2, "boot · config · logx · daemon|(node_info.cfg 계열, 1단계 3개 서비스)", span=3, w=640)
    g.node("v", 3, 2, "vmconfig · stdlogger|i18n · apiauth · redact")
    g.node("w", 0, 3, "walk · fsx|(파일 순회, cifs·remove)", span=2, w=400)
    g.node("x", 2, 3, "xapi · secret|(XAPI·암호, agent·vm·vbr_secret 공용)", span=3, w=640)
    for a, b in [("c1", "cifs"), ("c2", "rm"), ("c3", "ag"), ("c4", "vm")]:
        g.edge(a, b)
    g.edge("cifs", "b"); g.edge("rm", "b"); g.edge("ag", "b")
    g.edge("vm", "v")
    g.edge("c5", "x")
    return g.render()


if __name__ == "__main__":
    for f in (overall, cifs, remove, agent, vm_arch, vm_states, vm_export, packages):
        print(f())
