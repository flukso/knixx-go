package bus

import (
	"knixx/thunder"
	"knixx/tp1"
	"strings"
	"testing"

	"github.com/vapourismo/knx-go/knx/dpt"
)

// ga returns the group address main/middle/sub.
func ga(main, middle, sub uint16) uint16 {
	return main<<11 | middle<<8 | sub
}

// decodeDevice binds a group to every decoded datapoint type, on the
// main/middle groups of the default configuration.
func decodeDevice() *thunder.Device {
	dev := testDevice()
	dev.Sensor = thunder.Sensor{Temperature: ga(2, 0, 1), Humidity: ga(3, 0, 1), Illuminance: ga(4, 0, 1)}
	dev.Occupancy.Group = ga(5, 0, 1)
	return dev
}

// describe renders frame with a decoder for dev.
func describe(dev *thunder.Device, frame []byte) string {
	d := decoder{dev: dev}
	return d.describe(frame)
}

// frameOf packs a telegram from 1.1.5 to dest with service apci and data.
func frameOf(t *testing.T, dest uint16, apci tp1.Apci, data []byte, edit func(*tp1.Telegram)) []byte {
	t.Helper()
	tg := tp1.Telegram{
		Ctrl: tp1.Ctrl{Priority: tp1.PriorityLow},
		Src:  0x1105,
		Dest: dest,
		Npci: tp1.Npci{AddrType: tp1.AddrGroup, Routing: 6},
		Apci: apci,
		Data: data,
	}
	if edit != nil {
		edit(&tg)
	}
	buf, err := tg.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return buf
}

func TestDescribeFrame(t *testing.T) {
	dev := decodeDevice()
	tests := []struct {
		name  string
		frame []byte
		want  string
	}{
		{"switch on",
			frameOf(t, 0x0002, tp1.GroupValueWrite, dpt.DPT_1001(true).Pack(), nil),
			"L_Data.ind: 1.1.5 > 0/0/2 GroupValueWrite $01 (A, DPT 1.001: On), low priority, routing 6"},
		{"second group of a switch, off",
			frameOf(t, 0x0004, tp1.GroupValueWrite, dpt.DPT_1001(false).Pack(), nil),
			"L_Data.ind: 1.1.5 > 0/0/4 GroupValueWrite $00 (B, DPT 1.001: Off), low priority, routing 6"},
		{"temperature response",
			frameOf(t, ga(2, 0, 1), tp1.GroupValueResponse, dpt.DPT_9001(21).Pack(), nil),
			"L_Data.ind: 1.1.5 > 2/0/1 GroupValueResponse 0c 1a (temperature, DPT 9.001: 21.00 °C), low priority, routing 6"},
		{"humidity",
			frameOf(t, ga(3, 0, 1), tp1.GroupValueWrite, dpt.DPT_9007(40).Pack(), nil),
			"(humidity, DPT 9.007: 40.00 %)"},
		{"illuminance",
			frameOf(t, ga(4, 0, 1), tp1.GroupValueWrite, dpt.DPT_9004(320).Pack(), nil),
			"(illuminance, DPT 9.004: 320.00 lux)"},
		{"occupancy",
			frameOf(t, ga(5, 0, 1), tp1.GroupValueWrite, dpt.DPT_1018(true).Pack(), nil),
			"GroupValueWrite $01 (occupancy, DPT 1.018: occupied)"},
		{"group read",
			frameOf(t, 0x0002, tp1.GroupValueRead, []byte{0x00}, nil),
			"L_Data.ind: 1.1.5 > 0/0/2 GroupValueRead (A, DPT 1.001), low priority, routing 6"},
		{"unbound group",
			frameOf(t, 0x0909, tp1.GroupValueWrite, []byte{0x00, 0x12, 0x34}, nil),
			"L_Data.ind: 1.1.5 > 1/1/9 GroupValueWrite 12 34, low priority, routing 6"},
		{"individual, numbered, repeated, system priority",
			frameOf(t, 0x7801, tp1.MemoryRead, []byte{0x03, 0x00, 0x60}, func(tg *tp1.Telegram) {
				tg.AddrType, tg.Transport, tg.Sequence = tp1.AddrIndividual, tp1.NumberedDataPacket, 3
				tg.Repeat, tg.Priority = true, tp1.PrioritySystem
			}),
			"L_Data.ind: 1.1.5 > 7.8.1 MemoryRead 03 00 60, system priority, routing 6, sequence 3, repeated"},
		{"undecodable value",
			frameOf(t, ga(2, 0, 1), tp1.GroupValueWrite, []byte{0x00, 0x0c}, nil),
			"GroupValueWrite 0c (temperature, DPT 9.001), low priority"},
		{"inferred switch line",
			frameOf(t, ga(0, 0, 7), tp1.GroupValueWrite, dpt.DPT_1001(true).Pack(), nil),
			"L_Data.ind: 1.1.5 > 0/0/7 GroupValueWrite $01 (DPT 1.001: On), low priority"},
		{"inferred temperature line",
			frameOf(t, ga(2, 0, 9), tp1.GroupValueWrite, dpt.DPT_9001(21).Pack(), nil),
			"L_Data.ind: 1.1.5 > 2/0/9 GroupValueWrite 0c 1a (DPT 9.001: 21.00 °C), low priority"},
		{"inferred illuminance line",
			frameOf(t, ga(4, 0, 2), tp1.GroupValueWrite, dpt.DPT_9004(320).Pack(), nil),
			"(DPT 9.004: 320.00 lux)"},
		{"inferred occupancy line",
			frameOf(t, ga(5, 0, 3), tp1.GroupValueWrite, dpt.DPT_1018(false).Pack(), nil),
			"(DPT 1.018: not occupied)"},
		{"inferred read",
			frameOf(t, ga(2, 0, 9), tp1.GroupValueRead, []byte{0x00}, nil),
			"2/0/9 GroupValueRead (DPT 9.001), low priority"},
		{"inferred, value of another type",
			frameOf(t, ga(2, 0, 9), tp1.GroupValueWrite, []byte{0x00, 0x2a}, nil),
			"2/0/9 GroupValueWrite 2a, low priority"},
		{"other middle group",
			frameOf(t, ga(2, 1, 1), tp1.GroupValueWrite, dpt.DPT_9001(21).Pack(), nil),
			"2/1/1 GroupValueWrite 0c 1a, low priority"},
		{"other main group",
			frameOf(t, ga(6, 0, 1), tp1.GroupValueWrite, dpt.DPT_9001(21).Pack(), nil),
			"6/0/1 GroupValueWrite 0c 1a, low priority"},
		{"broadcast",
			frameOf(t, 0, tp1.GroupValueWrite, []byte{0x01}, nil),
			"0/0/0 GroupValueWrite $01, low priority"},
		{"bad checksum",
			[]byte{0xbc, 0x11, 0x05, 0x00, 0x02, 0xe1, 0x00, 0x81, 0x00},
			"L_Data.ind: tp1: checksum mismatch"},
	}
	for _, tt := range tests {
		got := describe(dev, tt.frame)
		if !strings.Contains(got, tt.want) {
			t.Errorf("%s: describe(% x) = %q, want %q", tt.name, tt.frame, got, tt.want)
		}
	}
}

// TestSniffDecode feeds a received frame to Run, which reads it while it
// waits for the stat replies: it is decoded only while sniffing.
func TestSniffDecode(t *testing.T) {
	defer Sniff.Store(false)
	frame := frameOf(t, 0x0003, tp1.GroupValueWrite, dpt.DPT_1001(true).Pack(), nil)
	want := "L_Data.ind: 1.1.5 > 0/0/3 GroupValueWrite $01 (B, DPT 1.001: On)"
	for _, sniff := range []bool{false, true} {
		Sniff.Store(sniff)
		d := fakeDevice{rx: append([]byte(nil), frame...)}
		got := run(t, &d, testDevice(), []Command{{Kind: CmdStat}})
		if strings.Contains(got, want) != sniff {
			t.Errorf("sniff=%v: output %q", sniff, got)
		}
	}
}

func TestDecodeConflictingLine(t *testing.T) {
	dev := decodeDevice()
	dev.Sensor.Humidity = ga(2, 0, 2)
	frame := frameOf(t, ga(2, 0, 3), tp1.GroupValueWrite, dpt.DPT_9001(21).Pack(), nil)
	if got, want := describe(dev, frame), "2/0/3 GroupValueWrite 0c 1a, low priority"; !strings.Contains(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	// The bound groups keep their own type.
	frame = frameOf(t, ga(2, 0, 2), tp1.GroupValueWrite, dpt.DPT_9007(40).Pack(), nil)
	if got, want := describe(dev, frame), "(humidity, DPT 9.007: 40.00 %)"; !strings.Contains(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDecoderReset(t *testing.T) {
	dev := decodeDevice()
	d := decoder{dev: dev}
	frame := frameOf(t, ga(2, 0, 9), tp1.GroupValueWrite, dpt.DPT_9001(21).Pack(), nil)
	if got := d.describe(frame); !strings.Contains(got, "DPT 9.001") {
		t.Fatalf("before the change: %q", got)
	}
	dev.Sensor.Temperature = ga(6, 0, 1)
	if got := d.describe(frame); !strings.Contains(got, "DPT 9.001") {
		t.Errorf("the tables follow dev before reset: %q", got)
	}
	d.reset()
	if got := d.describe(frame); strings.Contains(got, "DPT") {
		t.Errorf("after reset: %q", got)
	}
}

// TestSniffDecodeConfig checks that Run decodes with the configuration of
// the last CmdConfig. The frames arrive with the stat replies.
func TestSniffDecodeConfig(t *testing.T) {
	defer Sniff.Store(false)
	Sniff.Store(true)
	moved := *decodeDevice()
	moved.Sensor.Temperature = ga(6, 0, 1)
	replies := []byte{0x07, 0x4b, 0xfb}
	d := fakeDevice{onStat: [][]byte{
		append(frameOf(t, ga(2, 0, 9), tp1.GroupValueWrite, dpt.DPT_9001(21).Pack(), nil), replies...),
		append(append(frameOf(t, ga(2, 0, 9), tp1.GroupValueWrite, dpt.DPT_9001(21).Pack(), nil),
			frameOf(t, ga(6, 0, 9), tp1.GroupValueWrite, dpt.DPT_9001(22).Pack(), nil)...), replies...),
	}}
	got := run(t, &d, decodeDevice(), []Command{
		{Kind: CmdStat},
		{Kind: CmdConfig, Dev: moved},
		{Kind: CmdStat},
	})
	for _, want := range []string{
		"2/0/9 GroupValueWrite 0c 1a (DPT 9.001: 21.00 °C)",
		"2/0/9 GroupValueWrite 0c 1a, low priority",
		"6/0/9 GroupValueWrite 0c 4c (DPT 9.001: 22.00 °C)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q lacks %q", got, want)
		}
	}
}
