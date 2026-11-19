#!/bin/sh
# 원본(C)과 전환본(Go)의 결과 비교 시험. 컨테이너 안에서 실행한다(Dockerfile 참고).
#
# 두 프로그램을 각자의 작업 디렉터리(/w/asis, /w/tobe)에서 같은 입력으로 실행하고
# 대상 디렉터리 내용, 이전 버전 파일, 삭제 목록을 비교한다.
set -u

FAIL=0
check() {
    # check "설명" 명령...
    desc="$1"
    shift
    if "$@" > /tmp/check.out 2>&1; then
        echo "PASS  $desc"
    else
        echo "FAIL  $desc"
        sed 's/^/      /' /tmp/check.out | head -20
        FAIL=1
    fi
}

wait_log() {
    # wait_log 파일 문자열 : 백그라운드로 분리된 프로그램이 끝날 때까지 기다린다.
    i=0
    while [ "$i" -lt 40 ]; do
        if grep -q "$2" "$1" 2>/dev/null; then
            return 0
        fi
        sleep 1
        i=$((i + 1))
    done
    echo "TIMEOUT waiting [$2] in $1"
    return 1
}

make_source() {
    root="$1"
    mkdir -p "$root/a/deep" "$root/b/with space" "$root/skip" "$root/.snapshot" "$root/empty"
    echo "root file"   > "$root/root.txt"
    echo "one"         > "$root/a/one.txt"
    echo "two"         > "$root/a/deep/two.txt"
    echo "three"       > "$root/b/with space/three file.txt"
    echo "excluded"    > "$root/skip/x.txt"
    echo "snapshot"    > "$root/.snapshot/s.txt"
    # 큰 파일은 한 번만 만들어 원본·전환본이 같은 내용을 쓰게 한다.
    [ -f /tmp/big.bin ] || head -c 2000000 /dev/urandom > /tmp/big.bin
    cp /tmp/big.bin "$root/a/big.bin"
    ln -s "$root/a/one.txt" "$root/a/link.txt"
}

setup() {
    # setup 이름 : 작업 디렉터리, 원본·대상 디렉터리, 설정 파일을 만든다.
    home="/w/$1"
    mkdir -p "$home/history" "$home/src" "$home/tgt"
    make_source "$home/src"
    # Full Scan Day 는 원본 2.0672 가 읽지 않는다. 전환본(2.068 기능, C-CIFS-21)은 키가 없으면
    # 일요일로 보므로, 실행 1~7 이 요일에 따라 달라지지 않게 0~6 이 아닌 값으로 끈다.
    # 실행 8 에서 2.068 재구성판과 함께 요일 값을 바꿔 시험한다.
    cat > "$home/node_info.cfg" <<EOF
[VBR Manager]
Full Scan Day	= -
MAX Log size		= 100000000
MAX Thread cnt		= 2
EXCLUSION DIR	=	$home/src/skip

[File Remove (BATCH) for NCC-VBR]
MAX Log size		= 100000000
MAX Thread cnt		= 2
QUEUE ID		= 30077
DELETE_MODE_LINE
EOF
}

set_delete_mode() {
    # set_delete_mode 이름 값 : remove 설정의 Delete Mode 를 바꾼다(값이 비면 항목 삭제).
    cfg="/w/$1/node_info.cfg"
    if [ -n "$2" ]; then
        sed -i "s/^DELETE_MODE_LINE$/Delete Mode = $2/; s/^Delete Mode = .*$/Delete Mode = $2/" "$cfg"
    else
        sed -i "s/^Delete Mode = .*$/DELETE_MODE_LINE/" "$cfg"
    fi
}

run_cifs() {
    # run_cifs 이름 실행파일 : 백업을 한 번 실행하고 끝날 때까지 기다린다.
    home="/w/$1"
    rm -f "$home/server.log"
    (cd "$home" && "$2" "$home/src" "$home/tgt" 20261117090503001)
    wait_log "$home/server.log" "VBR CIFS backup finished"
}

run_remove() {
    home="/w/$1"
    rm -f "$home/server.log"
    (cd "$home" && "$2" "$home/tgt" "$3")
    wait_log "$home/server.log" "NCC-VBR batch finished"
}

totals() {
    # 마지막 합계 줄에서 숫자만 뽑는다(원본과 전환본의 뒤쪽 문구가 달라서).
    grep "Total File = " "/w/$1/server.log" | tail -1 \
        | sed 's/^.*Total File = \[\([0-9]*\)\], Total Dir = \[\([0-9]*\)\], \([A-Za-z ]*\) = \[\([0-9]*\)\].*$/\1 \2 \4/'
}

tree_list() {
    # 대상 디렉터리의 상대 경로 목록. 이전 버전 파일은 시각 부분을 가린다.
    (cd "/w/$1/tgt" && find . | sed 's/-vbr-[0-9]\{14\}\.zip$/-vbr-STAMP.zip/' | sort)
}

content_sums() {
    # 이전 버전 파일을 뺀 모든 파일의 내용 체크섬.
    (cd "/w/$1/tgt" && find . -type f ! -name '*-vbr-*.zip' -exec md5sum {} + | sort -k 2)
}

version_sums() {
    # 이전 버전 파일을 풀어낸 내용의 체크섬.
    (cd "/w/$1/tgt" && find . -type f -name '*-vbr-*.zip' | sort | while IFS= read -r f; do
        printf '%s ' "$(echo "$f" | sed 's/-vbr-[0-9]\{14\}\.zip$//')"
        gzip -dc < "$f" | md5sum
    done)
}

same() {
    # same 함수 : 원본과 전환본에서 함수 출력이 같은지 본다.
    "$1" asis > /tmp/asis.out
    "$1" tobe > /tmp/tobe.out
    diff /tmp/asis.out /tmp/tobe.out
}

changed_is() {
    # changed_is 건수 이름 : 마지막 실행의 변경 건수가 기대한 값인지 본다.
    [ "$(totals "$2" | cut -d' ' -f3)" = "$1" ]
}

history_names() {
    ls "/w/$1/history" | sed "s/$1/X/g"
}

delete_list() {
    # 작업 디렉터리 이름과 이전 버전 파일의 시각(실행 시점마다 다르다)을 가린다.
    sed "s#/w/$1/#/w/X/#; s/-vbr-[0-9]\{14\}\.zip/-vbr-STAMP.zip/" "/w/$1/delete.log" | sort
}

echo "=== 0. 준비"
setup asis
setup tobe

echo "=== 1. 첫 실행(전체 복사)"
run_cifs asis /build/cifs/vbr_cifs
run_cifs tobe /build/go/vbr_cifs
check "1-1 대상 디렉터리 구조가 같다"          same tree_list
check "1-2 복사된 파일 내용이 같다"            same content_sums
check "1-3 합계(파일·디렉터리·변경)가 같다"    same totals
check "1-4 제외 디렉터리는 복사되지 않는다"    test ! -e /w/tobe/tgt/skip
check "1-5 스냅샷 디렉터리는 복사되지 않는다"  test ! -e /w/tobe/tgt/.snapshot
check "1-6 심볼릭 링크는 복사되지 않는다"      test ! -e /w/tobe/tgt/a/link.txt
check "1-7 기준 시각 파일 이름이 같다"         same history_names

echo "=== 2. 변경 후 실행(증분 복사 + 이전 버전 보관)"
sleep 2
for name in asis tobe; do
    echo "one changed and longer" > "/w/$name/src/a/one.txt"
    echo "brand new"              > "/w/$name/src/b/new.txt"
    mkdir -p "/w/$name/src/c/sub"
    echo "new dir file"           > "/w/$name/src/c/sub/n.txt"
done
run_cifs asis /build/cifs/vbr_cifs
run_cifs tobe /build/go/vbr_cifs
check "2-1 대상 디렉터리 구조가 같다"          same tree_list
check "2-2 복사된 파일 내용이 같다"            same content_sums
check "2-3 이전 버전 파일의 내용이 같다"       same version_sums
check "2-4 합계(파일·디렉터리·변경)가 같다"    same totals

echo "=== 3. 변경 없이 실행"
sleep 2
run_cifs asis /build/cifs/vbr_cifs
run_cifs tobe /build/go/vbr_cifs
check "3-1 대상 디렉터리 구조가 같다"          same tree_list
check "3-2 합계(파일·디렉터리·변경 0건)가 같다" same totals

echo "=== 4. 삭제 LOG 모드(보존 0일)"
sleep 2
run_remove asis /build/remove/vbr_remove 0
run_remove tobe /build/go/vbr_remove 0
check "4-1 삭제 명령 목록이 같다"              same delete_list
check "4-2 파일은 지워지지 않는다"             same tree_list
check "4-3 합계(파일·디렉터리·삭제)가 같다"    same totals

echo "=== 5. 삭제 LOG 모드(보존 30일: 대상 없음)"
run_remove asis /build/remove/vbr_remove 30
run_remove tobe /build/go/vbr_remove 30
check "5-1 삭제 대상이 없다(원본)"             test ! -s /w/asis/delete.log
check "5-2 삭제 대상이 없다(전환본)"           test ! -s /w/tobe/delete.log

echo "=== 6. 삭제 DELETE 모드(보존 0일)"
set_delete_mode asis DELETE
set_delete_mode tobe DELETE
run_remove asis /build/remove/vbr_remove 0
run_remove tobe /build/go/vbr_remove 0
check "6-1 대상 디렉터리 구조가 같다"          same tree_list
check "6-2 남은 파일 내용이 같다"              same content_sums
check "6-3 이전 버전 파일이 남지 않는다"       sh -c '! find /w/tobe/tgt -name "*-vbr-*.zip" | grep -q .'
check "6-4 합계(파일·디렉터리·삭제)가 같다"    same totals

echo "=== 7. 원본과 전환본이 서로의 결과를 이어받기"
# 원본이 만든 기준 시각 파일과 대상 디렉터리로 전환본을 실행하고, 반대로도 실행한다.
# 전환(교체)과 되돌리기가 가능한지 보는 시험이다.
sleep 2
for name in asis tobe; do
    echo "changed for takeover test" > "/w/$name/src/a/deep/two.txt"
done
run_cifs asis /build/go/vbr_cifs
run_cifs tobe /build/cifs/vbr_cifs
check "7-1 대상 디렉터리 구조가 같다"          same tree_list
check "7-2 복사된 파일 내용이 같다"            same content_sums
check "7-3 이전 버전 파일의 내용이 같다"       same version_sums
check "7-4 합계(파일·디렉터리·변경)가 같다"    same totals
check "7-5 전환본이 원본의 기준 시각을 이어받아 1건만 변경한다" changed_is 1 asis
check "7-6 원본이 전환본의 기준 시각을 이어받아 1건만 변경한다" changed_is 1 tobe

set_full_scan_day() {
    # set_full_scan_day 이름 값 : cifs 설정의 Full Scan Day 를 바꾼다.
    sed -i "s/^Full Scan Day\t= .*$/Full Scan Day\t= $2/" "/w/$1/node_info.cfg"
}

full_scan_logged() {
    grep -q "Full search mode enable.\[$2\]" "/w/$1/server.log"
}

echo "=== 8. 2.068 Full Scan Day(원본은 2.068 재구성판)"
# 원본 쪽을 2.068 재구성판(Dockerfile, make-cifs-2068.sh)으로 바꾼다. 실행 7 뒤에는 두 쪽 모두
# 기준 시각 파일이 있다. 원본 파일은 건드리지 않고 대상 파일만 크기를 바꿔, 기준 시각 필터에 걸려
# 비교되지 않는 상태(K-CIFS-03)를 만든다. 지정 요일이 아니면 0건, 지정 요일이면 다시 복사한다.
today=$(date +%w)
other=$(( (today + 1) % 7 ))
sleep 2
for name in asis tobe; do
    echo "target damaged, size differs" > "/w/$name/tgt/root.txt"
    set_full_scan_day "$name" "$other"
done
run_cifs asis /build/cifs/vbr_cifs_2068
run_cifs tobe /build/go/vbr_cifs
check "8-1 다른 요일: 합계가 같다"                         same totals
check "8-2 다른 요일: 원본은 기준 시각 필터로 0건 변경"     changed_is 0 asis
check "8-3 다른 요일: 전환본도 0건 변경"                   changed_is 0 tobe
check "8-4 다른 요일: 원본은 전체 비교 로그가 없다"         sh -c '! grep -q "Full search mode enable" /w/asis/server.log'
check "8-5 다른 요일: 전환본도 전체 비교 로그가 없다"       sh -c '! grep -q "Full search mode enable" /w/tobe/server.log'

sleep 2
for name in asis tobe; do
    set_full_scan_day "$name" "$today"
done
run_cifs asis /build/cifs/vbr_cifs_2068
run_cifs tobe /build/go/vbr_cifs
check "8-6 지정 요일: 대상 디렉터리 구조가 같다"           same tree_list
check "8-7 지정 요일: 복사된 파일 내용이 같다"             same content_sums
check "8-8 지정 요일: 이전 버전 파일의 내용이 같다"        same version_sums
check "8-9 지정 요일: 합계가 같다"                         same totals
check "8-10 지정 요일: 원본은 손상된 대상 1건을 다시 복사"  changed_is 1 asis
check "8-11 지정 요일: 전환본도 1건을 다시 복사"           changed_is 1 tobe
check "8-12 지정 요일: 원본 로그 Full search mode enable"  full_scan_logged asis "$today"
check "8-13 지정 요일: 전환본 로그 Full search mode enable" full_scan_logged tobe "$today"
check "8-14 지정 요일: 두 쪽 모두 기준 시각 0으로 시작"    sh -c 'grep -q "Backup base time  = \[0\]" /w/asis/server.log && grep -q "Backup base time  = \[0\]" /w/tobe/server.log'

echo
if [ "$FAIL" -eq 0 ]; then
    echo "RESULT: ALL PASS"
else
    echo "RESULT: FAILED"
    echo "--- asis server.log (tail)"; tail -15 /w/asis/server.log
    echo "--- tobe server.log (tail)"; tail -15 /w/tobe/server.log
fi
exit "$FAIL"
