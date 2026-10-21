package usbserial

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// bauds maps the rates used here to their termios constants.
var bauds = map[int]uint32{1200: unix.B1200, 115200: unix.B115200}

// setRaw makes the terminal fd raw at baud.
func setRaw(fd, baud int) error {
	b, ok := bauds[baud]
	if !ok {
		return fmt.Errorf("unsupported baud rate %d", baud)
	}
	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return err
	}
	raw(t)
	t.Cflag &^= unix.CBAUD
	t.Cflag |= b
	t.Ispeed, t.Ospeed = b, b
	return unix.IoctlSetTermios(fd, unix.TCSETS, t)
}
