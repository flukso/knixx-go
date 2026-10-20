//go:build tinygo

package main

import (
	"device/arm"
	"fmt"
	"io"
	"knixx/cli"
	"knixx/led"
	"knixx/nus"
	"knixx/outq"
	"knixx/switches"
	"machine"
	"sync/atomic"
	"time"

	"tinygo.org/x/bluetooth"
)

// nusName is the advertised name. With the flags and the 128-bit NUS UUID,
// the advertising packet leaves room for a name of 8 bytes.
const nusName = "knixx"

// nusFlushTimeout bounds the wait for the queued output when NUS is turned
// off while a central is connected.
const nusFlushTimeout = time.Second

// sdClockHFCLKRequest is the SVC number of sd_clock_hfclk_request in the
// S140 6.1.1 nrf_soc.h: SOC_SVC_BASE_NOT_AVAILABLE + 22.
const sdClockHFCLKRequest = 0x2C + 22

var (
	// bleEnabled records that the SoftDevice runs and the NUS service is
	// registered; neither can be undone.
	bleEnabled bool

	nusAdv *bluetooth.Advertisement
	nusTx  bluetooth.Characteristic

	// nusRx receives the writes of the central, from the SoftDevice event
	// interrupt.
	nusRx nus.Ring

	// Set from the SoftDevice event interrupt: whether a central is
	// connected, and its connection.
	nusConnected atomic.Bool
	nusCentral   bluetooth.Device

	// wasConnected is the connection state the console goroutine last saw,
	// and idle the timer that runs while no central is connected.
	wasConnected bool
	idle         nus.Timer

	// nusOutQ queues the console output for NUS, which a sender goroutine
	// hands to nusOut.
	nusOutQ outq.Queue
	nusOut  = nus.Writer{
		Send: func(p []byte) error {
			_, err := nusTx.Write(p)
			return err
		},
		Connected: nusConnected.Load,
	}
)

// nusPort is the console port on NUS.
type nusPort struct{}

func (nusPort) Buffered() int           { return nusRx.Buffered() }
func (nusPort) ReadByte() (byte, error) { return nusRx.ReadByte() }
func (nusPort) out() *outq.Queue        { return &nusOutQ }
func onNUS() bool                       { return active == port(nusPort{}) }

// enableBLE enables the SoftDevice and registers the NUS service.
func enableBLE() error {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return err
	}
	// USB needs the HFXO, which the SoftDevice would otherwise stop when
	// the radio is idle.
	if code := arm.SVCall0(sdClockHFCLKRequest); code != 0 {
		return fmt.Errorf("hfclk request: error %#x", code)
	}
	adapter.SetConnectHandler(func(d bluetooth.Device, connected bool) {
		// Runs in the SoftDevice event interrupt.
		if connected {
			nusCentral = d
		}
		nusConnected.Store(connected)
	})
	var rx bluetooth.Characteristic
	if err := adapter.AddService(&bluetooth.Service{
		UUID: bluetooth.ServiceUUIDNordicUART,
		Characteristics: []bluetooth.CharacteristicConfig{
			{
				Handle: &rx,
				UUID:   bluetooth.CharacteristicUUIDUARTRX,
				Flags:  bluetooth.CharacteristicWritePermission | bluetooth.CharacteristicWriteWithoutResponsePermission,
				WriteEvent: func(_ bluetooth.Connection, _ int, value []byte) {
					// Runs in the SoftDevice event interrupt.
					nusRx.Put(value)
				},
			},
			{
				Handle: &nusTx,
				UUID:   bluetooth.CharacteristicUUIDUARTTX,
				Flags:  bluetooth.CharacteristicNotifyPermission | bluetooth.CharacteristicReadPermission,
			},
		},
	}); err != nil {
		return err
	}
	nusAdv = adapter.DefaultAdvertisement()
	return nusAdv.Configure(bluetooth.AdvertisementOptions{
		LocalName:    nusName,
		ServiceUUIDs: []bluetooth.UUID{bluetooth.ServiceUUIDNordicUART},
	})
}

// nusOn starts advertising NUS and moves the console to it.
func nusOn() error {
	if !bleEnabled {
		if err := enableBLE(); err != nil {
			return err
		}
		bleEnabled = true
	}
	if err := nusAdv.Start(); err != nil {
		return err
	}
	nusRx.Reset()
	wasConnected = false
	idle.Arm(time.Now())
	attach(nusPort{}, true)
	led.Notify(leds, led.Trigger{Mode: led.BLE})
	return nil
}

// nusOff stops advertising, drops the central and moves the console back to
// USB. The SoftDevice stays enabled.
func nusOff() {
	// Stopping first keeps the disconnect from restarting the advertising.
	// While connected the advertising is stopped already, which is fine.
	nusAdv.Stop()
	if nusConnected.Load() {
		// Let the sender hand the queued output, such as the reply to nus
		// off, to the central before the link drops.
		for deadline := time.Now().Add(nusFlushTimeout); nusOutQ.Buffered() > 0 && time.Now().Before(deadline); {
			time.Sleep(outputPoll)
		}
		nusCentral.Disconnect()
	}
	nusOutQ.Reset()
	idle.Disarm()
	led.Notify(leds, led.Trigger{Mode: led.BLE, Cancel: true})
	// Drop what was typed on USB in the meantime.
	for machine.Serial.Buffered() > 0 {
		machine.Serial.ReadByte()
	}
	attach(usbPort{}, false)
}

// pollNUS follows the connection of the central and gives the console back
// to USB once no central connected for nus.IdleTimeout. It runs on the
// console goroutine.
func pollNUS() {
	if !onNUS() {
		return
	}
	now := time.Now()
	if c := nusConnected.Load(); c != wasConnected {
		wasConnected = c
		if c {
			idle.Disarm()
			// The banner follows on the central's first key, once it
			// subscribed to the notifications.
			editor.Discard()
			greeted = false
		} else {
			idle.Arm(now)
		}
	}
	if idle.Expired(now) {
		nusOff()
		fmt.Fprintf(console, "\nnus: no BLE connection for %d s, console back on USB\n%s",
			int(nus.IdleTimeout/time.Second), prompt)
	}
}

// nusCmd moves the console between USB and BLE.
func nusCmd() cli.Cmd {
	return cli.Cmd{
		Name: "nus",
		Help: "nus on|off: move the console to BLE (Nordic UART Service) or back to USB",
		Func: func(args []string, w io.Writer) {
			switch {
			case len(args) == 0:
				nusState(w)
			case len(args) == 1 && args[0] == "on":
				nusStart(w)
			case len(args) == 1 && args[0] == "off":
				nusStop(w)
			default:
				fmt.Fprintln(w, "usage: nus [on|off]")
			}
		},
	}
}

// nusStart is nus on: it moves the console to BLE, reporting on w.
func nusStart(w io.Writer) {
	if onNUS() {
		fmt.Fprintln(w, "nus: already on")
		return
	}
	fmt.Fprintf(w, "nus: advertising as %q; the console moves to BLE, back to USB after %d s without a connection, on nus off or on reset\n",
		nusName, int(nus.IdleTimeout/time.Second))
	if err := nusOn(); err != nil {
		fail("nus:", err)
	}
}

// nusStop is nus off: it moves the console back to USB, reporting on w and
// on USB.
func nusStop(w io.Writer) {
	if !onNUS() {
		fmt.Fprintln(w, "nus: not on")
		return
	}
	fmt.Fprintln(w, "nus: off, console back on USB")
	nusOff()
	fmt.Fprintln(console, "nus: off, console back on USB")
}

// NUS requests of the wall switch gestures, see requestNUS.
const (
	nusKeep int32 = iota
	nusWantOn
	nusWantOff
)

// nusRequest holds the latest NUS request of the wall switches. The switch
// goroutine sets it; the console goroutine, which owns the console port,
// carries it out in pollSwitchNUS.
var nusRequest atomic.Int32

// requestNUS asks the console goroutine for nus on or off.
func requestNUS(on bool) {
	if on {
		nusRequest.Store(nusWantOn)
	} else {
		nusRequest.Store(nusWantOff)
	}
}

// pollSwitchNUS carries out the NUS request of the wall switches as the nus
// command would. It runs on the console goroutine.
func pollSwitchNUS() {
	switch nusRequest.Swap(nusKeep) {
	case nusWantOn:
		fmt.Fprintf(console, "\nswitches: A on + B off held %d s\n", int(switches.HoldTime/time.Second))
		nusStart(console)
	case nusWantOff:
		fmt.Fprintf(console, "\nswitches: A off + B on held %d s\n", int(switches.HoldTime/time.Second))
		nusStop(console)
	default:
		return
	}
	console.Write([]byte(prompt))
}

// nusState reports whether the console runs on BLE.
func nusState(w io.Writer) {
	switch {
	case !onNUS():
		fmt.Fprintln(w, "nus: off")
	case nusConnected.Load():
		fmt.Fprintln(w, "nus: on, connected")
	default:
		fmt.Fprintf(w, "nus: on, advertising, %d s left\n", int64(idle.Left(time.Now())/time.Second))
	}
}
