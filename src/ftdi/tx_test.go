package main

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

func run(t *testing.T, d *fakeDevice, dev *light.Device, queued []command) string {
	t.Helper()
	var captured bytes.Buffer
	prev := out
	out = &captured
	defer func() { out = prev }()

	cmds := make(chan command, txQueueLen)
	done := make(chan struct{})
	for _, cmd := range queued {
		cmds <- cmd
	}
	close(cmds)
	go transmit(d, dev, cmds, done)

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
	queued := []command{
		{kind: cmdSwitch, sw: dev.Switch("A"), on: true},
		{kind: cmdSwitch, sw: dev.Switch("B"), on: true},
		{kind: cmdSwitch, sw: dev.Switch("A"), on: false},
	}

	var want bytes.Buffer
	for _, cmd := range queued {
		tg := dev.Telegram(cmd.sw, cmd.on)
		buf, err := tg.Pack()
		if err != nil {
			t.Fatal(err)
		}
		ncn.TxData(&want, buf)
	}

	defer sniff.Store(false)
	for _, on := range []bool{true, false} {
		sniff.Store(on)
		var d fakeDevice
		run(t, &d, dev, queued)
		if !bytes.Equal(d.Bytes(), want.Bytes()) {
			t.Errorf("sniff=%v: got % x, want % x", on, d.Bytes(), want.Bytes())
		}
	}
}

func TestStat(t *testing.T) {
	sniff.Store(false)
	d := fakeDevice{rx: []byte{0x07, 0x4b, 0xfb}}
	got := run(t, &d, testDevice(), []command{{kind: cmdStat}})

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
	sniff.Store(false)
	var d fakeDevice
	got := run(t, &d, testDevice(), []command{{kind: cmdStat}})
	for _, want := range []string{"no U_State.ind reply", "no U_SystemStat.ind reply"} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q lacks %q", got, want)
		}
	}
}
