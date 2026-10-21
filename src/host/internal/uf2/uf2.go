// Package uf2 checks UF2 files and finds the USB drive of a UF2 bootloader,
// to which a UF2 file is copied to flash a board.
// https://github.com/microsoft/uf2
package uf2

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// BlockSize is the size of a UF2 block; every block is one sector.
const BlockSize = 512

const (
	magicStart0 = 0x0A324655
	magicStart1 = 0x9E5D5157
	magicEnd    = 0x0AB16F30

	flagNotMainFlash = 0x00000001
	flagFamilyID     = 0x00002000
)

// InfoFile is the file a UF2 bootloader's drive holds, with its board ID.
const InfoFile = "INFO_UF2.TXT"

// ErrNoDrive is returned by MountVolume when no drive has one of the names.
var ErrNoDrive = errors.New("uf2: no such drive")

// Check reports whether data is a complete UF2 file for family: whole
// blocks with the UF2 magic numbers, the family ID in every block that goes
// to the main flash, and the block numbers 0 to n-1 of n blocks.
func Check(data []byte, family uint32) error {
	if len(data) == 0 || len(data)%BlockSize != 0 {
		return fmt.Errorf("uf2: %d bytes, not whole %d-byte blocks", len(data), BlockSize)
	}
	n := uint32(len(data) / BlockSize)
	for i := uint32(0); i < n; i++ {
		b := data[i*BlockSize : (i+1)*BlockSize]
		word := func(off int) uint32 { return binary.LittleEndian.Uint32(b[off:]) }
		if word(0) != magicStart0 || word(4) != magicStart1 || word(508) != magicEnd {
			return fmt.Errorf("uf2: block %d: not a UF2 block", i)
		}
		if word(20) != i || word(24) != n {
			return fmt.Errorf("uf2: block %d is numbered %d of %d, want %d of %d",
				i, word(20), word(24), i, n)
		}
		flags := word(8)
		if flags&flagNotMainFlash != 0 {
			continue
		}
		if flags&flagFamilyID == 0 {
			return fmt.Errorf("uf2: block %d has no family ID", i)
		}
		if f := word(28); f != family {
			return fmt.Errorf("uf2: block %d is for family %#08x, want %#08x", i, f, family)
		}
	}
	return nil
}

// FindVolume returns the mount point of the first drive named one of names
// that holds InfoFile: /Volumes/<name> on macOS, a vfat mount from
// /proc/mounts on Linux. A drive nothing has mounted yet is not found; see
// MountVolume.
func FindVolume(names []string) (string, bool) {
	var mounts []string
	switch runtime.GOOS {
	case "darwin":
		for _, n := range names {
			mounts = append(mounts, filepath.Join("/Volumes", n))
		}
	case "linux":
		f, err := os.Open("/proc/mounts")
		if err != nil {
			return "", false
		}
		defer f.Close()
		mounts = fatMounts(f, names)
	}
	for _, m := range mounts {
		if _, err := os.Stat(filepath.Join(m, InfoFile)); err == nil {
			return m, true
		}
	}
	return "", false
}

// fatMounts returns the mount points in the /proc/mounts text r of the FAT
// file systems whose last path element is one of names.
func fatMounts(r io.Reader, names []string) []string {
	var mounts []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 || f[2] != "vfat" && f[2] != "msdos" {
			continue
		}
		// /proc/mounts escapes spaces and other characters as octal.
		dir := unescape(f[1])
		for _, n := range names {
			if filepath.Base(dir) == n {
				mounts = append(mounts, dir)
			}
		}
	}
	return mounts
}

// unescape decodes the \ooo octal escapes of /proc/mounts.
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && isOctal(s[i+1:i+4]) {
			b.WriteByte((s[i+1]-'0')<<6 | (s[i+2]-'0')<<3 | (s[i+3] - '0'))
			i += 3
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isOctal(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '7' {
			return false
		}
	}
	return len(s) == 3
}

// BoardID returns the Board-ID line of the InfoFile on the drive at mount,
// or "" when it has none.
func BoardID(mount string) string {
	data, err := os.ReadFile(filepath.Join(mount, InfoFile))
	if err != nil {
		return ""
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		if id, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("Board-ID:")); ok {
			return string(bytes.TrimSpace(id))
		}
	}
	return ""
}
