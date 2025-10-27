package main

import (
	"fmt"
	"os"
	"time"

	"knixx/ncn"
	"knixx/tp1"

	"github.com/vapourismo/knx-go/knx/cemi"
	"github.com/vapourismo/knx-go/knx/dpt"
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

func main() {
	var input = make([]byte, 128)
	var n int

	d, err := ftdi.OpenFirst(vendor, product, ftdi.ChannelAny)
	checkErr(err)
	defer d.Close()

	d.Reset()
	d.SetLatencyTimer(latency)
	d.SetBaudrate(baud)
	d.SetLineProperties(ftdi.DataBits8, ftdi.StopBits1, ftdi.ParityNone)
	d.PurgeWriteBuffer() // rx

	var tg = tp1.Telegram{
		Ctrl: tp1.Ctrl{
			Repeat:   false,
			Priority: tp1.PriorityLow},
		Src:  uint16(cemi.NewIndividualAddr3(7, 8, 1)),
		Dest: uint16(cemi.NewGroupAddr3(0, 0, 2)),
		Npci: tp1.Npci{
			AddrType: tp1.AddrGroup,
			Routing:  0},
		Tpci: tp1.Tpci{
			Transport: tp1.UnnumberedDataPacket,
			Sequence:  0},
		Apci: tp1.GroupValueWrite,
		Data: dpt.DPT_1001(false).Pack()}

	if buf, err := tg.Pack(); err == nil {
		fmt.Printf("> % x\n", buf)
		ncn.TxData(d, buf)
	}

	for {
		for n = 0; n == 0; {
			time.Sleep(10 * time.Millisecond)
			n, _ = d.Read(input)
		}
		fmt.Printf("< % x\n", input[:n])
	}
}
