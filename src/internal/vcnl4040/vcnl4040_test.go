package vcnl4040

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// fakeBus logs the transactions and answers register reads from regs.
type fakeBus struct {
	log  []string
	regs map[byte]uint16
	// failRead fails the reads of a register.
	failRead map[byte]bool
	err      error
}

var errNack = errors.New("nack")

func (f *fakeBus) Tx(addr uint16, w, r []byte) error {
	if addr != Address {
		return fmt.Errorf("address %#x", addr)
	}
	if f.err != nil {
		return f.err
	}
	if len(r) == 0 {
		f.log = append(f.log, fmt.Sprintf("W %02x % x", w[0], w[1:]))
		return nil
	}
	f.log = append(f.log, fmt.Sprintf("R %02x", w[0]))
	if f.failRead[w[0]] {
		return errNack
	}
	v := f.regs[w[0]]
	r[0], r[1] = byte(v), byte(v>>8)
	return nil
}

func newDevice(f *fakeBus) *Device {
	d := New(f)
	d.Sleep = func(d time.Duration) { f.log = append(f.log, fmt.Sprintf("D %d", d/time.Millisecond)) }
	return &d
}

func TestConfigure(t *testing.T) {
	f := &fakeBus{regs: map[byte]uint16{regID: 0x0186}}
	if err := newDevice(f).Configure(); err != nil {
		t.Fatal(err)
	}
	want := []string{"R 0c", "W 03 01 00", "W 04 00 00", "W 00 01 00"}
	if !reflect.DeepEqual(f.log, want) {
		t.Errorf("got %q, want %q", f.log, want)
	}
}

func TestConfigureID(t *testing.T) {
	f := &fakeBus{regs: map[byte]uint16{regID: 0x0086}}
	if err := newDevice(f).Configure(); !errors.Is(err, ErrID) {
		t.Errorf("err %v, want ErrID", err)
	}
	if len(f.log) != 1 {
		t.Errorf("wrote to an unknown device: %q", f.log)
	}
}

func TestReadIlluminance(t *testing.T) {
	f := &fakeBus{regs: map[byte]uint16{regALSData: 0x1234}}
	lux10, err := newDevice(f).ReadIlluminance()
	if err != nil {
		t.Fatal(err)
	}
	if lux10 != 0x1234 {
		t.Errorf("lux10 = %d, want %d", lux10, 0x1234)
	}
	want := []string{"W 00 00 00", "D 160", "R 09", "W 00 01 00"}
	if !reflect.DeepEqual(f.log, want) {
		t.Errorf("got %q, want %q", f.log, want)
	}
}

func TestReadErrorShutsDown(t *testing.T) {
	f := &fakeBus{failRead: map[byte]bool{regALSData: true}}
	if _, err := newDevice(f).ReadIlluminance(); !errors.Is(err, errNack) {
		t.Errorf("err %v, want nack", err)
	}
	if last := f.log[len(f.log)-1]; last != "W 00 01 00" {
		t.Errorf("last transaction %q, want the shutdown", last)
	}
}

func TestBusError(t *testing.T) {
	f := &fakeBus{err: errNack}
	d := newDevice(f)
	if err := d.Configure(); !errors.Is(err, errNack) {
		t.Errorf("Configure: %v", err)
	}
	if _, err := d.ReadIlluminance(); !errors.Is(err, errNack) {
		t.Errorf("ReadIlluminance: %v", err)
	}
}
