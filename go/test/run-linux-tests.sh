#!/bin/sh
# Linux 에서 단위 시험 실행 파일(bin/linux-test/*.test)을 돌린다.
# Windows 개발 PC 에서는 WSL 로 실행한다:  wsl -d Ubuntu -- sh /mnt/c/dev/ncc-vbr-go/go/test/run-linux-tests.sh
# Linux 파일 시스템의 시각(atime/ctime)과 심볼릭 링크 동작을 보려고 /tmp 로 복사해 실행한다.
set -eu
here="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d /tmp/vbr-unit.XXXXXX)"
trap 'rm -rf "$work"' EXIT

cp "$here"/bin/linux-test/*.test "$work"/
chmod +x "$work"/*.test
cd "$work"
failed=0
for bin in ./*.test; do
    echo "== $bin"
    if ! "$bin" -test.v > "$bin.out" 2>&1; then
        failed=1
    fi
    grep -E '^(--- |PASS|FAIL|ok)' "$bin.out" || true
done
exit "$failed"
