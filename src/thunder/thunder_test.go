package thunder

import (
	"bytes"
	"testing"

	"github.com/vapourismo/knx-go/knx/cemi"
	"github.com/vapourismo/knx-go/knx/dpt"
)

func testDevice() *Device {
	return &Device{
		Individual: uint16(cemi.NewIndividualAddr3(7, 8, 1)),
		Switches: [NumSwitches]Switch{
			NewSwitch("A", uint16(cemi.NewGroupAddr3(0, 0, 2))),
			NewSwitch("B", uint16(cemi.NewGroupAddr3(0, 0, 3))),
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
		tg := d.Telegram(d.Switch(tt.name).Groups[0], tt.on)
		got, err := tg.Pack()
		if err != nil {
			t.Fatalf("%s on=%v: %v", tt.name, tt.on, err)
		}
		if !bytes.Equal(got, tt.want) {
			t.Errorf("%s on=%v: got % x, want % x", tt.name, tt.on, got, tt.want)
		}
	}
}

func TestGroupWriteDPT9(t *testing.T) {
	d := testDevice()
	tests := []struct {
		name  string
		group uint16
		data  []byte
		want  []byte
	}{
		{"21.5 °C to 1/0/1", uint16(cemi.NewGroupAddr3(1, 0, 1)), dpt.DPT_9001(21.5).Pack(),
			[]byte{0xbc, 0x78, 0x01, 0x08, 0x01, 0x83, 0x00, 0x80, 0x0c, 0x33, 0x0f}},
		{"48.2 % to 1/0/2", uint16(cemi.NewGroupAddr3(1, 0, 2)), dpt.DPT_9007(48.2).Pack(),
			[]byte{0xbc, 0x78, 0x01, 0x08, 0x02, 0x83, 0x00, 0x80, 0x14, 0xb5, 0x92}},
	}
	for _, tt := range tests {
		tg := d.GroupWrite(tt.group, tt.data)
		got, err := tg.Pack()
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if !bytes.Equal(got, tt.want) {
			t.Errorf("%s: got % x, want % x", tt.name, got, tt.want)
		}
	}
}

func TestNewSwitch(t *testing.T) {
	sw := NewSwitch("A", 1, 2, 3)
	if got := sw.GroupList(); len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Errorf("GroupList = %v", got)
	}
	many := make([]uint16, MaxGroups+2)
	if sw := NewSwitch("B", many...); sw.NGroups != MaxGroups {
		t.Errorf("NGroups = %d, want %d", sw.NGroups, MaxGroups)
	}
}
