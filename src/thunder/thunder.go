// Package thunder models the knixx thunder board: its individual address, its
// switches and its sensor, and builds the telegrams they send.
package thunder

import (
	"knixx/tp1"
	"strings"

	"github.com/vapourismo/knx-go/knx/dpt"
)

const (
	NumSwitches = 2
	// MaxGroups bounds the group addresses a switch can be bound to.
	MaxGroups = 8
)

// Switch sends to each of its group addresses Groups[:NGroups].
type Switch struct {
	Name    string
	Groups  [MaxGroups]uint16
	NGroups uint8
}

// NewSwitch returns a switch bound to groups. Groups beyond MaxGroups are
// dropped.
func NewSwitch(name string, groups ...uint16) Switch {
	sw := Switch{Name: name}
	sw.NGroups = uint8(copy(sw.Groups[:], groups))
	return sw
}

// GroupList returns the group addresses the switch is bound to.
func (s *Switch) GroupList() []uint16 {
	return s.Groups[:s.NGroups]
}

// Sensor holds the group addresses that temperature and humidity readings
// are published to. 0 leaves a value unbound.
type Sensor struct {
	Temperature uint16
	Humidity    uint16
}

type Device struct {
	Individual uint16
	Switches   [NumSwitches]Switch
	Sensor     Sensor
}

// Switch looks up a switch by its case-insensitive name. It returns nil when
// no switch matches.
func (d *Device) Switch(name string) *Switch {
	if i, ok := d.SwitchIndex(name); ok {
		return &d.Switches[i]
	}
	return nil
}

// SwitchIndex returns the index of the switch with the case-insensitive
// name.
func (d *Device) SwitchIndex(name string) (uint8, bool) {
	for i := range d.Switches {
		if strings.EqualFold(d.Switches[i].Name, name) {
			return uint8(i), true
		}
	}
	return 0, false
}

// Telegram builds the group value write that sets group to the requested
// state.
func (d *Device) Telegram(group uint16, on bool) tp1.Telegram {
	return d.GroupWrite(group, dpt.DPT_1001(on).Pack())
}

// GroupWrite builds the group value write of data, a value packed by a knx-go
// dpt type, to group.
func (d *Device) GroupWrite(group uint16, data []byte) tp1.Telegram {
	return tp1.Telegram{
		Ctrl: tp1.Ctrl{
			Repeat:   false,
			Priority: tp1.PriorityLow},
		Src:  d.Individual,
		Dest: group,
		Npci: tp1.Npci{
			AddrType: tp1.AddrGroup,
			Routing:  0},
		Tpci: tp1.Tpci{
			Transport: tp1.UnnumberedDataPacket,
			Sequence:  0},
		Apci: tp1.GroupValueWrite,
		Data: data}
}
