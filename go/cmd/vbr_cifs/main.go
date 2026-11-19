// vbr_cifs 는 NAS(CIFS) 디렉터리 증분 백업 명령이다.
//
//	vbr_cifs [-home dir] [-config file] [-foreground] source-dir target-dir seq-no
package main

import (
	"os"

	"ncc-vbr/internal/boot"
	"ncc-vbr/internal/cifs"
)

func main() {
	app := boot.Start(boot.Spec{
		Title:    cifs.Title,
		Version:  cifs.Version,
		Usage:    "source-dir target-dir seq-no",
		ArgCount: 3,
		Section:  cifs.Section,
	})
	defer app.Close()

	summary, err := cifs.Run(app.Context, cifs.Job{
		Source: app.Args[0],
		Target: app.Args[1],
		Seq:    app.Args[2],
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
