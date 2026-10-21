package legacydfu

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// knixx is the init packet that make dfu builds.
var knixx = InitPacket{DevType: 0x0052, DevRev: 0xFFFF, AppVer: 0xFFFFFFFF, SDReq: []uint16{0x00B6}}

func TestCRC16(t *testing.T) {
	if got := CRC16([]byte("123456789")); got != 0x29B1 {
		t.Errorf("CRC16 = %#04x, want 0x29b1", got)
	}
}

func TestInitPacket(t *testing.T) {
	// The knixx.dat of a package built by adafruit-nrfutil.
	want := []byte{0x52, 0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01, 0x00, 0xb6, 0x00, 0xea, 0x9a}
	if got := knixx.Marshal(0x9AEA); !bytes.Equal(got, want) {
		t.Errorf("Marshal = % x, want % x", got, want)
	}
}

// record returns the hex record of type typ at addr with data.
func record(typ byte, addr uint16, data ...byte) string {
	rec := append([]byte{byte(len(data)), byte(addr >> 8), byte(addr), typ}, data...)
	var sum byte
	for _, b := range rec {
		sum += b
	}
	return fmt.Sprintf(":%X%02X\n", rec, -sum)
}

func TestReadHex(t *testing.T) {
	hex := record(0x04, 0, 0x00, 0x02) + // 0x20000
		record(0x00, 0x6000, 1, 2, 3, 4) +
		record(0x00, 0x6008, 5) + // a gap of 4, an odd end
		record(0x05, 0, 0, 2, 0x60, 0) +
		record(0x04, 0, 0x10, 0x00) + // the UICR, left out
		record(0x00, 0x1014, 0xaa, 0xbb, 0xcc, 0xdd) +
		record(0x01, 0)
	base, image, err := ReadHex(strings.NewReader(hex))
	want := []byte{1, 2, 3, 4, 0xff, 0xff, 0xff, 0xff, 5, 0xff, 0xff, 0xff}
	if err != nil || base != 0x26000 || !bytes.Equal(image, want) {
		t.Errorf("ReadHex = %#x, % x, %v; want 0x26000, % x", base, image, err, want)
	}
}

func TestReadHexMBR(t *testing.T) {
	// Data below 0x1000 is the MBR's and left out.
	hex := record(0x00, 0x0ffe, 1, 2, 3, 4) + record(0x01, 0)
	base, image, err := ReadHex(strings.NewReader(hex))
	if err != nil || base != 0x1000 || !bytes.Equal(image, []byte{3, 4, 0xff, 0xff}) {
		t.Errorf("ReadHex = %#x, % x, %v", base, image, err)
	}
}

func TestReadHexErrors(t *testing.T) {
	data := record(0x00, 0x6000, 1, 2, 3, 4)
	for name, hex := range map[string]string{
		"no eof":       data,
		"bad checksum": strings.Replace(data, "01020304", "01020305", 1) + record(0x01, 0),
		"bad length":   ":0560000001020304F2\n" + record(0x01, 0),
		"no colon":     strings.TrimPrefix(data, ":") + record(0x01, 0),
		"bad type":     record(0x06, 0) + record(0x01, 0),
		"overlap":      data + record(0x00, 0x6002, 9) + record(0x01, 0),
		"after eof":    record(0x01, 0) + data,
		"empty":        record(0x01, 0),
	} {
		if _, _, err := ReadHex(strings.NewReader(hex)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestWriteZip(t *testing.T) {
	p := NewPackage([]byte{1, 2, 3, 4, 5, 6, 7, 8}, knixx)
	var buf bytes.Buffer
	if err := p.WriteZip(&buf, "knixx", knixx); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Read(z)
	if err != nil || !bytes.Equal(got.Bin, p.Bin) || !bytes.Equal(got.Dat, p.Dat) {
		t.Fatalf("Read = %+v, %v; want %+v", got, err, p)
	}

	// The manifest as adafruit-nrfutil writes it.
	raw, err := readFile(z, "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(`{
    "manifest": {
        "application": {
            "bin_file": "knixx.bin",
            "dat_file": "knixx.dat",
            "init_packet_data": {
                "application_version": 4294967295,
                "device_revision": 65535,
                "device_type": 82,
                "firmware_crc16": %d,
                "softdevice_req": [
                    182
                ]
            }
        },
        "dfu_version": 0.5
    }
}`, CRC16(p.Bin))
	if string(raw) != want {
		t.Errorf("manifest.json:\n%s\nwant:\n%s", raw, want)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Error(err)
	}
}

// TestBuildHex builds the package of the firmware, when make nrf or make dfu
// has built its hex file.
func TestBuildHex(t *testing.T) {
	f, err := os.Open("../../../../build/knixx.hex")
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	base, bin, err := ReadHex(f)
	if err != nil {
		t.Fatal(err)
	}
	if base != 0x26000 {
		t.Errorf("application at %#x, want 0x26000, after the S140", base)
	}
	var buf bytes.Buffer
	if err := NewPackage(bin, knixx).WriteZip(&buf, "knixx", knixx); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Read(z); err != nil {
		t.Error(err)
	}
}
