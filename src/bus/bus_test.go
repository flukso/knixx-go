package bus

import (
	"bytes"
	"knixx/ncn"
	"knixx/thunder"
	"strings"
	"sync/atomic"
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

func run(t *testing.T, d *fakeDevice, dev *thunder.Device, queued []Command) string {
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

func testDevice() *thunder.Device {
	return &thunder.Device{
		Individual: 0x7801,
		Switches: [thunder.NumSwitches]thunder.Switch{
			thunder.NewSwitch("A", 0x0002),
			thunder.NewSwitch("B", 0x0003, 0x0004, 0x0005),
		},
	}
}

// frames returns the requests for setting each group to on, in order.
func frames(t *testing.T, dev *thunder.Device, on bool, groups ...uint16) []byte {
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
	errs := countErrors(t)
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
		if n := errs.Load(); n != 0 {
			t.Errorf("sniff=%v: OnError called %d times", on, n)
		}
	}
}

// countErrors counts the calls of OnError until the test ends.
func countErrors(t *testing.T) *atomic.Int32 {
	var n atomic.Int32
	OnError = func() { n.Add(1) }
	t.Cleanup(func() { OnError = nil })
	return &n
}

func TestNoConfirmation(t *testing.T) {
	Sniff.Store(false)
	errs := countErrors(t)
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
	if n := errs.Load(); n != 3 {
		t.Errorf("OnError called %d times, want 3", n)
	}
}

func TestNegativeConfirmation(t *testing.T) {
	defer Sniff.Store(false)
	errs := countErrors(t)
	dev := testDevice()
	for _, sniff := range []bool{false, true} {
		Sniff.Store(sniff)
		errs.Store(0)
		d := fakeDevice{rx: []byte{0x13, 0x0b}}
		got := run(t, &d, dev, []Command{{Kind: CmdSwitch, Switch: 0, On: true}})
		if !strings.Contains(got, "negative") {
			t.Errorf("sniff=%v: output %q", sniff, got)
		}
		if n := errs.Load(); n != 1 {
			t.Errorf("sniff=%v: OnError called %d times, want 1", sniff, n)
		}
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
	moved.Switches[0] = thunder.NewSwitch("A", 0x0909)

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

// writes returns the requests for the given group value writes, in order.
func writes(t *testing.T, dev *thunder.Device, ws ...write) []byte {
	t.Helper()
	var want bytes.Buffer
	for _, w := range ws {
		tg := dev.GroupWrite(w.group, w.data[:w.n])
		buf, err := tg.Pack()
		if err != nil {
			t.Fatal(err)
		}
		ncn.TxData(&want, buf)
	}
	return want.Bytes()
}

func TestSensor(t *testing.T) {
	Sniff.Store(false)
	dev := testDevice()
	dev.Sensor = thunder.Sensor{Temperature: 0x0101, Humidity: 0x0102}
	reading := Command{Kind: CmdSensor, Temperature: 21500, Humidity: 4820}

	d := fakeDevice{confirm: true}
	run(t, &d, dev, []Command{reading, {Kind: CmdSwitch, Switch: 0, On: true}})
	want := append(writes(t, dev,
		newWrite(0x0101, []byte{0x00, 0x0c, 0x33}), // 21.5 °C
		newWrite(0x0102, []byte{0x00, 0x14, 0xb5}), // 48.2 %
	), frames(t, dev, true, 0x0002)...)
	if !bytes.Equal(d.Bytes(), want) {
		t.Errorf("got % x, want % x", d.Bytes(), want)
	}
	if d.overlap {
		t.Error("a frame was sent before the previous L_Data.con")
	}

	// An unbound value is not sent.
	dev.Sensor.Temperature = 0
	d = fakeDevice{confirm: true}
	run(t, &d, dev, []Command{reading})
	if want := writes(t, dev, newWrite(0x0102, []byte{0x00, 0x14, 0xb5})); !bytes.Equal(d.Bytes(), want) {
		t.Errorf("humidity only: got % x, want % x", d.Bytes(), want)
	}

	dev.Sensor.Humidity = 0
	d = fakeDevice{confirm: true}
	run(t, &d, dev, []Command{reading})
	if d.Len() != 0 {
		t.Errorf("unbound sensor sent % x", d.Bytes())
	}
}

func TestOccupancyState(t *testing.T) {
	var o occupancy
	t0 := time.Unix(1000, 0)
	const timeout = 60 * time.Second
	steps := []struct {
		motion bool
		at     time.Duration
		want   bool // turns on for motion, off otherwise
	}{
		{false, 0, false},                // nothing to expire
		{true, 0, true},                  // first motion turns on
		{true, 10 * time.Second, false},  // already on, restarts the timer
		{false, 69 * time.Second, false}, // 59 s after the last motion
		{false, 70 * time.Second, true},  // 60 s after the last motion
		{false, 80 * time.Second, false}, // already off
		{true, 90 * time.Second, true},   // on again
	}
	for i, s := range steps {
		var got bool
		if s.motion {
			got = o.motion(t0.Add(s.at))
		} else {
			got = o.expire(t0.Add(s.at), timeout)
		}
		if got != s.want {
			t.Errorf("step %d: got %v, want %v", i, got, s.want)
		}
	}
}

// runFor runs Run with the queued commands for at least d before closing
// its command channel.
func runFor(t *testing.T, d *fakeDevice, dev *thunder.Device, queued []Command, wait time.Duration) {
	t.Helper()
	prev := Out
	Out = &bytes.Buffer{}
	defer func() { Out = prev }()

	cmds := make(chan Command, QueueLen)
	done := make(chan struct{})
	for _, cmd := range queued {
		cmds <- cmd
	}
	go Run(d, dev, cmds, done)
	time.Sleep(wait)
	close(cmds)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not finish")
	}
}

func TestOccupancy(t *testing.T) {
	Sniff.Store(false)
	prevUnit := occupancyUnit
	occupancyUnit = 10 * time.Millisecond
	defer func() { occupancyUnit = prevUnit }()

	dev := testDevice()
	dev.Occupancy = thunder.Occupancy{Group: 0x0201, Timeout: 10, Threshold: 44}
	on := writes(t, dev, newWrite(0x0201, []byte{0x01}))
	off := writes(t, dev, newWrite(0x0201, []byte{0x00}))
	motion := Command{Kind: CmdMotion}

	// Motion turns occupancy on once; it is turned off after 100 ms
	// without motion.
	d := fakeDevice{confirm: true}
	runFor(t, &d, dev, []Command{motion, motion, motion}, 50*time.Millisecond)
	if want := on; !bytes.Equal(d.Bytes(), want) {
		t.Errorf("before the timeout: got % x, want % x", d.Bytes(), want)
	}
	if !Occupied.Load() {
		t.Error("not occupied after motion")
	}

	d = fakeDevice{confirm: true}
	runFor(t, &d, dev, []Command{motion, {Kind: CmdSwitch, Switch: 0, On: true}}, 300*time.Millisecond)
	want := append(append(on, frames(t, dev, true, 0x0002)...), off...)
	if !bytes.Equal(d.Bytes(), want) {
		t.Errorf("after the timeout: got % x, want % x", d.Bytes(), want)
	}
	if d.overlap {
		t.Error("a frame was sent before the previous L_Data.con")
	}
	if Occupied.Load() {
		t.Error("still occupied after the timeout")
	}

	// Without a group, motion is ignored.
	dev.Occupancy.Group = 0
	d = fakeDevice{confirm: true}
	runFor(t, &d, dev, []Command{motion}, 50*time.Millisecond)
	if d.Len() != 0 {
		t.Errorf("unbound occupancy sent % x", d.Bytes())
	}
}

func TestLight(t *testing.T) {
	Sniff.Store(false)
	dev := testDevice()
	dev.Sensor = thunder.Sensor{Illuminance: 0x0103}
	reading := Command{Kind: CmdLight, Illuminance: 4321}

	d := fakeDevice{confirm: true}
	run(t, &d, dev, []Command{reading})
	want := writes(t, dev, newWrite(0x0103, []byte{0x00, 0x2d, 0x46})) // 432.1 lx
	if !bytes.Equal(d.Bytes(), want) {
		t.Errorf("got % x, want % x", d.Bytes(), want)
	}

	// The temperature and humidity groups do not receive it.
	dev.Sensor = thunder.Sensor{Temperature: 0x0101, Humidity: 0x0102}
	d = fakeDevice{confirm: true}
	run(t, &d, dev, []Command{reading})
	if d.Len() != 0 {
		t.Errorf("unbound illuminance sent % x", d.Bytes())
	}
}
