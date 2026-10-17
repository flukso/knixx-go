package sht2x

import (
	"bytes"
	"errors"
	"testing"
)

// fakeBus records the bytes written and answers reads with reply.
type fakeBus struct {
	addr    uint16
	written []byte
	reply   []byte
	err     error
}

func (f *fakeBus) Tx(addr uint16, w, r []byte) error {
	f.addr = addr
	f.written = append(f.written, w...)
	copy(r, f.reply)
	return f.err
}

func TestCRC(t *testing.T) {
	// Example from the datasheet.
	if got := crc8([]byte{0x68, 0x3a}); got != 0x7c {
		t.Errorf("crc8 = %#02x, want 0x7c", got)
	}
}

func TestReadTemperature(t *testing.T) {
	// The low status bits (0x17 vs 0x14) must not change the result.
	for _, reply := range [][]byte{{0x66, 0x14, 0x88}, {0x66, 0x17, 0xdb}} {
		bus := fakeBus{reply: reply}
		d := New(&bus)
		got, err := d.ReadTemperature()
		if err != nil {
			t.Fatal(err)
		}
		if got != 23217 {
			t.Errorf("% x: temperature %d m°C, want 23217", reply, got)
		}
		if bus.addr != Address || !bytes.Equal(bus.written, []byte{cmdTriggerT}) {
			t.Errorf("wrote % x to %#x", bus.written, bus.addr)
		}
	}
}

func TestReadHumidity(t *testing.T) {
	bus := fakeBus{reply: []byte{0x7c, 0x80, 0xf5}}
	d := New(&bus)
	got, err := d.ReadHumidity()
	if err != nil {
		t.Fatal(err)
	}
	if got != 5479 {
		t.Errorf("humidity %d c%%, want 5479", got)
	}
	if !bytes.Equal(bus.written, []byte{cmdTriggerRH}) {
		t.Errorf("wrote % x", bus.written)
	}
}

func TestHumidityClamped(t *testing.T) {
	bus := fakeBus{reply: []byte{0x00, 0x00, 0x00}} // crc8 of 00 00 is 0
	d := New(&bus)
	if got, err := d.ReadHumidity(); err != nil || got != 0 {
		t.Errorf("got %d, %v; want 0", got, err)
	}
}

func TestChecksumError(t *testing.T) {
	bus := fakeBus{reply: []byte{0x66, 0x14, 0x00}}
	d := New(&bus)
	if _, err := d.ReadTemperature(); !errors.Is(err, ErrChecksum) {
		t.Errorf("err = %v, want ErrChecksum", err)
	}
}

func TestBusError(t *testing.T) {
	want := errors.New("nack")
	bus := fakeBus{err: want}
	d := New(&bus)
	if _, err := d.ReadHumidity(); !errors.Is(err, want) {
		t.Errorf("err = %v", err)
	}
	if err := d.Configure(); !errors.Is(err, want) {
		t.Errorf("Configure err = %v", err)
	}
}
