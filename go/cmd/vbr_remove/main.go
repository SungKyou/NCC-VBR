// vbr_remove 는 보존 기간이 지난 이전 버전 파일 삭제 명령이다.
//
//	vbr_remove [-home dir] [-config file] [-foreground] delete-dir days
package main

import (
	"os"

	"ncc-vbr/internal/boot"
	"ncc-vbr/internal/remove"
)

func main() {
	app := boot.Start(boot.Spec{
		Title:    remove.Title,
		Version:  remove.Version,
		Usage:    "delete-dir days",
		ArgCount: 2,
		Section:  remove.Title,
		Check:    checkArgs,
	})
	defer app.Close()

	// checkArgs 를 통과했으므로 오류가 없다.
	days, _ := remove.ParseDays(app.Args[1])
	summary, err := remove.Run(app.Context, remove.Job{
		Dir:    app.Args[0],
		Days:   days,
		Config: app.Config,
		Log:    app.Log,
	})
	switch {
	case err != nil:
		os.Exit(boot.ExitFatal)
	case summary.Errors > 0:
		os.Exit(boot.ExitWithErrors)
	}
}

// checkArgs 는 보존 일수가 0 이상의 정수인지 본다.
func checkArgs(args []string) error {
	_, err := remove.ParseDays(args[1])
	return err
}
