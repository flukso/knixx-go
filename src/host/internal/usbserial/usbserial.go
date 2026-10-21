// Package usbserial finds and drives the USB CDC serial port of a board:
// the 1200 baud touch that resets it into its bootloader, a console
// command and its output, and the raw port for a terminal. It uses termios through golang.org/x/sys/unix,
// on Linux and macOS.
package usbserial

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Ports returns the USB serial ports of the boards with one of the USB IDs
// vidpid, as "vvvv:pppp" in hex. On Linux the IDs come from sysfs; on macOS,
// which has them only in IOKit, every /dev/cu.usbmodem* port is returned.
func Ports(vidpid ...string) ([]string, error) {
	switch runtime.GOOS {
	case "linux":
		return sysfsPorts("/", vidpid)
	case "darwin":
		return filepath.Glob("/dev/cu.usbmodem*")
	}
	return nil, fmt.Errorf("usbserial: %s is not supported", runtime.GOOS)
}

// sysfsPorts returns the /dev/ttyACM* ports under root whose USB device has
// one of the IDs vidpid.
func sysfsPorts(root string, vidpid []string) ([]string, error) {
	ttys, err := filepath.Glob(filepath.Join(root, "sys/class/tty/ttyACM*"))
	if err != nil {
		return nil, err
	}
	var ports []string
	for _, tty := range ttys {
		// device is the USB interface; its parent the USB device.
		intf, err := filepath.EvalSymlinks(filepath.Join(tty, "device"))
		if err != nil {
			continue
		}
		vid, err1 := os.ReadFile(filepath.Join(intf, "..", "idVendor"))
		pid, err2 := os.ReadFile(filepath.Join(intf, "..", "idProduct"))
		if err1 != nil || err2 != nil {
			continue
		}
		id := strings.TrimSpace(string(vid)) + ":" + strings.TrimSpace(string(pid))
		for _, want := range vidpid {
			if strings.EqualFold(id, want) {
				ports = append(ports, filepath.Join("/dev", filepath.Base(tty)))
				break
			}
		}
	}
	return ports, nil
}

// Port is an open serial port, read and written as its file, which takes
// read deadlines.
type Port struct{ *os.File }

// Open opens the serial port name in raw mode at baud. O_NONBLOCK keeps the
// open from waiting for carrier and lets reads take a deadline.
func Open(name string, baud int) (*Port, error) {
	f, err := os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	p := &Port{f}
	if err := p.control(func(fd int) error { return setRaw(fd, baud) }); err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return p, nil
}

// control runs fn on the file descriptor without taking it out of Go's
// poller, which f.Fd would.
func (p *Port) control(fn func(fd int) error) error {
	rc, err := p.SyscallConn()
	if err != nil {
		return err
	}
	var ferr error
	if err := rc.Control(func(fd uintptr) { ferr = fn(int(fd)) }); err != nil {
		return err
	}
	return ferr
}

// Exclusive keeps others from opening the port while p is open: their open
// fails with EBUSY, instead of taking part of the board's output.
func (p *Port) Exclusive() error {
	return p.control(func(fd int) error { return unix.IoctlSetInt(fd, unix.TIOCEXCL, 0) })
}

// clearDTR drops DTR.
func (p *Port) clearDTR() error {
	return p.control(func(fd int) error {
		return unix.IoctlSetPointerInt(fd, unix.TIOCMBIC, unix.TIOCM_DTR)
	})
}

// raw makes t raw, 8N1, ignoring the modem lines, as cfmakeraw does.
func raw(t *unix.Termios) {
	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag &^= unix.CSIZE | unix.PARENB
	t.Cflag |= unix.CS8 | unix.CREAD | unix.CLOCAL
	t.Cc[unix.VMIN] = 1
	t.Cc[unix.VTIME] = 0
}

// Reset1200 opens port at 1200 baud and drops DTR, which makes a board with
// an Arduino-style USB stack reset into its bootloader, then closes it. It
// tries 3 times, 1 s apart, as TinyGo does.
func Reset1200(name string) error {
	var err error
	for try := 0; try < 3; try++ {
		if try > 0 {
			time.Sleep(time.Second)
		}
		var p *Port
		if p, err = Open(name, 1200); err != nil {
			continue
		}
		err = p.clearDTR()
		p.Close()
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("usbserial: 1200 baud reset of %s: %w", name, err)
}

// ansi matches the escape sequences of a terminal.
var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// Query sends a CR, to start the console, and then cmd to the console on
// port name at 115200 baud, and returns what came back within wait, with
// the escape sequences and CRs removed.
func Query(name, cmd string, wait time.Duration) (string, error) {
	p, err := Open(name, 115200)
	if err != nil {
		return "", err
	}
	defer p.Close()

	var out []byte
	read := func(d time.Duration) error {
		p.SetReadDeadline(time.Now().Add(d))
		buf := make([]byte, 256)
		for {
			n, err := p.Read(buf)
			out = append(out, buf[:n]...)
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return nil
			}
			if err != nil {
				return err
			}
		}
	}
	if _, err := p.Write([]byte("\r")); err != nil {
		return "", err
	}
	if err := read(300 * time.Millisecond); err != nil {
		return "", err
	}
	if _, err := p.Write([]byte(cmd + "\r")); err != nil {
		return "", err
	}
	if err := read(wait); err != nil {
		return "", err
	}
	s := ansi.ReplaceAllString(string(out), "")
	return strings.ReplaceAll(s, "\r", ""), nil
}
