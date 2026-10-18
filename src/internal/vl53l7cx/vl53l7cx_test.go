package vl53l7cx

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"testing"
	"time"
)

// testdata/configure.golden is the log of the C driver, run by
// testdata/harness against the fake sensor that fakeSensor reimplements.
//
//go:generate sh -c "cc -std=c99 -O1 -Itestdata/harness -IVL53L7CX_ULD_driver_2.0.1/VL53L7CX_ULD_API/inc -o /tmp/vl53l7cx-harness testdata/harness/harness.c VL53L7CX_ULD_driver_2.0.1/VL53L7CX_ULD_API/src/vl53l7cx_api.c VL53L7CX_ULD_driver_2.0.1/VL53L7CX_ULD_API/src/vl53l7cx_plugin_motion_indicator.c VL53L7CX_ULD_driver_2.0.1/VL53L7CX_ULD_API/src/vl53l7cx_plugin_detection_thresholds.c -lm && /tmp/vl53l7cx-harness > testdata/configure.golden"

const frameSize = 196

// op is a logged transaction: a write, a read or a wait.
type op struct {
	kind byte // 'W', 'R' or 'D'
	reg  uint16
	n    int
	data []byte // of a write
}

// fakeSensor answers like the fake sensor of testdata/harness/harness.c and
// logs the transactions.
type fakeSensor struct {
	ops  []op
	fail error
}

func put32(p []byte, v uint32) { binary.BigEndian.PutUint32(p, v) }

// frame builds the ranging data frame the fake sensor returns.
func frame(raw []byte) {
	clear(raw)
	raw[0] = 7
	raw[10], raw[11] = 0x12, 0x34
	put32(raw[16:], 0x54B400C0)
	put32(raw[32:], 0x54C00040)
	put32(raw[40:], 0xD85808C0)
	put32(raw[44:], 1111)
	put32(raw[48:], 2222)
	raw[52+3], raw[52+2], raw[52+1] = 1, 3, 16
	for k := uint32(0); k < 32; k++ {
		put32(raw[56+4*k:], (k+1)*10*65535)
	}
	raw[frameSize-2], raw[frameSize-1] = 0x12, 0x34
}

func reply(reg uint16, p []byte) {
	for i := range p {
		p[i] = byte(i*7 + 3)
	}
	switch reg {
	case 0x0000:
		if len(p) == 1 {
			p[0] = 0xF0
		} else if len(p) == frameSize {
			frame(p)
		}
	case 0x0001:
		p[0] = 0x02
	case 0x0006:
		p[0] = 0x01
	case 0x0007:
		p[0] = 0x00
	case 0x0021:
		p[0] = 0x10
	case 0x2FFC:
		copy(p, []byte{0xFF, 0x04, 0x00, 0x00})
	case 0x2C00:
		status := []byte{0x02, 0x03, 0x00, 0x00}
		for i := range p {
			p[i] = 0
			if i < 4 {
				p[i] = status[i]
			}
		}
	case 0x2C04:
		if len(p) == 24 {
			p[14], p[15] = frameSize>>8, frameSize&0xff
		}
	default:
		if len(p) == 1 {
			p[0] = 0
		}
	}
}

func (s *fakeSensor) Tx(addr uint16, w, r []byte) error {
	if addr != Address {
		return fmt.Errorf("address %#x", addr)
	}
	if s.fail != nil {
		return s.fail
	}
	reg := binary.BigEndian.Uint16(w)
	if len(r) == 0 {
		s.ops = append(s.ops, op{kind: 'W', reg: reg, n: len(w) - 2, data: append([]byte(nil), w[2:]...)})
		return nil
	}
	s.ops = append(s.ops, op{kind: 'R', reg: reg, n: len(r)})
	reply(reg, r)
	return nil
}

func (s *fakeSensor) sleep(d time.Duration) {
	s.ops = append(s.ops, op{kind: 'D', n: int(d / time.Millisecond)})
}

// log renders the transactions like the harness, merging the chunks of a
// large write.
func (s *fakeSensor) log() []string {
	var merged []op
	for _, o := range s.ops {
		if k := len(merged) - 1; k >= 0 && o.kind == 'W' && merged[k].kind == 'W' &&
			merged[k].n > 1 && o.n > 1 && int(merged[k].reg)+merged[k].n == int(o.reg) {
			merged[k].data = append(merged[k].data, o.data...)
			merged[k].n += o.n
			continue
		}
		merged = append(merged, o)
	}
	var lines []string
	for _, o := range merged {
		switch o.kind {
		case 'W':
			h := fnv.New32a()
			h.Write(o.data)
			lines = append(lines, fmt.Sprintf("W %04x %d %08x", o.reg, o.n, h.Sum32()))
		case 'R':
			lines = append(lines, fmt.Sprintf("R %04x %d", o.reg, o.n))
		case 'D':
			lines = append(lines, fmt.Sprintf("D %d", o.n))
		}
	}
	return lines
}

func newDevice() (*Device, *fakeSensor) {
	s := &fakeSensor{}
	d := New(s)
	d.Sleep = s.sleep
	return &d, s
}

// configure runs the sequence of testdata/harness/harness.c.
func configure(d *Device) (Motion, error) {
	alive, err := d.IsAlive()
	if err != nil {
		return Motion{}, err
	}
	if !alive {
		return Motion{}, ErrNotAlive
	}
	steps := []func() error{
		d.Init,
		func() error { return d.SetResolution(Resolution4x4) },
		func() error { return d.SetRangingFrequency(1) },
		func() error { return d.SetRangingMode(RangingAutonomous) },
		func() error { return d.SetIntegrationTime(5) },
		func() error { return d.MotionIndicatorInit(Resolution4x4) },
		func() error { return d.SetMotionDistance(1000, 2400) },
		func() error { return d.SetMotionThresholds(44) },
		func() error { return d.EnableDetectionThresholds(true) },
		d.StartRanging,
	}
	for i, step := range steps {
		if err := step(); err != nil {
			return Motion{}, fmt.Errorf("step %d: %w", i, err)
		}
	}
	m, err := d.Motion()
	if err != nil {
		return m, err
	}
	return m, d.StopRanging()
}

func TestMatchesULD(t *testing.T) {
	f, err := os.Open("testdata/configure.golden")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var want []string
	var motion string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "M ") {
			motion = line
		} else {
			want = append(want, line)
		}
	}

	d, s := newDevice()
	m, err := configure(d)
	if err != nil {
		t.Fatal(err)
	}
	got := s.log()

	for i := 0; i < len(got) && i < len(want); i++ {
		if got[i] != want[i] {
			t.Fatalf("transaction %d: got %q, want %q (C driver)", i, got[i], want[i])
		}
	}
	if len(got) != len(want) {
		t.Fatalf("%d transactions, want %d", len(got), len(want))
	}

	gotMotion := "M"
	for _, v := range m.Values {
		gotMotion += fmt.Sprintf(" %d", v)
	}
	if gotMotion != motion {
		t.Errorf("motion %q, want %q", gotMotion, motion)
	}
	if m.Global1 != 1111 || m.Global2 != 2222 || m.Status != 1 || m.NbDetected != 3 || m.NbAggregates != 16 {
		t.Errorf("motion %+v", m)
	}
}

func TestFirmwareChunks(t *testing.T) {
	d, s := newDevice()
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}
	for _, o := range s.ops {
		if o.kind == 'W' && o.n > bufSize {
			t.Fatalf("write of %d bytes exceeds the %d byte buffer", o.n, bufSize)
		}
	}
	if len(firmware) != 0x15000 || len(defaultConfiguration) != 972 ||
		len(defaultXtalk) != xtalkSize || len(getNVMCmd) != 40 {
		t.Errorf("embedded sizes %d %d %d %d", len(firmware), len(defaultConfiguration),
			len(defaultXtalk), len(getNVMCmd))
	}
}

func TestInvalid(t *testing.T) {
	d, s := newDevice()
	for _, err := range []error{
		d.SetMotionThresholds(0),
		d.SetMotionThresholds(MaxMotionThreshold + 1),
		d.SetRangingFrequency(0),
		d.SetRangingFrequency(61),
		d.SetIntegrationTime(1),
		d.SetIntegrationTime(1001),
		d.SetResolution(32),
		d.SetRangingMode(2),
		d.MotionIndicatorInit(32),
		d.SetMotionDistance(400, 1500), // before MotionIndicatorInit
	} {
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("err %v, want ErrInvalid", err)
		}
	}
	if len(s.ops) != 0 {
		t.Errorf("invalid parameters caused %d transactions", len(s.ops))
	}
}

func TestBusError(t *testing.T) {
	d, s := newDevice()
	s.fail = errors.New("nack")
	if err := d.Init(); err == nil || err.Error() != "nack" {
		t.Errorf("err %v", err)
	}
	if _, err := d.IsAlive(); err == nil {
		t.Error("IsAlive succeeded on a failing bus")
	}
}

// distancePairs lists windows with their validity; thunder_test.go holds
// the same table for thunder.Occupancy.DistanceValid.
var distancePairs = []struct {
	min, max uint16
	valid    bool
}{
	{400, 1500, true},
	{400, 1900, true},
	{2500, 4000, true},
	{399, 1000, false},
	{1000, 4001, false},
	{1000, 2501, false},
	{1500, 1500, false},
	{1500, 1000, false},
}

func TestMotionDistance(t *testing.T) {
	for _, c := range []struct {
		min, max      uint16
		refBinOffset  int32
		featureLength uint8
	}{
		// Computed by the ULD's formula in single precision.
		{400, 1500, 13636, 9},
		{400, 1900, 13636, 12},
		{1000, 2500, 46382, 12},
		{2500, 4000, 128246, 12},
	} {
		r, f := motionFields(c.min, c.max)
		if r != c.refBinOffset || f != c.featureLength {
			t.Errorf("motionFields(%d, %d) = %d, %d, want %d, %d",
				c.min, c.max, r, f, c.refBinOffset, c.featureLength)
		}
	}

	d, s := newDevice()
	if err := d.MotionIndicatorInit(Resolution4x4); err != nil {
		t.Fatal(err)
	}
	n := len(s.ops)
	for _, c := range distancePairs {
		if got := ValidMotionDistance(c.min, c.max); got != c.valid {
			t.Errorf("ValidMotionDistance(%d, %d) = %v", c.min, c.max, got)
		}
		if err := d.SetMotionDistance(c.min, c.max); c.valid != (err == nil) {
			t.Errorf("SetMotionDistance(%d, %d): %v", c.min, c.max, err)
		}
		if !c.valid && len(s.ops) != n {
			t.Errorf("SetMotionDistance(%d, %d) caused transactions", c.min, c.max)
		}
		n = len(s.ops)
	}
}
