package bus

import (
	"bytes"
	"knixx/light"
	"knixx/ncn"
	"strings"
	"testing"
	"time"
)

// fakeDevice records what is written and hands out rx on the first read.
type fakeDevice struct {
	bytes.Buffer
	rx []byte
}

func (f *fakeDevice) Read(p []byte) (int, error) {
	n := copy(p, f.rx)
	f.rx = f.rx[n:]
	return n, nil
}

func run(t *testing.T, d *fakeDevice, dev *light.Device, queued []Command) string {
	t.Helper()
	var captured bytes.Buffer
	prev := Out
	Out = &captured
	defer func() { Out = prev }()

	cmds := make(chan Command, QueueLen)
	done := make(chan struct{})
	for _, cmd := range queued {
		cmds <- cmd
	}
	close(cmds)
	go Run(d, dev, cmds, done)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("transmit did not finish")
	}
	return captured.String()
}

func testDevice() *light.Device {
	return &light.Device{
		Individual: 0x7801,
		Switches: [light.NumSwitches]light.Switch{
			{Name: "A", Group: 0x0002},
			{Name: "B", Group: 0x0003},
		},
	}
}

func TestTransmit(t *testing.T) {
	dev := testDevice()
	queued := []Command{
		{Kind: CmdSwitch, Sw: dev.Switch("A"), On: true},
		{Kind: CmdSwitch, Sw: dev.Switch("B"), On: true},
		{Kind: CmdSwitch, Sw: dev.Switch("A"), On: false},
	}

	var want bytes.Buffer
	for _, cmd := range queued {
		tg := dev.Telegram(cmd.Sw, cmd.On)
		buf, err := tg.Pack()
		if err != nil {
			t.Fatal(err)
		}
		ncn.TxData(&want, buf)
	}

	defer Sniff.Store(false)
	for _, on := range []bool{true, false} {
		Sniff.Store(on)
		var d fakeDevice
		run(t, &d, dev, queued)
		if !bytes.Equal(d.Bytes(), want.Bytes()) {
			t.Errorf("sniff=%v: got % x, want % x", on, d.Bytes(), want.Bytes())
		}
	}
}

func TestStat(t *testing.T) {
	Sniff.Store(false)
	d := fakeDevice{rx: []byte{0x07, 0x4b, 0xfb}}
	got := run(t, &d, testDevice(), []Command{{Kind: CmdStat}})

	if want := []byte{ncn.U_StateReq, ncn.U_SystemStatReq}; !bytes.Equal(d.Bytes(), want) {
		t.Errorf("request % x, want % x", d.Bytes(), want)
	}
	for _, want := range []string{
		"U_State.ind: no errors (0x07)",
		"U_SystemStat.ind: V20V ok, VDD2 ok, VBUS ok, VFILT ok, XTAL ok, no thermal warning, mode Normal (0xfb)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q lacks %q", got, want)
		}
	}
}

func TestStatNoReply(t *testing.T) {
	Sniff.Store(false)
	var d fakeDevice
	got := run(t, &d, testDevice(), []Command{{Kind: CmdStat}})
	for _, want := range []string{"no U_State.ind reply", "no U_SystemStat.ind reply"} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q lacks %q", got, want)
		}
	}
}
