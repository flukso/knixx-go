package main

import (
	"flag"
	"fmt"
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

	cmds := make(chan command, txQueueLen)
	done := make(chan struct{})
	go transmit(d, dev, cmds, done)

	shell := ishell.New()
	shell.AddCmd(&ishell.Cmd{
		Name: "switch",
		Help: switchUsage,
		Func: func(c *ishell.Context) {
			name, on, err := parseSwitch(c.Args)
			if err != nil {
				c.Println(err)
				return
			}
			sw := dev.Switch(name)
			if sw == nil {
				c.Println("unknown switch:", name)
				return
			}
			select {
			case cmds <- command{sw, on}:
			default:
				c.Println("tx queue full")
			}
		},
	})
	shell.AddCmd(&ishell.Cmd{
		Name: "sniff",
		Help: sniffUsage,
		Func: func(c *ishell.Context) {
			switch len(c.Args) {
			case 0:
				if sniff.Load() {
					c.Println("sniff is on")
				} else {
					c.Println("sniff is off")
				}
			case 1:
				on, ok := parseOnOff(c.Args[0])
				if !ok {
					c.Println(sniffUsage)
					return
				}
				sniff.Store(on)
			default:
				c.Println(sniffUsage)
			}
		},
	})
	shell.Run()

	close(cmds)
	<-done
}
