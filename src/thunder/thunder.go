// Package thunder models the knixx thunder board: its individual address, its
// switches, its sensors and the occupancy they detect, and builds the
// telegrams they send.
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

// Occupancy configures the occupancy derived from the motion indicator of
// the time-of-flight sensor.
type Occupancy struct {
	// Group receives DPT 1.018 occupancy telegrams. 0 leaves it unbound.
	Group uint16
	// Timeout is the time in seconds without motion after which occupancy
	// is turned off.
	Timeout uint16
	// Threshold is the motion intensity above which a zone detects motion.
	Threshold uint16
	// Frequency is the ranging frequency in Hz of the sensor's autonomous
	// mode, at resolution 4x4.
	Frequency uint16
	// Integration is the integration time in ms of each frame.
	Integration uint16
	// MinDistance and MaxDistance bound in mm the distance window in which
	// the sensor detects motion, see DistanceValid.
	MinDistance, MaxDistance uint16
}

// Occupancy defaults and limits.
const (
	DefaultOccupancyTimeout = 60
	MinOccupancyTimeout     = 1
	MaxOccupancyTimeout     = 65535
	DefaultMotionThreshold  = 44
	MinMotionThreshold      = 1
	// MaxMotionThreshold is the largest threshold the sensor can store.
	MaxMotionThreshold      = 32767
	DefaultRangingFrequency = 1
	MinRangingFrequency     = 1
	// MaxRangingFrequency is the limit of resolution 4x4.
	MaxRangingFrequency    = 60
	DefaultIntegrationTime = 5
	MinIntegrationTime     = 2
	MaxIntegrationTime     = 1000
	DefaultMinDistance     = 400
	DefaultMaxDistance     = 1500
	// MinMotionDistance and MaxMotionDistance bound each distance;
	// MaxMotionSpan bounds the width of the window.
	MinMotionDistance = 400
	MaxMotionDistance = 4000
	MaxMotionSpan     = 1500
)

// DistanceValid reports whether the sensor accepts the distance window: on
// top of the limits of each distance, MaxDistance must exceed MinDistance by
// at most MaxMotionSpan. It mirrors vl53l7cx.ValidMotionDistance.
func (o Occupancy) DistanceValid() bool {
	return o.MinDistance >= MinMotionDistance && o.MaxDistance <= MaxMotionDistance &&
		o.MaxDistance > o.MinDistance && o.MaxDistance-o.MinDistance <= MaxMotionSpan
}

// PeriodStretched reports whether the integration time plus the sensor's
// 1 ms overhead does not fit the ranging period, in which case the sensor
// lengthens the period to fit and ranges below Frequency.
func (o Occupancy) PeriodStretched() bool {
	// (Integration + 1) ms >= 1000/Frequency ms, without the rounding of
	// the division.
	return (uint32(o.Integration)+1)*uint32(o.Frequency) >= 1000
}

type Device struct {
	Individual uint16
	Switches   [NumSwitches]Switch
	Sensor     Sensor
	Occupancy  Occupancy
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
