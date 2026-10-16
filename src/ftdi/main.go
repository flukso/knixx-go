package main

import (
	"flag"
	"fmt"
	"knixx/bus"
	"knixx/cli"
	"knixx/config"
	"os"

	"github.com/abiosoft/ishell"
	"github.com/ziutek/ftdi"
)

func checkErr(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

const (
	vendor  = 0x0403
	product = 0x6001
	baud    = 38400
	latency = 16 // ms
)

var configPath = flag.String("config", "knixx.ini", "Path to the ini file")

func main() {
	flag.Parse()

	dev, err := config.Load(*configPath)
	checkErr(err)

	d, err := ftdi.OpenFirst(vendor, product, ftdi.ChannelAny)
	checkErr(err)
	defer d.Close()

	d.Reset()
	d.SetLatencyTimer(latency)
	d.SetBaudrate(baud)
	d.SetLineProperties(ftdi.DataBits8, ftdi.StopBits1, ftdi.ParityNone)
	d.PurgeWriteBuffer() // rx

	cmds := make(chan bus.Command, bus.QueueLen)
	done := make(chan struct{})
	go bus.Run(d, dev, cmds, done)

	shell := ishell.New()
	for _, cmd := range cli.Commands(dev, cmds) {
		shell.AddCmd(&ishell.Cmd{
			Name: cmd.Name,
			Help: cmd.Help,
			Func: func(c *ishell.Context) { cmd.Func(c.Args, os.Stdout) },
		})
	}
	shell.Run()

	close(cmds)
	<-done
}
