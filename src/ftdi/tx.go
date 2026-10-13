package main

import (
	"fmt"
	"io"
	"knixx/light"
	"knixx/ncn"
	"os"
	"sync/atomic"
	"time"
)

const (
	txQueueLen = 16
	rxPoll     = 10 * time.Millisecond
)

// sniff enables the hex dump of the bytes exchanged with the transceiver.
var sniff atomic.Bool

func dump(prefix string, buf []byte) {
	if sniff.Load() {
		fmt.Printf("%s % x\n", prefix, buf)
	}
}

type command struct {
	sw *light.Switch
	on bool
}

// transmit sends a telegram for every command received on cmds and dumps
// whatever the transceiver returns in between. It is the only user of d while
// it runs. It returns once cmds is closed and drained, closing done.
func transmit(d io.ReadWriter, dev *light.Device, cmds <-chan command, done chan<- struct{}) {
	var input [128]byte

	defer close(done)
	ticker := time.NewTicker(rxPoll)
	defer ticker.Stop()

	for {
		select {
		case cmd, ok := <-cmds:
			if !ok {
				return
			}
			tg := dev.Telegram(cmd.sw, cmd.on)
			buf, err := tg.Pack()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				continue
			}
			dump(">", buf)
			if _, err := ncn.TxData(d, buf); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		case <-ticker.C:
			if n, _ := d.Read(input[:]); n > 0 {
				dump("<", input[:n])
			}
		}
	}
}
