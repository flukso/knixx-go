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
	txQueueLen  = 16
	rxPoll      = 10 * time.Millisecond
	statTimeout = 100 * time.Millisecond
)

// sniff enables the hex dump of the bytes exchanged with the transceiver.
var sniff atomic.Bool

// out receives everything the transmit goroutine prints.
var out io.Writer = os.Stdout

func dump(prefix string, buf []byte) {
	if sniff.Load() {
		fmt.Fprintf(out, "%s % x\n", prefix, buf)
	}
}

type cmdKind uint8

const (
	cmdSwitch cmdKind = iota
	cmdStat
)

type command struct {
	kind cmdKind
	sw   *light.Switch
	on   bool
}

type receiver struct {
	parser ncn.Parser
	// stat requests that a U_State.ind and U_SystemStat.ind be reported in
	// full; the flags record which of the two have been seen.
	stat, gotState, gotSystemStat bool
}

// handle dumps data and reports the services it contains.
func (r *receiver) handle(data []byte) {
	dump("<", data)
	for _, b := range data {
		s, v := r.parser.Feed(b)
		switch {
		case s == ncn.None:
			continue
		case r.stat && s == ncn.U_StateInd && !r.gotState:
			r.gotState = true
		case r.stat && s == ncn.U_SystemStatInd && !r.gotSystemStat:
			r.gotSystemStat = true
		case !sniff.Load():
			continue
		}
		fmt.Fprintf(out, "  %s (%#02x)\n", ncn.Describe(s, v), v)
	}
}

// transmit sends a telegram for every command received on cmds and reports
// whatever the transceiver returns in between. It is the only user of d while
// it runs. It returns once cmds is closed and drained, closing done.
func transmit(d io.ReadWriter, dev *light.Device, cmds <-chan command, done chan<- struct{}) {
	var input [128]byte
	var rx receiver

	defer close(done)
	ticker := time.NewTicker(rxPoll)
	defer ticker.Stop()

	for {
		select {
		case cmd, ok := <-cmds:
			if !ok {
				return
			}
			switch cmd.kind {
			case cmdSwitch:
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
			case cmdStat:
				stat(d, &rx, input[:])
			}
		case <-ticker.C:
			if n, _ := d.Read(input[:]); n > 0 {
				rx.handle(input[:n])
			}
		}
	}
}

// stat requests the state and system status of the transceiver and waits up
// to statTimeout for both replies.
func stat(d io.ReadWriter, rx *receiver, input []byte) {
	req := [...]byte{ncn.U_StateReq, ncn.U_SystemStatReq}
	dump(">", req[:])
	if _, err := d.Write(req[:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	rx.stat, rx.gotState, rx.gotSystemStat = true, false, false
	defer func() { rx.stat = false }()

	for deadline := time.Now().Add(statTimeout); time.Now().Before(deadline); {
		time.Sleep(rxPoll)
		if n, _ := d.Read(input); n > 0 {
			rx.handle(input[:n])
		}
		if rx.gotState && rx.gotSystemStat {
			return
		}
	}
	if !rx.gotState {
		fmt.Fprintln(out, "  no U_State.ind reply")
	}
	if !rx.gotSystemStat {
		fmt.Fprintln(out, "  no U_SystemStat.ind reply")
	}
}
