#!/bin/sh
# agent 원본 비교 시험. 컨테이너 안에서 실행한다(Dockerfile 참고).
#   /p/c  : 원본 vbr_agent(C) + dummy2, 가짜 xe·sar, 수신 7101
#   /p/go : 전환본 vbr_agent(Go), 가짜 XAPI(8443), 수신 7102
set -u
export PATH=/p/fake-bin:$PATH

# 시험 자료: 원본 1.602 XVA 검증을 통과하는 최소 XVA(gzip tar, ova.xml, 마지막 항목 .xxhash)
mkdir -p /p/xva && cd /p/xva
echo '<value/>' > ova.xml
mkdir -p Ref:1 && head -c 1024 /dev/zero > Ref:1/00000000 && echo 0 > Ref:1/00000000.xxhash
tar czf /p/xva-template.tar.gz ova.xml Ref:1/00000000 Ref:1/00000000.xxhash
echo "INSTALLATION_UUID='853af309-5cca-4c94-89ec-80acca09830c'" > /etc/xensource-inventory

# ── 원본 ──
mkdir -p /p/c/BACKUP
cp /build/agent/vbr_agent /build/agent/dummy2 /p/c/
cat > /p/c/node_info.cfg <<'EOF'
[VBR Agent for Xenserver]
PROGRAM = vbr_agent
PORT-NO = 7101
MAX Log size = 100000000
Interval Time = 2
Retry Count = 3
Mount Point = /p/c/BACKUP
CPU Limit = 50
EOF
cd /p/c && ./vbr_agent
echo "original agent started (exit $?)"
# 원본은 기동 중에 ps -ef|grep vbr_agent 로 중복 실행을 센다. 그 사이에 이름이 같은 명령이 돌면
# "already running" 으로 멈추므로(R-AG-07) 원본이 수신을 시작할 때까지 기다린다.
sleep 3

# ── 전환본 ──
mkdir -p /p/go/BACKUP
/build/go/fakexapi -listen :8443 -xva /p/xva-template.tar.gz -log /p/xapi.log &
cd /p/go
/build/go/vbr_secret keygen /p/go/agent.keyring >/dev/null
ENC=$(echo "parity-password" | /build/go/vbr_secret encrypt /p/go/agent.keyring)
cat > /p/go/node_info.cfg <<EOF
[VBR Agent for Xenserver]
PORT-NO = 7102
MAX Log size = 100000000
XAPI Address = https://127.0.0.1:8443
XAPI User = root
XAPI Password = $ENC
XAPI Verify SSL = N
XAPI Keyring = /p/go/agent.keyring
Backup Root = /p/go/BACKUP
Allow Client = 127.0.0.1
Async Thread = 3
EOF
/build/go/vbr_agent
echo "converted agent started (exit $?)"
sleep 3

echo "== driver =="
/build/go/driver -c 127.0.0.1:7101 -cdir /p/c -go 127.0.0.1:7102 -godir /p/go
rc=$?

echo
echo "== server.log (original, last 15) =="; tail -15 /p/c/server.log
echo "== server.log (converted, last 15) =="; tail -15 /p/go/server.log
echo "== call counts =="; wc -l /p/xe.log /p/xapi.log
echo "== xe calls (original, first 25 except host-list) =="; grep -v "host-list" /p/xe.log | head -25
echo "== XAPI calls (converted, first 25) =="; head -25 /p/xapi.log
exit $rc
