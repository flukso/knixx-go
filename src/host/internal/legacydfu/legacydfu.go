// Package legacydfu updates the application of a Nordic SDK 11 bootloader,
// such as the Adafruit nRF52 bootloader, over BLE with the legacy DFU
// protocol. The transport is abstracted by Link. The packages are those of
// adafruit-nrfutil dfu genpkg: NewPackage builds one from an image, which
// ReadHex makes from a hex file, WriteZip writes it and Open reads it.
package legacydfu

import (
	"archive/zip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// The DFU service and its characteristics, from the bootloader's ble_dfu.
const (
	ServiceUUID      = "00001530-1212-efde-1523-785feabcd123"
	ControlPointUUID = "00001531-1212-efde-1523-785feabcd123"
	PacketUUID       = "00001532-1212-efde-1523-785feabcd123"
)

// Control point op codes.
const (
	opStart     = 0x01
	opInit      = 0x02
	opReceive   = 0x03
	opValidate  = 0x04
	opActivate  = 0x05
	opPRNReq    = 0x08
	opResponse  = 0x10
	opPRN       = 0x11
	initRx      = 0x00
	initDone    = 0x01
	modeApp     = 0x04
	statSuccess = 0x01
)

// Packet writes carry at least MinChunk bytes, the payload with the default
// ATT MTU, and at most MaxChunk, the payload with the MTU of 247 that the
// Adafruit bootloader negotiates. The bootloader takes only multiples of 4
// bytes per write.
const (
	MinChunk = 20
	MaxChunk = 244
)

// PRN is the number of firmware packets after which the bootloader reports
// the bytes it received; it keeps the writes without response from
// overrunning it.
const PRN = 10

// Timeouts for the bootloader's answers. Start erases the application area
// and validation reads it back, so they get longer.
var (
	ReplyTimeout = 10 * time.Second
	SlowTimeout  = 30 * time.Second
)

// Package is the application image and its init packet.
type Package struct {
	Bin, Dat []byte
}

// manifest is the part of manifest.json in a genpkg package used here.
type manifest struct {
	Manifest struct {
		Application *struct {
			BinFile string `json:"bin_file"`
			DatFile string `json:"dat_file"`
		} `json:"application"`
	} `json:"manifest"`
}

// Open reads the application of the DFU package at path.
func Open(path string) (*Package, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	return Read(&z.Reader)
}

// Read reads the application of a DFU package.
func Read(z *zip.Reader) (*Package, error) {
	raw, err := readFile(z, "manifest.json")
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	app := m.Manifest.Application
	if app == nil {
		return nil, errors.New("dfu package: no application")
	}
	var p Package
	if p.Bin, err = readFile(z, app.BinFile); err != nil {
		return nil, err
	}
	if p.Dat, err = readFile(z, app.DatFile); err != nil {
		return nil, err
	}
	if len(p.Bin) == 0 || len(p.Bin)%4 != 0 {
		return nil, fmt.Errorf("dfu package: application of %d bytes, not a non-zero multiple of 4", len(p.Bin))
	}
	return &p, nil
}

func readFile(z *zip.Reader, name string) ([]byte, error) {
	f, err := z.Open(name)
	if err != nil {
		return nil, fmt.Errorf("dfu package: %w", err)
	}
	defer f.Close()
	return io.ReadAll(f)
}

// Link is a connection to the DFU service.
type Link interface {
	// WriteControl writes the control point, with response.
	WriteControl(p []byte) error
	// WritePacket writes the packet characteristic, without response.
	WritePacket(p []byte) error
	// Notifications delivers the notifications of the control point.
	Notifications() <-chan []byte
	// PacketSize is the largest payload of a packet write, the ATT MTU
	// less 3; 0 when unknown.
	PacketSize() int
}

// chunk is the payload of the packet writes on l.
func chunk(l Link) int {
	n := min(l.PacketSize(), MaxChunk) &^ 3
	return max(n, MinChunk)
}

// Update sends p to the bootloader on l and activates it. progress, when not
// nil, is called with the bytes sent of the image.
func Update(l Link, p *Package, progress func(sent, total int)) error {
	u := updater{l: l, chunk: chunk(l)}

	var sizes [12]byte
	binary.LittleEndian.PutUint32(sizes[8:], uint32(len(p.Bin)))
	u.control("start", opStart, modeApp)
	u.packets("image size", sizes[:])
	u.response("start", opStart, SlowTimeout)

	u.control("init", opInit, initRx)
	u.packets("init packet", p.Dat)
	u.control("init", opInit, initDone)
	u.response("init", opInit, ReplyTimeout)

	u.control("receipt request", opPRNReq, PRN, 0)
	u.control("receive", opReceive)
	for sent, n := 0, 0; sent < len(p.Bin) && u.err == nil; n++ {
		end := min(sent+u.chunk, len(p.Bin))
		u.packets("image", p.Bin[sent:end])
		sent = end
		if (n+1)%PRN == 0 && sent < len(p.Bin) {
			u.receipt(sent)
		}
		if progress != nil && u.err == nil {
			progress(sent, len(p.Bin))
		}
	}
	u.response("receive", opReceive, ReplyTimeout)

	u.control("validate", opValidate)
	u.response("validate", opValidate, SlowTimeout)

	// The bootloader resets at once; the link may drop before the write is
	// acknowledged.
	if u.err == nil {
		l.WriteControl([]byte{opActivate})
	}
	return u.err
}

// updater runs the steps of an update and keeps the first error.
type updater struct {
	l     Link
	chunk int
	err   error
}

func (u *updater) control(step string, p ...byte) {
	if u.err != nil {
		return
	}
	if err := u.l.WriteControl(p); err != nil {
		u.err = fmt.Errorf("%s: %w", step, err)
	}
}

// packets writes p in chunks of at most u.chunk bytes.
func (u *updater) packets(step string, p []byte) {
	for len(p) > 0 && u.err == nil {
		n := min(len(p), u.chunk)
		if err := u.l.WritePacket(p[:n]); err != nil {
			u.err = fmt.Errorf("%s: %w", step, err)
		}
		p = p[n:]
	}
}

// next waits for the next notification.
func (u *updater) next(step string, timeout time.Duration) []byte {
	select {
	case n, ok := <-u.l.Notifications():
		if !ok {
			u.err = fmt.Errorf("%s: link closed", step)
			return nil
		}
		return n
	case <-time.After(timeout):
		u.err = fmt.Errorf("%s: no answer within %v", step, timeout)
		return nil
	}
}

// response waits for the response to op, skipping receipt notifications.
func (u *updater) response(step string, op byte, timeout time.Duration) {
	for u.err == nil {
		n := u.next(step, timeout)
		switch {
		case u.err != nil:
		case len(n) >= 1 && n[0] == opPRN:
			continue
		case len(n) < 3 || n[0] != opResponse || n[1] != op:
			u.err = fmt.Errorf("%s: unexpected notification % x", step, n)
		case n[2] != statSuccess:
			u.err = fmt.Errorf("%s: %s", step, status(n[2]))
		}
		return
	}
}

// receipt waits for the receipt notification of sent bytes.
func (u *updater) receipt(sent int) {
	n := u.next("receipt", ReplyTimeout)
	switch {
	case u.err != nil:
	case len(n) == 3 && n[0] == opResponse:
		u.err = fmt.Errorf("image: %s", status(n[2]))
	case len(n) != 5 || n[0] != opPRN:
		u.err = fmt.Errorf("receipt: unexpected notification % x", n)
	case int(binary.LittleEndian.Uint32(n[1:])) != sent:
		u.err = fmt.Errorf("receipt: bootloader has %d bytes, sent %d", binary.LittleEndian.Uint32(n[1:]), sent)
	}
}

// status names a response value.
func status(v byte) string {
	switch v {
	case 2:
		return "invalid state"
	case 3:
		return "not supported"
	case 4:
		return "data size exceeds limit"
	case 5:
		return "CRC error"
	case 6:
		return "operation failed"
	}
	return fmt.Sprintf("status %d", v)
}
