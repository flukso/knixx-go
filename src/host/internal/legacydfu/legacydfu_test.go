package legacydfu

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeBootloader answers like the SDK 11 bootloader and logs the writes.
type fakeBootloader struct {
	log    []string
	notify chan []byte
	// state of the image transfer
	receiving bool
	size      int
	got       []byte
	packets   int
	prn       int
	// failValidate answers validation with a CRC error; silent drops the
	// response to start.
	failValidate bool
	silent       bool
	// packetSize is what PacketSize reports.
	packetSize int
	// sizes of the image packets
	sizes []int
}

func newFake() *fakeBootloader {
	return &fakeBootloader{notify: make(chan []byte, 16)}
}

func (f *fakeBootloader) Notifications() <-chan []byte { return f.notify }

func (f *fakeBootloader) PacketSize() int { return f.packetSize }

func (f *fakeBootloader) respond(op, status byte) {
	f.notify <- []byte{opResponse, op, status}
}

func (f *fakeBootloader) WriteControl(p []byte) error {
	f.log = append(f.log, fmt.Sprintf("C % x", p))
	switch p[0] {
	case opInit:
		if p[1] == initDone {
			f.respond(opInit, statSuccess)
		}
	case opPRNReq:
		f.prn = int(binary.LittleEndian.Uint16(p[1:]))
	case opReceive:
		f.receiving = true
	case opValidate:
		if f.failValidate {
			f.respond(opValidate, 5)
		} else {
			f.respond(opValidate, statSuccess)
		}
	}
	return nil
}

func (f *fakeBootloader) WritePacket(p []byte) error {
	if len(p) > chunk(f) || len(p)%4 != 0 && f.receiving {
		return fmt.Errorf("bad packet of %d bytes", len(p))
	}
	if !f.receiving {
		f.log = append(f.log, fmt.Sprintf("P % x", p))
		if len(p) == 12 && f.size == 0 {
			f.size = int(binary.LittleEndian.Uint32(p[8:]))
			if !f.silent {
				f.respond(opStart, statSuccess)
			}
		}
		return nil
	}
	f.got = append(f.got, p...)
	f.packets++
	f.sizes = append(f.sizes, len(p))
	if len(f.got) == f.size {
		f.respond(opReceive, statSuccess)
	} else if f.packets%f.prn == 0 {
		var n [5]byte
		n[0] = opPRN
		binary.LittleEndian.PutUint32(n[1:], uint32(len(f.got)))
		f.notify <- n[:]
	}
	return nil
}

func testPackage(n int) *Package {
	p := &Package{Bin: make([]byte, n), Dat: []byte{0x52, 0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 1, 0, 0xb6, 0, 0xd9, 0x7f}}
	for i := range p.Bin {
		p.Bin[i] = byte(i)
	}
	return p
}

func TestUpdate(t *testing.T) {
	f := newFake()
	p := testPackage(1000)
	var last int
	if err := Update(f, p, func(sent, total int) { last = sent }); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.got, p.Bin) {
		t.Error("image differs")
	}
	if last != len(p.Bin) {
		t.Errorf("progress ended at %d", last)
	}
	want := []string{
		"C 01 04",
		"P 00 00 00 00 00 00 00 00 e8 03 00 00",
		"C 02 00",
		"P 52 00 ff ff ff ff ff ff 01 00 b6 00 d9 7f",
		"C 02 01",
		"C 08 0a 00",
		"C 03",
		"C 04",
		"C 05",
	}
	if strings.Join(f.log, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(f.log, "\n"), strings.Join(want, "\n"))
	}
}

func TestUpdatePacketSize(t *testing.T) {
	for _, c := range []struct{ size, want int }{
		{0, MinChunk},   // unknown MTU
		{10, MinChunk},  // never below the default MTU
		{150, 148},      // a multiple of 4
		{244, MaxChunk}, // MTU 247
		{514, MaxChunk}, // MTU 517
	} {
		f := newFake()
		f.packetSize = c.size
		p := testPackage(3000)
		if err := Update(f, p, nil); err != nil {
			t.Fatalf("size %d: %v", c.size, err)
		}
		if !bytes.Equal(f.got, p.Bin) {
			t.Errorf("size %d: image differs", c.size)
		}
		if f.sizes[0] != c.want || slices.Max(f.sizes) != c.want {
			t.Errorf("size %d: packets %v, want %d", c.size, f.sizes, c.want)
		}
	}
}

func TestUpdateValidateFails(t *testing.T) {
	f := newFake()
	f.failValidate = true
	err := Update(f, testPackage(40), nil)
	if err == nil || !strings.Contains(err.Error(), "validate: CRC error") {
		t.Errorf("err %v", err)
	}
	if f.log[len(f.log)-1] == "C 05" {
		t.Error("activated after a failed validation")
	}
}

func TestUpdateTimeout(t *testing.T) {
	prev := SlowTimeout
	SlowTimeout = 20 * time.Millisecond
	defer func() { SlowTimeout = prev }()
	f := newFake()
	f.silent = true
	err := Update(f, testPackage(40), nil)
	if err == nil || !strings.Contains(err.Error(), "start: no answer") {
		t.Errorf("err %v", err)
	}
	if len(f.log) != 2 {
		t.Errorf("went on after the timeout: %q", f.log)
	}
}

// badReceipt reports a wrong byte count.
type badReceipt struct{ *fakeBootloader }

func (b badReceipt) WritePacket(p []byte) error {
	if b.receiving && b.packets+1 == PRN {
		b.got = append(b.got, p...)
		b.packets++
		b.notify <- []byte{opPRN, 1, 0, 0, 0}
		return nil
	}
	return b.fakeBootloader.WritePacket(p)
}

func TestUpdateBadReceipt(t *testing.T) {
	err := Update(badReceipt{newFake()}, testPackage(400), nil)
	if err == nil || !strings.Contains(err.Error(), "bootloader has 1 bytes, sent 200") {
		t.Errorf("err %v", err)
	}
}

type failingLink struct{ *fakeBootloader }

var errGone = errors.New("gone")

func (failingLink) WriteControl([]byte) error { return errGone }

func TestUpdateWriteError(t *testing.T) {
	if err := Update(failingLink{newFake()}, testPackage(40), nil); !errors.Is(err, errGone) {
		t.Errorf("err %v", err)
	}
}

// zipOf builds a package with the given files.
func zipOf(t *testing.T, files map[string][]byte) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write(data)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return z
}

const manifestJSON = `{"manifest": {"application": {"bin_file": "knixx.bin", "dat_file": "knixx.dat",
	"init_packet_data": {"device_type": 82}}, "dfu_version": 0.5}}`

func TestRead(t *testing.T) {
	p, err := Read(zipOf(t, map[string][]byte{
		"manifest.json": []byte(manifestJSON),
		"knixx.bin":     make([]byte, 8),
		"knixx.dat":     {1, 2, 3},
	}))
	if err != nil || len(p.Bin) != 8 || len(p.Dat) != 3 {
		t.Errorf("%+v %v", p, err)
	}
}

func TestReadErrors(t *testing.T) {
	for name, files := range map[string]map[string][]byte{
		"no manifest":    {"knixx.bin": make([]byte, 8)},
		"no application": {"manifest.json": []byte(`{"manifest": {"bootloader": {}}}`)},
		"no bin":         {"manifest.json": []byte(manifestJSON), "knixx.dat": {1}},
		"odd size":       {"manifest.json": []byte(manifestJSON), "knixx.bin": make([]byte, 6), "knixx.dat": {1}},
	} {
		if _, err := Read(zipOf(t, files)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
