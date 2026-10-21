package legacydfu

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// The address limits of an application image, as adafruit-nrfutil converts a
// hex file: the MBR below mbrEnd and the UICR from uicrStart on are left out.
const (
	mbrEnd    = 0x1000
	uicrStart = 0x10000000
)

// ReadHex reads an Intel HEX file into one contiguous image, as
// adafruit-nrfutil dfu genpkg does: from the lowest address, at least
// mbrEnd, to the highest below uicrStart, with the gaps filled with 0xFF and
// the size rounded up to a multiple of 4. It returns the image and its start
// address.
func ReadHex(r io.Reader) (base uint32, image []byte, err error) {
	type chunk struct {
		addr uint32
		data []byte
	}
	var (
		chunks []chunk
		upper  uint32 // the extended linear or segment address
		eof    bool
	)
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if eof {
			return 0, nil, fmt.Errorf("hex line %d: data after the end of file record", n)
		}
		rec, err := parseRecord(line)
		if err != nil {
			return 0, nil, fmt.Errorf("hex line %d: %w", n, err)
		}
		data := rec[4:]
		switch typ := rec[3]; typ {
		case 0x00:
			addr := upper + (uint32(rec[1])<<8 | uint32(rec[2]))
			chunks = append(chunks, chunk{addr, data})
		case 0x01:
			eof = true
		case 0x02, 0x04:
			if len(data) != 2 {
				return 0, nil, fmt.Errorf("hex line %d: address record of %d bytes", n, len(data))
			}
			upper = uint32(data[0])<<8 | uint32(data[1])
			if typ == 0x02 {
				upper <<= 4
			} else {
				upper <<= 16
			}
		case 0x03, 0x05:
			// A start address, which the image does not hold.
		default:
			return 0, nil, fmt.Errorf("hex line %d: record type %#02x", n, typ)
		}
	}
	if err := sc.Err(); err != nil {
		return 0, nil, err
	}
	if !eof {
		return 0, nil, errors.New("hex: no end of file record")
	}

	// The extent of the data in the application area.
	lo, hi := uint32(uicrStart), uint32(0)
	for _, c := range chunks {
		start, end := max(c.addr, mbrEnd), min(c.addr+uint32(len(c.data)), uicrStart)
		if start < end {
			lo, hi = min(lo, start), max(hi, end)
		}
	}
	if lo >= hi {
		return 0, nil, errors.New("hex: no application data")
	}
	image = make([]byte, (hi-lo+3)&^3)
	for i := range image {
		image[i] = 0xFF
	}
	written := make([]bool, len(image))
	for _, c := range chunks {
		for i, b := range c.data {
			a := c.addr + uint32(i)
			if a < lo || a >= hi {
				continue
			}
			if written[a-lo] {
				return 0, nil, fmt.Errorf("hex: address %#x written twice", a)
			}
			written[a-lo] = true
			image[a-lo] = b
		}
	}
	return lo, image, nil
}

// parseRecord decodes one record, ":llaaaatt<data>cc", and checks its length
// and checksum. It returns the bytes from the length to the data.
func parseRecord(line string) ([]byte, error) {
	if line[0] != ':' {
		return nil, errors.New("no record start")
	}
	rec, err := hex.DecodeString(line[1:])
	if err != nil {
		return nil, err
	}
	if len(rec) < 5 || len(rec) != 5+int(rec[0]) {
		return nil, errors.New("bad record length")
	}
	var sum byte
	for _, b := range rec {
		sum += b
	}
	if sum != 0 {
		return nil, errors.New("bad checksum")
	}
	return rec[:len(rec)-1], nil
}
