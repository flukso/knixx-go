// Package bus drives the NCN5121 transceiver: it transmits queued commands
// and reports what the transceiver sends back.
package bus

import (
	"fmt"
	"io"
	"knixx/ncn"
	"knixx/thunder"
	"os"
	"sync/atomic"
	"time"

	"github.com/vapourismo/knx-go/knx/cemi"
	"github.com/vapourismo/knx-go/knx/dpt"
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

// Occupied reports the occupancy that Run last published.
var Occupied atomic.Bool

// occupancyUnit is the unit of thunder.Occupancy.Timeout.
var occupancyUnit = time.Second

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
	CmdSensor
	// CmdMotion reports motion seen by the time-of-flight sensor.
	CmdMotion
)

type Command struct {
	Kind Kind
	// Switch indexes Device.Switches for CmdSwitch.
	Switch uint8
	On     bool
	// Dev replaces the device configuration for CmdConfig.
	Dev thunder.Device
	// Temperature in milli degrees Celsius and Humidity in hundredths of a
	// percent are published to the sensor's groups for CmdSensor.
	Temperature int32
	Humidity    int32
}

// write is a group value write waiting to be sent.
type write struct {
	group uint16
	data  [maxData]byte
	n     uint8
}

// maxData fits the packed values sent: DPT 1 and DPT 9.
const maxData = 3

// occupancy turns motion into occupancy: it is on from the first motion
// until a timeout passed without motion.
type occupancy struct {
	on   bool
	last time.Time
}

// motion records motion at now and reports whether occupancy turns on.
func (o *occupancy) motion(now time.Time) bool {
	o.last = now
	if o.on {
		return false
	}
	o.on = true
	return true
}

// expire reports whether occupancy turns off at now, timeout after the last
// motion.
func (o *occupancy) expire(now time.Time, timeout time.Duration) bool {
	if !o.on || now.Sub(o.last) < timeout {
		return false
	}
	o.on = false
	return true
}

func newWrite(group uint16, data []byte) write {
	w := write{group: group}
	w.n = uint8(copy(w.data[:], data))
	return w
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
// bound group and a sensor command one per bound value, each after the
// previous one was confirmed by L_Data.con or timed out; further commands
// wait until all telegrams are done. Motion turns occupancy on, and the
// occupancy is turned off again once the configured timeout passed without
// motion; both are published as DPT 1.018 to the occupancy group, if it is
// bound. Run is the only
// user of d while it runs, and works on its own copy of dev, which CmdConfig
// replaces. It returns once cmds is closed and drained, closing done.
func Run(d io.ReadWriter, dev *thunder.Device, cmds <-chan Command, done chan<- struct{}) {
	var input [128]byte
	var rx receiver
	cur := *dev

	// The writes of the command being sent, and the index of the next one.
	var queue [thunder.MaxGroups]write
	var nq, next uint8
	// The group whose L_Data.con is awaited.
	var awaiting bool
	var group uint16
	var deadline time.Time
	var occ occupancy

	defer close(done)
	ticker := time.NewTicker(rxPoll)
	defer ticker.Stop()

	for {
		if next < nq && !awaiting {
			w := &queue[next]
			group = w.group
			next++
			if send(d, &cur, group, w.data[:w.n]) {
				awaiting, rx.gotCon = true, false
				deadline = time.Now().Add(conTimeout)
			}
		}

		in := cmds
		if next < nq || awaiting {
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
				data := dpt.DPT_1001(cmd.On).Pack()
				nq, next = 0, 0
				for _, g := range cur.Switches[cmd.Switch].GroupList() {
					queue[nq] = newWrite(g, data)
					nq++
				}
			case CmdSensor:
				nq, next = 0, 0
				if g := cur.Sensor.Temperature; g != 0 {
					queue[nq] = newWrite(g, dpt.DPT_9001(float32(cmd.Temperature)/1000).Pack())
					nq++
				}
				if g := cur.Sensor.Humidity; g != 0 {
					queue[nq] = newWrite(g, dpt.DPT_9007(float32(cmd.Humidity)/100).Pack())
					nq++
				}
			case CmdMotion:
				g := cur.Occupancy.Group
				if g == 0 || !occ.motion(time.Now()) {
					continue
				}
				Occupied.Store(true)
				queue[0] = newWrite(g, dpt.DPT_1018(true).Pack())
				nq, next = 1, 0
			case CmdStat:
				stat(d, &rx, input[:])
			case CmdConfig:
				cur = cmd.Dev
			}
		case <-ticker.C:
			if n, _ := d.Read(input[:]); n > 0 {
				rx.handle(input[:n])
			}
			if awaiting {
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
			timeout := time.Duration(cur.Occupancy.Timeout) * occupancyUnit
			if next >= nq && !awaiting && occ.expire(time.Now(), timeout) {
				Occupied.Store(false)
				if g := cur.Occupancy.Group; g != 0 {
					queue[0] = newWrite(g, dpt.DPT_1018(false).Pack())
					nq, next = 1, 0
				}
			}
		}
	}
}

// send transmits the group value write of data to group. It reports whether
// the telegram was handed to the transceiver.
func send(d io.Writer, dev *thunder.Device, group uint16, data []byte) bool {
	tg := dev.GroupWrite(group, data)
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
