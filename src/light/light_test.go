package light

import (
	"bytes"
	"testing"

	"github.com/vapourismo/knx-go/knx/cemi"
)

func testDevice() *Device {
	return &Device{
		Individual: uint16(cemi.NewIndividualAddr3(7, 8, 1)),
		Switches: [NumSwitches]Switch{
			{Name: "A", Group: uint16(cemi.NewGroupAddr3(0, 0, 2))},
			{Name: "B", Group: uint16(cemi.NewGroupAddr3(0, 0, 3))},
		},
	}
}

func TestSwitchLookup(t *testing.T) {
	d := testDevice()
	if sw := d.Switch("A"); sw != &d.Switches[0] {
		t.Errorf("Switch(A) = %v", sw)
	}
	if sw := d.Switch("b"); sw != &d.Switches[1] {
		t.Errorf("Switch(b) = %v", sw)
	}
	if sw := d.Switch("C"); sw != nil {
		t.Errorf("Switch(C) = %v, want nil", sw)
	}
}

func TestTelegram(t *testing.T) {
	d := testDevice()
	tests := []struct {
		name string
		on   bool
		want []byte
	}{
		{"A", false, []byte{0xbc, 0x78, 0x01, 0x00, 0x02, 0x81, 0x00, 0x80, 0x39}},
		{"A", true, []byte{0xbc, 0x78, 0x01, 0x00, 0x02, 0x81, 0x00, 0x81, 0x38}},
		{"B", false, []byte{0xbc, 0x78, 0x01, 0x00, 0x03, 0x81, 0x00, 0x80, 0x38}},
		{"B", true, []byte{0xbc, 0x78, 0x01, 0x00, 0x03, 0x81, 0x00, 0x81, 0x39}},
	}
	for _, tt := range tests {
		tg := d.Telegram(d.Switch(tt.name), tt.on)
		got, err := tg.Pack()
		if err != nil {
			t.Fatalf("%s on=%v: %v", tt.name, tt.on, err)
		}
		if !bytes.Equal(got, tt.want) {
			t.Errorf("%s on=%v: got % x, want % x", tt.name, tt.on, got, tt.want)
		}
	}
}
