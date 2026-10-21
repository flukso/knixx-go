package main

import (
	"fmt"
	"knixx/host/internal/legacydfu"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// runGenpkg builds the DFU package out from the application hex file in, as
// adafruit-nrfutil dfu genpkg does. The zip is written to a temporary file
// and renamed, so that a failed run leaves no partial package.
func runGenpkg(in, out string, ip legacydfu.InitPacket) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	base, bin, err := legacydfu.ReadHex(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("%s: %w", in, err)
	}
	p := legacydfu.NewPackage(bin, ip)

	tmp, err := os.CreateTemp(filepath.Dir(out), filepath.Base(out)+".*")
	if err != nil {
		return err
	}
	name := strings.TrimSuffix(filepath.Base(in), filepath.Ext(in))
	err = p.WriteZip(tmp, name, ip)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), out)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	status("%s: application of %d bytes at %#x, crc16 %#04x", out, len(bin), base, legacydfu.CRC16(bin))
	return nil
}

// parseIDs parses a comma separated list of 16-bit numbers, such as 0x00B6.
func parseIDs(s string) ([]uint16, error) {
	var ids []uint16
	for _, f := range strings.Split(s, ",") {
		v, err := strconv.ParseUint(strings.TrimSpace(f), 0, 16)
		if err != nil {
			return nil, err
		}
		ids = append(ids, uint16(v))
	}
	return ids, nil
}
