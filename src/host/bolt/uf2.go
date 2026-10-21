package main

import (
	"errors"
	"fmt"
	"knixx/host/internal/uf2"
	"knixx/host/internal/usbserial"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The Feather nRF52840: the UF2 family of its bootloader and the USB ID of
// the knixx application.
const (
	nrf52840Family = 0xADA52840
	appUSBID       = "239a:802a"
)

// runUF2 flashes the UF2 file over USB, as tinygo flash does for the
// Feather: a 1200 baud touch of the application's serial port resets the
// board into its UF2 bootloader, whose USB drive gets the file. A board
// that is in the bootloader already is flashed as it is. On Linux the drive
// is mounted through udisks when nothing else mounts it.
//
// The bootloader resets the board as soon as it has the last block, while
// the host may still write the drive's file entries, so a write error is
// expected. The flash is checked instead by the board coming back: with
// expect set, its version command must print expect; without, the drive
// must go away.
func runUF2(file, port, volume, expect string, wait time.Duration) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if err := uf2.Check(data, nrf52840Family); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	names := []string{volume}

	var warned bool
	mount, ok := findVolume(names, &warned)
	if ok {
		status("%s is mounted: the board is in its bootloader", mount)
	} else {
		if port == "" {
			if port, err = appPort(); err != nil {
				return err
			}
		}
		status("resetting the board on %s into its bootloader", port)
		if err := usbserial.Reset1200(port); err != nil {
			return err
		}
		time.Sleep(3 * time.Second)
		if mount, err = waitVolume(names, wait, &warned); err != nil {
			return err
		}
	}
	if id := uf2.BoardID(mount); id != "" {
		status("bootloader: %s", id)
	}

	status("writing %s (%d KiB) to %s", filepath.Base(file), len(data)/1024, mount)
	werr := writeFile(filepath.Join(mount, "flash.uf2"), data)
	if werr != nil {
		status("the write ended with %v; the bootloader resets the board after the last block", werr)
	}

	if expect != "" {
		got, err := waitVersion(port, wait)
		if err != nil {
			return err
		}
		if got != expect {
			return fmt.Errorf("the board reports %q, want %q", got, expect)
		}
		status("the board runs the new firmware: %s", got)
		return nil
	}
	if err := waitGone(mount, wait); err != nil {
		if werr != nil {
			return fmt.Errorf("write: %w", werr)
		}
		return err
	}
	status("the board left its bootloader")
	return nil
}

func status(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}

// appPort returns the serial port of the one knixx application on USB.
func appPort() (string, error) {
	ports, err := usbserial.Ports(appUSBID)
	if err != nil {
		return "", err
	}
	switch len(ports) {
	case 0:
		return "", errors.New("no knixx board on USB; double-tap reset for its bootloader, or use -port")
	case 1:
		return ports[0], nil
	}
	return "", fmt.Errorf("several serial ports (%s); pick one with -port", strings.Join(ports, ", "))
}

// findVolume returns the mount point of the bootloader's drive, mounting it
// when no automounter has, as on a Linux desktop without one. A mount that
// fails is reported once, through warned, so that the drive can still be
// mounted by hand while bolt waits.
func findVolume(names []string, warned *bool) (string, bool) {
	if mount, ok := uf2.FindVolume(names); ok {
		return mount, true
	}
	mount, err := uf2.MountVolume(names)
	switch {
	case errors.Is(err, uf2.ErrNoDrive):
		return "", false
	case err != nil:
		if !*warned {
			*warned = true
			status("%v; mount it by hand", err)
		}
		return "", false
	case mount != "":
		status("mounted %s", mount)
	}
	return uf2.FindVolume(names)
}

// waitVolume polls every 500 ms for up to wait for the bootloader's drive.
func waitVolume(names []string, wait time.Duration, warned *bool) (string, error) {
	for deadline := time.Now().Add(wait); ; time.Sleep(500 * time.Millisecond) {
		if mount, ok := findVolume(names, warned); ok {
			return mount, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("no UF2 drive named %s after %v", strings.Join(names, " or "), wait)
		}
	}
}

// writeFile writes data to the file name and closes it, reporting the
// first error.
func writeFile(name string, data []byte) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// waitVersion polls for up to wait for the application's serial port, port
// or the one of the knixx application, and returns the line its version
// command prints. The console starts a moment after the port appears.
func waitVersion(port string, wait time.Duration) (string, error) {
	deadline := time.Now().Add(wait)
	var last error
	for time.Now().Before(deadline) {
		time.Sleep(time.Second)
		p := port
		if p == "" {
			ports, err := usbserial.Ports(appUSBID)
			if err != nil || len(ports) != 1 {
				last = fmt.Errorf("waiting for the board's serial port (%d found)", len(ports))
				continue
			}
			p = ports[0]
		}
		out, err := usbserial.Query(p, "version", time.Second)
		if err != nil {
			last = err
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "version ") {
				return strings.TrimSpace(line), nil
			}
		}
		last = errors.New("no answer to version on the console")
	}
	return "", fmt.Errorf("the board did not come back after %v: %v", wait, last)
}

// waitGone polls for up to wait for the drive at mount to go away.
func waitGone(mount string, wait time.Duration) error {
	for deadline := time.Now().Add(wait); ; time.Sleep(500 * time.Millisecond) {
		if _, err := os.Stat(filepath.Join(mount, uf2.InfoFile)); err != nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the bootloader's drive is still there after %v", wait)
		}
	}
}
