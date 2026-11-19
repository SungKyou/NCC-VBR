// fakexapi 는 demo.ps1 자동 모드가 쓰는 가짜 XAPI 서버다(go/internal/vm/vmtest.Fake 를 띄운다).
//
//	fakexapi [-power halted|running] [-slow]
//
// 시작하면 "ADDR=<호스트:포트>" 한 줄을 출력하고 종료될 때까지 기다린다.
// -slow 면 /export 가 절반을 보낸 뒤 요청이 끊길 때까지 멈춘다(취소 시험용).
// 시험용 암호는 vmtest.Password 다(실제 암호가 아니다).
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"ncc-vbr/internal/vm/vmtest"
)

func main() {
	power := flag.String("power", "halted", "VM power_state")
	slow := flag.Bool("slow", false, "block /export after half of the body")
	flag.Parse()
	f := vmtest.New()
	f.PowerState = *power
	if *slow {
		f.SlowExport = make(chan struct{})
	}
	fmt.Printf("ADDR=%s\n", f.Addr())
	_ = os.Stdout.Sync()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	f.Close()
}
