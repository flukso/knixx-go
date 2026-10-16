// Package bus drives the NCN5121 transceiver: it transmits queued commands
// and reports what the transceiver sends back.
package bus

import (
	"fmt"
	"io"
	"knixx/light"
	"knixx/ncn"
	"os"
	"sync/atomic"
	"time"

	"github.com/vapourismo/knx-go/knx/cemi"
)

const (
	QueueLen    = 16
	rxPoll      = 10 * time.Millisecond
	statTimeout = 100 * time.Millisecond
)

// conTimeout bounds the wait for the L_Data.con of a sent telegram. It
// covers the NCN5121's worst case of 3 NACK and 3 BUSY repetitions.
var conTimeout = 500 * time.Millisecond

// Sniff enables the hex dump of the bytes exchanged with the transceiver.
var Sniff atomic.Bool

// Out receives everything Run prints.
var Out io.Writer = os.Stdout

func dump(prefix string, buf []byte) {
	if Sniff.Load() {
		fmt.Fprintf(Out, "%s % x\n", prefix, buf)
	}
}

type Kind uint8

const (
	CmdSwitch Kind = iota
	CmdStat
	CmdConfig
)

type Command struct {
	Kind Kind
	// Switch indexes Device.Switches for CmdSwitch.
	Switch uint8
	On     bool
	// Dev replaces the device configuration for CmdConfig.
	Dev light.Device
}

type receiver struct {
	parser ncn.Parser
	// stat requests that a U_State.ind and U_SystemStat.ind be reported in
	// full; the flags record which of the two have been seen.
	stat, gotState, gotSystemStat bool
	// gotCon records an L_Data.con and conPositive its outcome.
	gotCon, conPositive bool
}

// handle dumps data and reports the services it contains.
func (r *receiver) handle(data []byte) {
	dump("<", data)
	for _, b := range data {
		s, v := r.parser.Feed(b)
		if s == ncn.L_DataCon {
			r.gotCon, r.conPositive = true, ncn.ConPositive(v)
		}
		switch {
		case s == ncn.None:
			continue
		case r.stat && s == ncn.U_StateInd && !r.gotState:
			r.gotState = true
		case r.stat && s == ncn.U_SystemStatInd && !r.gotSystemStat:
			r.gotSystemStat = true
		case !Sniff.Load():
			continue
		}
		fmt.Fprintf(Out, "  %s (%#02x)\n", ncn.Describe(s, v), v)
	}
}

// Run executes the commands received on cmds and reports whatever the
// transceiver returns in between. A switch command sends one telegram per
// bound group, each after the previous one was confirmed by L_Data.con or
// timed out; further commands wait until all groups are done. Run is the only
// user of d while it runs, and works on its own copy of dev, which CmdConfig
// replaces. It returns once cmds is closed and drained, closing done.
func Run(d io.ReadWriter, dev *light.Device, cmds <-chan Command, done chan<- struct{}) {
	var input [128]byte
	var rx receiver
	cur := *dev

	// The switch command being sent, and the index of its next group.
	var pending bool
	var sw *light.Switch
	var on bool
	var next uint8
	// The group whose L_Data.con is awaited.
	var awaiting bool
	var group uint16
	var deadline time.Time

	defer close(done)
	ticker := time.NewTicker(rxPoll)
	defer ticker.Stop()

	for {
		if pending && !awaiting {
			if next == sw.NGroups {
				pending = false
			} else {
				group = sw.Groups[next]
				next++
				if send(d, &cur, group, on) {
					awaiting, rx.gotCon = true, false
					deadline = time.Now().Add(conTimeout)
				}
			}
		}

		in := cmds
		if pending || awaiting {
			in = nil
		}
		select {
		case cmd, ok := <-in:
			if !ok {
				return
			}
			switch cmd.Kind {
			case CmdSwitch:
				if int(cmd.Switch) >= len(cur.Switches) {
					fmt.Fprintln(Out, "bus: no switch", cmd.Switch)
					continue
				}
				pending, sw, on, next = true, &cur.Switches[cmd.Switch], cmd.On, 0
			case CmdStat:
				stat(d, &rx, input[:])
			case CmdConfig:
				cur = cmd.Dev
			}
		case <-ticker.C:
			if n, _ := d.Read(input[:]); n > 0 {
				rx.handle(input[:n])
			}
			if !awaiting {
				continue
			}
			switch {
			case rx.gotCon:
				awaiting = false
				if !rx.conPositive && !Sniff.Load() {
					fmt.Fprintln(Out, "  L_Data.con: negative for", cemi.GroupAddr(group))
				}
			case time.Now().After(deadline):
				awaiting = false
				fmt.Fprintln(Out, "  no L_Data.con for", cemi.GroupAddr(group))
			}
		}
	}
}

// send transmits the telegram setting group to on. It reports whether the
// telegram was handed to the transceiver.
func send(d io.Writer, dev *light.Device, group uint16, on bool) bool {
	tg := dev.Telegram(group, on)
	buf, err := tg.Pack()
	if err != nil {
		fmt.Fprintln(Out, err)
		return false
	}
	dump(">", buf)
	if _, err := ncn.TxData(d, buf); err != nil {
		fmt.Fprintln(Out, err)
		return false
	}
	return true
}

// stat requests the state and system status of the transceiver and waits up
// to statTimeout for both replies.
func stat(d io.ReadWriter, rx *receiver, input []byte) {
	req := [...]byte{ncn.U_StateReq, ncn.U_SystemStatReq}
	dump(">", req[:])
	if _, err := d.Write(req[:]); err != nil {
		fmt.Fprintln(Out, err)
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
		fmt.Fprintln(Out, "  no U_State.ind reply")
	}
	if !rx.gotSystemStat {
		fmt.Fprintln(Out, "  no U_SystemStat.ind reply")
	}
}
