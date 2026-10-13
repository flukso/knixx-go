package light

import (
	"knixx/tp1"
	"strings"

	"github.com/vapourismo/knx-go/knx/dpt"
)

const NumSwitches = 2

type Switch struct {
	Name  string
	Group uint16
}

type Device struct {
	Individual uint16
	Switches   [NumSwitches]Switch
}

// Switch looks up a switch by its case-insensitive name. It returns nil when
// no switch matches.
func (d *Device) Switch(name string) *Switch {
	for i := range d.Switches {
		if strings.EqualFold(d.Switches[i].Name, name) {
			return &d.Switches[i]
		}
	}
	return nil
}

// Telegram builds the group value write that sets sw to the requested state.
func (d *Device) Telegram(sw *Switch, on bool) tp1.Telegram {
	return tp1.Telegram{
		Ctrl: tp1.Ctrl{
			Repeat:   false,
			Priority: tp1.PriorityLow},
		Src:  d.Individual,
		Dest: sw.Group,
		Npci: tp1.Npci{
			AddrType: tp1.AddrGroup,
			Routing:  0},
		Tpci: tp1.Tpci{
			Transport: tp1.UnnumberedDataPacket,
			Sequence:  0},
		Apci: tp1.GroupValueWrite,
		Data: dpt.DPT_1001(on).Pack()}
}
