#!/bin/sh
# 원본 2.0672 의 사본에 2.068 의 Full Scan Day 코드를 넣어 2.068 재구성판을 만든다.
#
#   make-cifs-2068.sh 원본.c 결과.c
#
# 2.068 원본 소스는 저장소에 없다(Q-12). 사용자가 2026-11-18 에 준 2.068 추가 코드 조각만
# 그대로 넣는다. 저장소의 원본(cifs/vbr_cifs.c)은 고치지 않고 컨테이너 안의 사본만 바꾼다.
#
# 넣는 위치는 조각에서 정했다(2.068 의 실제 위치는 확인하지 못했다).
#  - 전역 변수 full_scan_day : backup_time 선언 다음
#  - 설정 읽기와 요일 비교    : lt_today = localtime(&backup_start_time); 다음
#    (조각이 lt_today 를 쓰므로 이보다 앞일 수 없다. 기준 시각 로그보다 앞에 둔다)
set -eu

src="$1"
out="$2"

awk '
/^long[ \t]+backup_time;/ {
    print
    print "char\t\t\tfull_scan_day;\t\t\t\t// 2.068 재구성(시험용)"
    decl = 1
    next
}
/^[ \t]*lt_today = localtime\(&backup_start_time\);/ && !body {
    print
    print "    // 2.068 특정 요일은 full scan backup으로 변경"
    print "    if (BS_GetProfileString(\"VBR Manager\", \"Full Scan Day\", buffer, sizeof(buffer)) == FALSE)"
    print "    {"
    print "        full_scan_day   =       '\''0'\'';    // default 설정을 일요일로"
    print "        BS_Tsyslog(\"Config file[%s] [%s] parameter not found.(Sunday default setting)..\\n\", CONFIG_FILE, \"Full Scan Day\");"
    print "    }"
    print "    else"
    print "    {"
    print "        full_scan_day   =       buffer[0];"
    print "    }"
    print ""
    print "    if ((full_scan_day - '\''0'\'') == lt_today->tm_wday)"
    print "    {"
    print "        backup_time     = 0L;"
    print "        BS_Tsyslog(\"Full search mode enable.[%c]\\n\", full_scan_day);"
    print "    }"
    body = 1
    next
}
{ print }
END {
    if (!decl || !body) {
        print "make-cifs-2068: insertion point not found" > "/dev/stderr"
        exit 1
    }
}
' "$src" > "$out"
