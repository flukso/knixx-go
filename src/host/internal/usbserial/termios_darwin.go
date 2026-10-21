package usbserial

import "golang.org/x/sys/unix"

// setRaw makes the terminal fd raw at baud.
func setRaw(fd, baud int) error {
	t, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return err
	}
	raw(t)
	t.Ispeed, t.Ospeed = uint64(baud), uint64(baud)
	return unix.IoctlSetTermios(fd, unix.TIOCSETA, t)
}
