# agent 원본(C 1.602)과 전환본(Go)을 같은 요청으로 비교하는 시험 환경.
# 원본은 소스를 고치지 않고 컴파일한다.
#
#   cd c:\dev\ncc-vbr-go
#   go\build.ps1 linux           (vbr_agent, vbr_secret, fakexapi, driver 포함)
#   docker build -f go/test/agent-parity/Dockerfile -t ncc-vbr-agent-parity .
#   docker run --rm --name ncc-vbr-agent-parity ncc-vbr-agent-parity
FROM alpine:3.20

RUN apk add --no-cache gcc musl-dev zlib-dev linux-headers psmisc

WORKDIR /build
COPY agent/ /build/agent/

# 원본 c1.sh 와 같은 옵션에 cifs 비교 시험과 같은 두 가지만 더했다.
#  -fcommon               : 헤더의 전역 변수 중복 정의 허용(최신 gcc)
#  -Wl,-z,stack-size=8MB  : 원본은 스레드 스택에 300KB 버퍼를 여러 개 둔다. glibc 기본 스택에 맞춘다.
RUN cd /build/agent \
 && cc -g -fcommon -w -c bs_common.c -o bs_common.o \
 && cc -g -O -fcommon -w vbr_agent.c bs_common.o -o vbr_agent -Wl,-z,stack-size=8388608 -lpthread -D_REENTRANT -lm -lz \
 && cc -g -O -fcommon -w dummy2.c bs_common.o -o dummy2 -Wl,-z,stack-size=8388608 -lpthread -D_REENTRANT -lm -lz

COPY go/bin/linux-amd64/vbr_agent  /build/go/vbr_agent
COPY go/bin/linux-amd64/vbr_secret /build/go/vbr_secret
COPY go/bin/linux-amd64/fakexapi   /build/go/fakexapi
COPY go/bin/linux-amd64/driver     /build/go/driver
COPY go/test/agent-parity/fake-bin/ /p/fake-bin/
COPY go/test/agent-parity/run.sh   /build/run.sh
RUN sed -i 's/\r$//' /build/run.sh /p/fake-bin/* && chmod +x /build/go/* /build/run.sh /p/fake-bin/*

CMD ["/build/run.sh"]
