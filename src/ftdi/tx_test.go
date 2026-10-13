package main

import (
	"bytes"
	"knixx/light"
	"knixx/ncn"
	"testing"
	"time"
)

type fakeDevice struct {
	bytes.Buffer
}

func (f *fakeDevice) Read(p []byte) (int, error) { return 0, nil }

func TestTransmit(t *testing.T) {
	dev := &light.Device{
		Individual: 0x7801,
		Switches: [light.NumSwitches]light.Switch{
			{Name: "A", Group: 0x0002},
			{Name: "B", Group: 0x0003},
		},
	}
	queued := []command{
		{dev.Switch("A"), true},
		{dev.Switch("B"), true},
		{dev.Switch("A"), false},
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
		cmds := make(chan command, txQueueLen)
		done := make(chan struct{})
		for _, cmd := range queued {
			cmds <- cmd
		}
		close(cmds)
		go transmit(&d, dev, cmds, done)

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("transmit did not finish")
		}
		if !bytes.Equal(d.Bytes(), want.Bytes()) {
			t.Errorf("sniff=%v: got % x, want % x", on, d.Bytes(), want.Bytes())
		}
	}
}
