#!/bin/sh
# NCC-VBR Go 빌드 (Linux/macOS 셸용). 원본 c1 스크립트를 대신한다.
#   ./build.sh            : linux/amd64 와 windows/amd64 를 모두 만든다
#   ./build.sh linux      : linux/amd64 만 만든다
#   ./build.sh test       : 단위 시험만 실행한다
# 산출물: bin/<os>-<arch>/vbr_cifs, vbr_remove, vbr_agent, vbr_secret
#         linux 에는 agent 비교 시험 도구 fakexapi, driver 도 만든다
set -eu
cd "$(dirname "$0")"

COMMANDS="vbr_cifs vbr_remove vbr_agent vbr_secret"
TEST_TOOLS="fakexapi driver"

build() {
    os="$1"
    arch="$2"
    ext=""
    [ "$os" = "windows" ] && ext=".exe"
    out="bin/${os}-${arch}"
    mkdir -p "$out"
    for cmd in $COMMANDS; do
        echo "build ${os}/${arch} ${cmd}"
        CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
            go build -trimpath -ldflags "-s -w" -o "${out}/${cmd}${ext}" "./cmd/${cmd}"
    done
    if [ "$os" = "linux" ]; then
        for tool in $TEST_TOOLS; do
            echo "build ${os}/${arch} ${tool} (test tool)"
            CGO_ENABLED=0 GOOS="$os" GOARCH="$arch"                 go build -trimpath -o "${out}/${tool}" "./test/agent-parity/${tool}"
        done
    fi
}

case "${1:-all}" in
    test)    go vet ./... && go test ./... ;;
    linux)   build linux amd64 ;;
    windows) build windows amd64 ;;
    all)     go vet ./... && build linux amd64 && build windows amd64 ;;
    *)       echo "Usage: $0 [all|linux|windows|test]"; exit 1 ;;
esac
