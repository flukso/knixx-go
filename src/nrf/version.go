//go:build tinygo

package main

import (
	"fmt"
	"io"
	"knixx/cli"
	"knixx/version"
)

// versionCmd shows the commit and build time of the firmware.
func versionCmd() cli.Cmd {
	return cli.Cmd{
		Name: "version",
		Help: "show the commit and build time of the firmware",
		Func: func(args []string, w io.Writer) {
			if len(args) != 0 {
				fmt.Fprintln(w, "usage: version")
				return
			}
			fmt.Fprintln(w, version.String())
		},
	}
}
