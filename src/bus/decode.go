package bus

import (
	"fmt"
	"knixx/thunder"
	"knixx/tp1"
	"strings"

	"github.com/vapourismo/knx-go/knx/cemi"
	"github.com/vapourismo/knx-go/knx/dpt"
)

// dptKind is the datapoint type of a bound group.
type dptKind uint8

const (
	dptNone dptKind = iota
	dpt1001
	dpt1018
	dpt9001
	dpt9004
	dpt9007
	// dptConflict marks a main/middle group with bindings of different
	// types.
	dptConflict
)

// value is a datapoint type that decodes a group value.
type value interface {
	Unpack(data []byte) error
	String() string
}

// id returns the DPT ID of k and a value of its type to unpack into.
func (k dptKind) id() (string, value) {
	switch k {
	case dpt1001:
		return "1.001", new(dpt.DPT_1001)
	case dpt1018:
		return "1.018", new(dpt.DPT_1018)
	case dpt9001:
		return "9.001", new(dpt.DPT_9001)
	case dpt9004:
		return "9.004", new(dpt.DPT_9004)
	case dpt9007:
		return "9.007", new(dpt.DPT_9007)
	}
	return "", nil
}

// bound is a group bound in the configuration.
type bound struct {
	group uint16
	name  string
	kind  dptKind
}

// decoder renders frames for the sniffer. It decodes the value of a group
// with the datapoint type of its binding in dev: DPT 1.001 for a switch
// group, DPT 9 for a sensor group and DPT 1.018 for the occupancy group. A
// group that is not bound gets the type of the bindings on its main/middle
// group, if they agree.
//
// The bindings are looked up in tables that are built on the first frame
// and dropped by reset when the configuration changes.
type decoder struct {
	dev   *thunder.Device
	valid bool
	bound [thunder.NumSwitches*thunder.MaxGroups + 4]bound
	n     int
	// lines holds the type of each main/middle group, indexed by the high
	// octet of the group address.
	lines [256]dptKind
}

// reset drops the tables, for a change of dev.
func (d *decoder) reset() {
	d.valid = false
}

// build fills the tables from dev.
func (d *decoder) build() {
	d.n = 0
	d.lines = [256]dptKind{}
	add := func(group uint16, name string, kind dptKind) {
		if group == 0 {
			return
		}
		d.bound[d.n] = bound{group, name, kind}
		d.n++
		switch line := &d.lines[group>>8]; *line {
		case dptNone:
			*line = kind
		case kind:
		default:
			*line = dptConflict
		}
	}
	for i := range d.dev.Switches {
		for _, g := range d.dev.Switches[i].GroupList() {
			add(g, d.dev.Switches[i].Name, dpt1001)
		}
	}
	add(d.dev.Sensor.Temperature, "temperature", dpt9001)
	add(d.dev.Sensor.Humidity, "humidity", dpt9007)
	add(d.dev.Sensor.Illuminance, "illuminance", dpt9004)
	add(d.dev.Occupancy.Group, "occupancy", dpt1018)
	d.valid = true
}

// describe renders a frame from the bus: the source and destination, the
// service with its data, the datapoint type and value of a group, and the
// priority and routing counter.
func (d *decoder) describe(frame []byte) string {
	var tg tp1.Telegram
	if err := tg.Unpack(frame); err != nil {
		return "L_Data.ind: " + err.Error()
	}

	var b strings.Builder
	fmt.Fprintf(&b, "L_Data.ind: %v > ", cemi.IndividualAddr(tg.Src))
	if tg.AddrType == tp1.AddrGroup {
		b.WriteString(cemi.GroupAddr(tg.Dest).String())
	} else {
		b.WriteString(cemi.IndividualAddr(tg.Dest).String())
	}
	fmt.Fprintf(&b, " %v", tg.Apci)
	groupValue := tg.Apci == tp1.GroupValueWrite || tg.Apci == tp1.GroupValueResponse
	switch {
	case tg.Apci == tp1.GroupValueRead || len(tg.Data) == 0:
	case groupValue && len(tg.Data) == 1:
		// A value of up to 6 bits, in the APCI octet.
		fmt.Fprintf(&b, " $%02x", tg.Data[0])
	case groupValue:
		// A longer value follows the APCI octet.
		fmt.Fprintf(&b, " % x", tg.Data[1:])
	default:
		fmt.Fprintf(&b, " % x", tg.Data)
	}
	if v := d.value(&tg); v != "" {
		b.WriteString(" (" + v + ")")
	}
	fmt.Fprintf(&b, ", %v priority, routing %d", tg.Priority, tg.Routing)
	if tg.Transport == tp1.NumberedDataPacket {
		fmt.Fprintf(&b, ", sequence %d", tg.Sequence)
	}
	if tg.Repeat {
		b.WriteString(", repeated")
	}
	return b.String()
}

// value describes the datapoint type of the group a telegram is sent to,
// followed by the value of a group value write or response:
//   - a bound group with its binding, e.g. "A, DPT 1.001: On";
//   - a group on the main/middle group of bindings of one type with that
//     type, e.g. "DPT 9.001: 21.00 °C". A value that does not unpack shows
//     that the group is of another type; nothing is returned then.
//
// It returns "" for any other group, for 0/0/0 and for an individual
// address.
func (d *decoder) value(tg *tp1.Telegram) string {
	if tg.AddrType != tp1.AddrGroup || tg.Dest == 0 {
		return ""
	}
	if !d.valid {
		d.build()
	}
	name, kind := "", dptNone
	for _, b := range d.bound[:d.n] {
		if b.group == tg.Dest {
			name, kind = b.name, b.kind
			break
		}
	}
	if kind == dptNone {
		kind = d.lines[tg.Dest>>8]
	}
	id, v := kind.id()
	if v == nil {
		return ""
	}
	s := "DPT " + id
	if name != "" {
		s = name + ", " + s
	}
	if tg.Apci != tp1.GroupValueWrite && tg.Apci != tp1.GroupValueResponse {
		return s
	}
	if v.Unpack(tg.Data) != nil {
		if name == "" {
			return ""
		}
		return s
	}
	return s + ": " + v.String()
}
