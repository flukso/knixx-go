//go:build tinygo

package main

import (
	"fmt"
	"io"
	"knixx/cli"
	"knixx/outq"
	"machine"
	"runtime"
	"time"
)

// sysCmd shows the state of the firmware: the uptime, the heap, the
// goroutines and the console output queues. A USB sender that stopped
// shows as a full usb queue whose lost count grows.
func sysCmd() cli.Cmd {
	return cli.Cmd{
		Name: "sys",
		Help: "show the uptime, the heap, the goroutines and the console queues",
		Func: func(args []string, w io.Writer) {
			if len(args) != 0 {
				fmt.Fprintln(w, "usage: sys")
				return
			}
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			fmt.Fprintln(w, "uptime    ", time.Since(boot).Round(time.Second))
			fmt.Fprintf(w, "heap       %d of %d bytes in use, %d objects, %d GCs\n",
				m.HeapInuse, m.HeapSys, m.HeapObjects, m.NumGC)
			fmt.Fprintln(w, "goroutines", runtime.NumGoroutine())
			queue(w, "usb", &usbOut)
			queue(w, "nus", &nusOutQ)
			fmt.Fprintln(w, "usb dtr   ", machine.Serial.DTR())
		},
	}
}

// queue shows the state of the output queue q of a console port.
func queue(w io.Writer, name string, q *outq.Queue) {
	fmt.Fprintf(w, "%-10s %d of %d bytes queued, %d lost\n", name+" out", q.Buffered(), outq.Len, q.Lost())
}
