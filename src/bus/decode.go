package bus

import (
	"knixx/thunder"
	"knixx/tp1"

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

// maxF16 is the upper bound of the DPT 9 types, in hundredths.
const maxF16 = 670760_00

// id returns the DPT ID of k. A DPT 9 type also gets the unit of its values
// and their lower bound in hundredths.
func (k dptKind) id() (id, unit string, min int32) {
	switch k {
	case dpt1001:
		return "1.001", "", 0
	case dpt1018:
		return "1.018", "", 0
	case dpt9001:
		return "9.001", " °C", -273_00
	case dpt9004:
		return "9.004", " lux", 0
	case dpt9007:
		return "9.007", " %", 0
	}
	return "", "", 0
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

	// tg, with payload for its data, is where a frame is unpacked, so that
	// rendering it does not allocate.
	tg      tp1.Telegram
	payload [tp1.MaxPayload]byte
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

// appendFrame appends the rendering of a frame from the bus to dst: the
// source and destination, the service with its data, the datapoint type and
// value of a group, and the priority and routing counter. It does not
// allocate while dst has room.
func (d *decoder) appendFrame(dst, frame []byte) []byte {
	dst = append(dst, "L_Data.ind: "...)
	tg := &d.tg
	tg.Data = d.payload[:0]
	if err := tg.Unpack(frame); err != nil {
		return append(dst, err.Error()...)
	}

	dst = appendIndividualAddr(dst, tg.Src)
	dst = append(dst, " > "...)
	if tg.AddrType == tp1.AddrGroup {
		dst = appendGroupAddr(dst, tg.Dest)
	} else {
		dst = appendIndividualAddr(dst, tg.Dest)
	}
	dst = append(dst, ' ')
	dst = append(dst, tg.Apci.String()...)
	groupValue := tg.Apci == tp1.GroupValueWrite || tg.Apci == tp1.GroupValueResponse
	switch {
	case tg.Apci == tp1.GroupValueRead || len(tg.Data) == 0:
	case groupValue && len(tg.Data) == 1:
		// A value of up to 6 bits, in the APCI octet.
		dst = append(dst, " $"...)
		dst = appendHex2(dst, tg.Data[0])
	case groupValue:
		// A longer value follows the APCI octet.
		dst = appendHexBytes(append(dst, ' '), tg.Data[1:])
	default:
		dst = appendHexBytes(append(dst, ' '), tg.Data)
	}
	dst = d.appendValue(dst, tg)
	dst = append(dst, ", "...)
	dst = append(dst, tg.Priority.String()...)
	dst = append(dst, " priority, routing "...)
	dst = appendDec(dst, uint64(tg.Routing))
	if tg.Transport == tp1.NumberedDataPacket {
		dst = append(dst, ", sequence "...)
		dst = appendDec(dst, uint64(tg.Sequence))
	}
	if tg.Repeat {
		dst = append(dst, ", repeated"...)
	}
	return dst
}

// appendValue appends, in parentheses, the datapoint type of the group a
// telegram is sent to, followed by the value of a group value write or
// response:
//   - a bound group with its binding, e.g. "A, DPT 1.001: On";
//   - a group on the main/middle group of bindings of one type with that
//     type, e.g. "DPT 9.001: 21.00 °C". A value that does not unpack shows
//     that the group is of another type; nothing is appended then.
//
// It appends nothing for any other group, for 0/0/0 and for an individual
// address.
func (d *decoder) appendValue(dst []byte, tg *tp1.Telegram) []byte {
	if tg.AddrType != tp1.AddrGroup || tg.Dest == 0 {
		return dst
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
	id, unit, min := kind.id()
	if id == "" {
		return dst
	}
	start := len(dst)
	dst = append(dst, " ("...)
	if name != "" {
		dst = append(dst, name...)
		dst = append(dst, ", "...)
	}
	dst = append(dst, "DPT "...)
	dst = append(dst, id...)
	if tg.Apci != tp1.GroupValueWrite && tg.Apci != tp1.GroupValueResponse {
		return append(dst, ')')
	}
	// The value, if it has the length and range of the type.
	var text string
	h, ok := int32(0), false
	switch kind {
	case dpt1001, dpt1018:
		ok = len(tg.Data) == 1
		on := ok && tg.Data[0]&1 == 1
		if kind == dpt1001 {
			text = dpt.DPT_1001(on).String()
		} else {
			text = dpt.DPT_1018(on).String()
		}
	default:
		h, ok = f16(tg.Data)
		ok = ok && h >= min && h <= maxF16
	}
	if !ok {
		if name == "" {
			return dst[:start]
		}
		return append(dst, ')')
	}
	dst = append(dst, ": "...)
	if text != "" {
		dst = append(dst, text...)
	} else {
		dst = appendHundredths(dst, h)
		dst = append(dst, unit...)
	}
	return append(dst, ')')
}
