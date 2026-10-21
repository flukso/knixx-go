package uf2

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const family = 0xADA52840

// file returns a UF2 file of n blocks for family f.
func file(n int, f uint32) []byte {
	data := make([]byte, n*BlockSize)
	for i := 0; i < n; i++ {
		b := data[i*BlockSize:]
		put := func(off int, v uint32) { binary.LittleEndian.PutUint32(b[off:], v) }
		put(0, magicStart0)
		put(4, magicStart1)
		put(8, flagFamilyID)
		put(12, 0x26000+uint32(i)*256)
		put(16, 256)
		put(20, uint32(i))
		put(24, uint32(n))
		put(28, f)
		put(508, magicEnd)
	}
	return data
}

func TestCheck(t *testing.T) {
	if err := Check(file(4, family), family); err != nil {
		t.Errorf("valid file: %v", err)
	}
	gap := file(4, family)
	binary.LittleEndian.PutUint32(gap[2*BlockSize+20:], 3)
	noFamily := file(2, family)
	binary.LittleEndian.PutUint32(noFamily[8:], 0)
	badMagic := file(2, family)
	badMagic[BlockSize+508] ^= 1
	for name, data := range map[string][]byte{
		"empty":        nil,
		"truncated":    file(3, family)[:3*BlockSize-1],
		"wrong family": file(2, 0xE48BFF56),
		"no family":    noFamily,
		"bad magic":    badMagic,
		"block gap":    gap,
		"short count":  file(4, family)[:2*BlockSize],
	} {
		if err := Check(data, family); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFatMounts(t *testing.T) {
	mounts := `sysfs /sys sysfs rw 0 0
/dev/sda1 /media/bart/FTHR840BOOT vfat rw,nosuid 0 0
/dev/sdb1 /run/media/bart/FTHR840BOOT\0401 vfat rw 0 0
/dev/sdc1 /media/bart/FTHR840BOOT ext4 rw 0 0
/dev/sdd1 /media/bart/OTHER vfat rw 0 0
`
	got := fatMounts(strings.NewReader(mounts), []string{"FTHR840BOOT", "FTHR840BOOT 1"})
	want := []string{"/media/bart/FTHR840BOOT", "/run/media/bart/FTHR840BOOT 1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fatMounts = %q, want %q", got, want)
	}
}

func TestBoardID(t *testing.T) {
	dir := t.TempDir()
	info := "UF2 Bootloader 0.6.0 lib/nrfx (v2.0.0)\r\nModel: Adafruit Feather nRF52840 Express\r\nBoard-ID: nRF52840-Feather-revD\r\n"
	if err := os.WriteFile(filepath.Join(dir, InfoFile), []byte(info), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := BoardID(dir); got != "nRF52840-Feather-revD" {
		t.Errorf("BoardID = %q", got)
	}
	if got := BoardID(t.TempDir()); got != "" {
		t.Errorf("BoardID without the file = %q", got)
	}
}

// TestCheckBuild checks the firmware make nrf built, if there is one.
func TestCheckBuild(t *testing.T) {
	data, err := os.ReadFile("../../../../build/knixx.uf2")
	if err != nil {
		t.Skip("no build/knixx.uf2:", err)
	}
	if err := Check(data, family); err != nil {
		t.Error(err)
	}
}
