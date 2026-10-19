//go:build tinygo

package main

import (
	"knixx/bus"
	"knixx/cli"
	"knixx/switches"
	"knixx/thunder"
	"machine"
	"sync/atomic"
	"time"
)

// switchPoll is the period at which presses and gestures are picked up.
const switchPoll = 20 * time.Millisecond

// contact is one throw of a wall switch: a press does switch <name> <on|off>.
// The contacts are debounced on the board and pull their pin low.
type contact struct {
	pin  machine.Pin
	name string
	on   bool
	// presses counts the falling edges; the interrupt handler only
	// increments it.
	presses atomic.Uint32
	edges   switches.Edges
}

var contacts = [...]*contact{
	{pin: machine.D9, name: "A", on: true},   // P0.26
	{pin: machine.D10, name: "A", on: false}, // P0.27
	{pin: machine.A0, name: "B", on: true},   // P0.04
	{pin: machine.A1, name: "B", on: false},  // P0.05
}

// The contacts of the gestures that move the console between USB and BLE.
var aOn, aOff, bOn, bOff = contacts[0], contacts[1], contacts[2], contacts[3]

// pressed reports whether c is held down.
func (c *contact) pressed() bool { return !c.pin.Get() }

// runSwitches turns presses of the wall switches into switch commands, as
// the switch command does, and the gestures held for switches.HoldTime into
// nus on (A on + B off) and nus off (A off + B on). A gesture starts with
// presses of its contacts, which switch the lights as usual.
func runSwitches(dev *thunder.Device, cmds chan<- bus.Command) {
	for _, c := range contacts {
		c.pin.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
		if err := c.pin.SetInterrupt(machine.PinFalling, func(machine.Pin) {
			c.presses.Add(1)
		}); err != nil {
			fail("switches: interrupt:", err)
			return
		}
	}

	var nusOnHold, nusOffHold switches.Hold
	for {
		time.Sleep(switchPoll)

		for _, c := range contacts {
			if c.edges.Changed(c.presses.Load()) {
				cli.Switch(dev, cmds, c.name, c.on, console)
			}
		}

		now := time.Now()
		if nusOnHold.Update(now, aOn.pressed() && bOff.pressed()) {
			requestNUS(true)
		}
		if nusOffHold.Update(now, aOff.pressed() && bOn.pressed()) {
			requestNUS(false)
		}
	}
}
