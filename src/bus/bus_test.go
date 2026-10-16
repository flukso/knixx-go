package bus

import (
	"bytes"
	"knixx/light"
	"knixx/ncn"
	"strings"
	"testing"
	"time"
)

// fakeDevice records what is written and hands out rx on reads. With
// confirm set it answers every frame with U_FrameState.ind and a positive
// L_Data.con, and flags a frame written before the previous one's
// confirmation was read.
type fakeDevice struct {
	bytes.Buffer
	rx      []byte
	confirm bool
	overlap bool
}

func (f *fakeDevice) Read(p []byte) (int, error) {
	n := copy(p, f.rx)
	f.rx = f.rx[n:]
	return n, nil
}

func (f *fakeDevice) Write(p []byte) (int, error) {
	if f.confirm && len(p) > 0 && p[0] == ncn.U_L_DataStartReq {
		if len(f.rx) > 0 {
			f.overlap = true
		}
		f.rx = append(f.rx, 0x13, 0x8b)
	}
	return f.Buffer.Write(p)
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
	case <-time.After(5 * time.Second):
		t.Fatal("transmit did not finish")
	}
	return captured.String()
}

func testDevice() *light.Device {
	return &light.Device{
		Individual: 0x7801,
		Switches: [light.NumSwitches]light.Switch{
			light.NewSwitch("A", 0x0002),
			light.NewSwitch("B", 0x0003, 0x0004, 0x0005),
		},
	}
}

// frames returns the requests for setting each group to on, in order.
func frames(t *testing.T, dev *light.Device, on bool, groups ...uint16) []byte {
	t.Helper()
	var want bytes.Buffer
	for _, g := range groups {
		tg := dev.Telegram(g, on)
		buf, err := tg.Pack()
		if err != nil {
			t.Fatal(err)
		}
		ncn.TxData(&want, buf)
	}
	return want.Bytes()
}

func TestTransmit(t *testing.T) {
	dev := testDevice()
	queued := []Command{
		{Kind: CmdSwitch, Switch: 0, On: true},
		{Kind: CmdSwitch, Switch: 1, On: true},
		{Kind: CmdSwitch, Switch: 0, On: false},
	}
	want := append(frames(t, dev, true, 0x0002, 0x0003, 0x0004, 0x0005), frames(t, dev, false, 0x0002)...)

	defer Sniff.Store(false)
	for _, on := range []bool{true, false} {
		Sniff.Store(on)
		d := fakeDevice{confirm: true}
		got := run(t, &d, dev, queued)
		if !bytes.Equal(d.Bytes(), want) {
			t.Errorf("sniff=%v: got % x, want % x", on, d.Bytes(), want)
		}
		if d.overlap {
			t.Errorf("sniff=%v: a frame was sent before the previous L_Data.con", on)
		}
		if strings.Contains(got, "no L_Data.con") {
			t.Errorf("sniff=%v: unexpected timeout: %q", on, got)
		}
	}
}

func TestNoConfirmation(t *testing.T) {
	Sniff.Store(false)
	prev := conTimeout
	conTimeout = 30 * time.Millisecond
	defer func() { conTimeout = prev }()

	dev := testDevice()
	var d fakeDevice
	got := run(t, &d, dev, []Command{{Kind: CmdSwitch, Switch: 1, On: true}})
	if want := frames(t, dev, true, 0x0003, 0x0004, 0x0005); !bytes.Equal(d.Bytes(), want) {
		t.Errorf("got % x, want % x", d.Bytes(), want)
	}
	for _, want := range []string{"no L_Data.con for 0/0/3", "no L_Data.con for 0/0/4", "no L_Data.con for 0/0/5"} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q lacks %q", got, want)
		}
	}
}

func TestNegativeConfirmation(t *testing.T) {
	Sniff.Store(false)
	dev := testDevice()
	d := fakeDevice{rx: []byte{0x13, 0x0b}}
	got := run(t, &d, dev, []Command{{Kind: CmdSwitch, Switch: 0, On: true}})
	if !strings.Contains(got, "L_Data.con: negative for 0/0/2") {
		t.Errorf("output %q", got)
	}
}

func TestStatAfterSwitch(t *testing.T) {
	Sniff.Store(false)
	dev := testDevice()
	d := fakeDevice{confirm: true}
	run(t, &d, dev, []Command{
		{Kind: CmdSwitch, Switch: 1, On: true},
		{Kind: CmdStat},
	})
	want := append(frames(t, dev, true, 0x0003, 0x0004, 0x0005), ncn.U_StateReq, ncn.U_SystemStatReq)
	if !bytes.Equal(d.Bytes(), want) {
		t.Errorf("got % x, want % x", d.Bytes(), want)
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

func TestConfig(t *testing.T) {
	Sniff.Store(false)
	dev := testDevice()
	moved := *dev
	moved.Switches[0] = light.NewSwitch("A", 0x0909)

	d := fakeDevice{confirm: true}
	run(t, &d, dev, []Command{
		{Kind: CmdSwitch, Switch: 0, On: true},
		{Kind: CmdConfig, Dev: moved},
		{Kind: CmdSwitch, Switch: 0, On: true},
	})
	want := append(frames(t, dev, true, 0x0002), frames(t, &moved, true, 0x0909)...)
	if !bytes.Equal(d.Bytes(), want) {
		t.Errorf("got % x, want % x", d.Bytes(), want)
	}
	if dev.Switches[0].Groups[0] != 0x0002 {
		t.Errorf("Run modified the caller's device: %+v", dev)
	}
}
