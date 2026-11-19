#!/bin/sh
# NCC-VBR agent 전환용 표본 수집 스크립트 (읽기 전용)
#
# 원본 vbr_agent 1.602가 실행하고 출력을 읽는 조회 명령을 그대로 실행해
# 결과를 파일로 모은다. VM·SR·VDI를 바꾸는 명령(vm-snapshot, vm-export,
# vm-import, vdi-destroy, vm-uninstall, *-param-set)은 실행하지 않는다.
#
# 실행 위치: 대상 XenServer 풀의 호스트 dom0, root
# 사용법:   sh collect-xe-samples.sh [시험용-VM-uuid]
#           VM uuid를 주지 않으면 이 호스트에서 실행 중인 첫 VM을 쓴다.
# 결과:     ./vbr-samples-<호스트이름>-<시각>.tar.gz  (이 파일을 전달)
#
# 각 표본 파일의 첫 줄은 실행한 명령, 끝줄은 종료 코드다.
# 원본 소스 위치는 docs/agent/05-interface-spec.md 7장 표와 같다.

set -u
HOST_NAME=$(hostname)
STAMP=$(date +%Y%m%d%H%M%S)
OUT="vbr-samples-${HOST_NAME}-${STAMP}"
mkdir -p "$OUT" || exit 1

run() {
    # $1 = 파일 이름, 나머지 = 셸로 실행할 명령(원본과 같은 문자열)
    name=$1
    shift
    {
        echo "\$ $*"
        sh -c "$*" 2>&1
        echo "[exit=$?]"
    } > "$OUT/$name.txt"
}

# ---- 환경 ----
run 00-os        "uname -a; cat /etc/redhat-release /etc/os-release 2>/dev/null"
run 00-inventory "cat /etc/xensource-inventory"
run 00-tools     "command -v xe sar free df fuser; rpm -q sysstat 2>/dev/null"
run 00-xapi-sock "ls -l /var/lib/xcp/xapi /var/xapi/xapi 2>&1"

HOST_UUID=$(sed -n "s/^INSTALLATION_UUID='\(.*\)'/\1/p" /etc/xensource-inventory)
VM_UUID=${1:-}
if [ -z "$VM_UUID" ]; then
    VM_UUID=$(xe vm-list resident-on="$HOST_UUID" is-control-domain=false \
        is-a-template=false power-state=running params=uuid --minimal | cut -d, -f1)
fi
echo "host=$HOST_UUID vm=$VM_UUID" > "$OUT/00-selected.txt"

# ---- 상태 감시 (원본 469, 895, 910, 933, 943행) ----
run 01-host-by-name "xe host-list params=uuid,name-label hostname=$HOST_NAME"
run 02-host-list    "xe host-list"
run 03-df           "df -Pk /|tail -1"
run 04-sar          "sar 3 1|grep Average"
run 05-free         "free|grep Mem"

# ---- INFO (1890, 1924, 1976행) ----
run 06-pool-list "xe pool-list params=uuid,name-label"
run 07-host-addr "xe host-list params=uuid,name-label,address"
run 08-vm-all    "xe vm-list params=uuid,name-label,resident-on,networks,os-version,power-state is-a-template=false is-control-domain=false"

# ---- SR-INFO (2146, 2179행) ----
run 09-pool-default-sr "xe pool-list params=uuid,default-SR"
run 10-sr-list         "xe sr-list params=uuid,host,name-label,physical-utilisation,physical-size,type"

# ---- LUN-INFO (2303, 2333행) ----
run 11-vbd-list "xe vbd-list vm-uuid=$VM_UUID type=Disk params=uuid,userdevice,vdi-uuid"
VDI_UUID=$(xe vbd-list vm-uuid="$VM_UUID" type=Disk params=vdi-uuid --minimal | cut -d, -f1)
VBD_UUID=$(xe vbd-list vm-uuid="$VM_UUID" type=Disk params=uuid --minimal | cut -d, -f1)
run 12-vdi-list "xe vdi-list uuid=$VDI_UUID params=name-label,virtual-size"

# ---- XS-INFO (2489행) ----
run 13-host-detail "xe host-list params=name-label,software-version,cpu_info,address,memory-total,memory-free,edition uuid=$HOST_UUID"

# ---- VM-INFO (2586행) ----
run 14-vm-detail "xe vm-list params=name-label,power-state,memory-actual,VCPUs-max,install-time,networks,os-version uuid=$VM_UUID"

# ---- RES-VM-INFO (2766, 2770행) ----
run 15-vm-resident "xe vm-list params=uuid,name-label is-a-template=false is-control-domain=false resident-on=$HOST_UUID"
run 16-vm-halted   "xe vm-list params=uuid,name-label is-a-template=false is-control-domain=false power-state=halted"

# ---- 부분 백업이 스냅샷에 쓰는 조회 (4020, 4044, 4056행). 원본 VM으로 대신 확인 ----
run 17-vbd-uuid      "xe vbd-list vm-uuid=$VM_UUID type=Disk params=uuid"
run 18-vbd-userdev   "xe vbd-param-get uuid=$VBD_UUID param-name=userdevice"
run 19-vbd-vdi       "xe vbd-param-get uuid=$VBD_UUID param-name=vdi-uuid"

# ---- 오류 출력 표본 (없는 uuid) ----
run 20-vm-notfound "xe vm-list params=name-label uuid=00000000-0000-0000-0000-000000000000"
run 21-vm-param-notfound"xe vm-param-get uuid=00000000-0000-0000-0000-000000000000 param-name=name-label"

tar czf "$OUT.tar.gz" "$OUT" && echo "완료: $(pwd)/$OUT.tar.gz"
