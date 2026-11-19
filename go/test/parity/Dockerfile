# 원본(C)과 전환본(Go)을 같은 입력으로 실행해 결과를 비교하는 시험 환경.
# 원본을 원본 그대로(소스 수정 없이) 컴파일한다.
#
#   cd c:\dev\ncc-vbr-go
#   go\build.ps1 linux
#   docker build -f go/test/parity/Dockerfile -t ncc-vbr-parity .
#   docker run --rm ncc-vbr-parity
FROM alpine:3.20

RUN apk add --no-cache gcc musl-dev zlib-dev linux-headers diffutils

WORKDIR /build
COPY cifs/   /build/cifs/
COPY remove/ /build/remove/

# 원본 c1 스크립트와 같은 옵션에 두 가지만 더했다(소스는 고치지 않는다).
#  -fcommon                 : 최신 gcc 에서 헤더의 전역 변수 중복 정의를 허용한다.
#  -Wl,-z,stack-size=8MB    : 원본은 스레드 스택에 1MB 가까운 버퍼를 둔다. 운영 환경(glibc)의 기본 스택 8MB 에 맞춘다.
RUN cd /build/cifs \
 && cc -g -fcommon -w -c bs_common.c -o bs_common.o \
 && cc -g -O -fcommon -w vbr_cifs.c bs_common.o -o vbr_cifs -Wl,-z,stack-size=8388608 -lpthread -D_REENTRANT -lm -lz
# 2.068 재구성판: 2.0672 사본에 사용자가 준 2.068 Full Scan Day 조각을 넣는다(Q-12, C-CIFS-21).
COPY go/test/parity/make-cifs-2068.sh /build/make-cifs-2068.sh
RUN sed -i 's/\r$//' /build/make-cifs-2068.sh \
 && sh /build/make-cifs-2068.sh /build/cifs/vbr_cifs.c /build/cifs/vbr_cifs_2068.c \
 && cd /build/cifs \
 && cc -g -O -fcommon -w vbr_cifs_2068.c bs_common.o -o vbr_cifs_2068 -Wl,-z,stack-size=8388608 -lpthread -D_REENTRANT -lm -lz
RUN cd /build/remove \
 && cc -g -fcommon -w -c bs_common.c -o bs_common.o \
 && cc -g -O -fcommon -w vbr_remove.c bs_common.o -o vbr_remove -Wl,-z,stack-size=8388608 -lpthread -D_REENTRANT -lm -lz

COPY go/bin/linux-amd64/vbr_cifs   /build/go/vbr_cifs
COPY go/bin/linux-amd64/vbr_remove /build/go/vbr_remove
COPY go/test/parity/run.sh         /build/run.sh
RUN sed -i 's/\r$//' /build/run.sh && chmod +x /build/go/* /build/run.sh

CMD ["/build/run.sh"]
