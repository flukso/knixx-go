package main

import (
	"errors"
	"fmt"
	"io"
	"knixx/host/internal/usbserial"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
	"tinygo.org/x/bluetooth"
)

// The console modes of bolt term: USB, BLE, or USB when the board answers
// there and BLE otherwise.
const (
	modeAuto = ""
	modeUSB  = "usb"
	modeBLE  = "ble"
)

// usbProbe is how long bolt term waits for the board to answer a CR on
// USB before it takes the console to be on BLE.
const usbProbe = 500 * time.Millisecond

// termMode returns the mode that the flags of bolt term select: -port
// implies USB and -addr BLE.
func termMode(usb, ble bool, port, addr string) (string, error) {
	switch {
	case usb && ble:
		return "", errors.New("-usb and -ble exclude each other")
	case (usb || port != "") && (ble || addr != ""):
		return "", errors.New("-usb and -port are for USB, -ble and -addr for BLE")
	case usb || port != "":
		return modeUSB, nil
	case ble || addr != "":
		return modeBLE, nil
	}
	return modeAuto, nil
}

// runTerm is a terminal for the console of a knixx board, on its USB serial
// port or on the Nordic UART Service (NUS) that the nus on console command
// starts. Keys go to the board as typed; Ctrl-] quits. Without a mode it
// takes USB when the board answers there: after nus on the USB port stays
// silent, and it then takes BLE. port selects the USB port, name and addr
// the BLE peripheral.
func runTerm(mode, port, name, addr string, wait time.Duration) error {
	switch mode {
	case modeBLE:
		return termBLE(name, addr, wait)
	case modeUSB:
		if port == "" {
			var err error
			if port, err = appPort("use -port"); err != nil {
				return err
			}
		}
		c, err := dialUSB(port)
		if err != nil {
			return err
		}
		defer c.close()
		status("connected to %s, Ctrl-] to quit", port)
		c.start(os.Stdout)
		return session(c, os.Stdin, true)
	}

	ports, err := usbserial.Ports(appUSBID)
	if err != nil {
		return err
	}
	switch len(ports) {
	case 0:
		status("no knixx board on USB, trying BLE")
		return termBLE(name, addr, wait)
	case 1:
	default:
		return fmt.Errorf("several serial ports (%v); pick one with -port, or use -ble", ports)
	}
	c, err := dialUSB(ports[0])
	if err != nil {
		return err
	}
	got, err := c.probe(usbProbe)
	if err != nil {
		c.close()
		return err
	}
	if len(got) == 0 {
		c.close()
		status("no answer on %s, the console is on BLE: trying BLE", ports[0])
		return termBLE(name, addr, wait)
	}
	defer c.close()
	status("connected to %s, Ctrl-] to quit", ports[0])
	os.Stdout.Write(got)
	c.start(os.Stdout)
	return session(c, os.Stdin, false)
}

// conn is the link to the board's console. What the board sends goes to
// the terminal from the conn's own reader.
type conn interface {
	// write sends p to the board, at most chunk bytes.
	write(p []byte) error
	// chunk is the largest write.
	chunk() int
	// gone is closed when the link drops.
	gone() <-chan struct{}
}

// eofWait is how long a session waits for the board's answer after the
// input ended, as with piped input.
var eofWait = time.Second

// session runs the terminal on c with the keys from in, raw when in is a
// terminal. With greet it first sends a CR: the board shows its banner and
// prompt on the first key.
func session(c conn, in io.Reader, greet bool) error {
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fd := int(f.Fd())
		state, err := term.MakeRaw(fd)
		if err != nil {
			return fmt.Errorf("terminal: %w", err)
		}
		defer term.Restore(fd, state)
	}
	if greet {
		if err := c.write([]byte{'\r'}); err != nil {
			return fmt.Errorf("write: %w", err)
		}
	}

	input := newPending()
	go read(in, input)
	for {
		select {
		case <-c.gone():
			fmt.Fprint(os.Stderr, "\r\n[disconnected]\r\n")
			return nil
		case <-input.ready:
			p, eof := input.take()
			out, quit := chunks(p, c.chunk())
			// On BLE, writes with response pace the input to what the
			// board takes, so that a paste does not overflow its receive
			// ring; on USB, its flow control does. What is typed meanwhile
			// goes out with the next write.
			for _, b := range out {
				if err := c.write(b); err != nil {
					return fmt.Errorf("write: %w", err)
				}
			}
			if quit {
				fmt.Fprint(os.Stderr, "\r\n")
				return nil
			}
			if eof {
				// Leave the board time to answer.
				time.Sleep(eofWait)
				return nil
			}
		}
	}
}

// read queues what is read from in, and closes input at the end.
func read(in io.Reader, input *pending) {
	buf := make([]byte, 256)
	for {
		n, err := in.Read(buf)
		if n > 0 {
			input.add(buf[:n])
		}
		if err != nil {
			input.close()
			return
		}
	}
}

// usbChunk is the largest write to the USB serial port.
const usbChunk = 256

// usbConn is the console on the board's USB serial port.
type usbConn struct {
	p    *usbserial.Port
	done chan struct{}
	once sync.Once
}

func dialUSB(name string) (*usbConn, error) {
	p, err := usbserial.Open(name, 115200)
	if err != nil {
		return nil, err
	}
	if err := p.Exclusive(); err != nil {
		p.Close()
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return &usbConn{p: p, done: make(chan struct{})}, nil
}

// probe sends a CR and returns what the board answers within d.
func (c *usbConn) probe(d time.Duration) ([]byte, error) {
	if _, err := c.p.Write([]byte{'\r'}); err != nil {
		return nil, err
	}
	var got []byte
	buf := make([]byte, 256)
	c.p.SetReadDeadline(time.Now().Add(d))
	defer c.p.SetReadDeadline(time.Time{})
	for {
		n, err := c.p.Read(buf)
		got = append(got, buf[:n]...)
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return got, nil
		}
		if err != nil {
			return got, err
		}
	}
}

// start copies what the board sends to out until the port fails, as when
// the board is unplugged or resets.
func (c *usbConn) start(out io.Writer) {
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := c.p.Read(buf)
			out.Write(buf[:n])
			if err != nil {
				c.once.Do(func() { close(c.done) })
				return
			}
		}
	}()
}

func (c *usbConn) write(p []byte) error {
	_, err := c.p.Write(p)
	return err
}

func (c *usbConn) chunk() int            { return usbChunk }
func (c *usbConn) gone() <-chan struct{} { return c.done }
func (c *usbConn) close()                { c.p.Close() }

// bleConn is the console on the board's NUS service.
type bleConn struct {
	rx   bluetooth.DeviceCharacteristic
	size int
	done chan struct{}
}

func (c *bleConn) write(p []byte) error {
	_, err := c.rx.Write(p)
	return err
}

func (c *bleConn) chunk() int            { return c.size }
func (c *bleConn) gone() <-chan struct{} { return c.done }

// termBLE runs the terminal on the BLE console of the board named name, or
// with address addr.
func termBLE(name, addr string, wait time.Duration) error {
	adapter, found, err := find(name, addr, wait, "nus on")
	if err != nil {
		return err
	}

	// The handler sees the connections of every device on the adapter.
	c := &bleConn{size: defaultChunk, done: make(chan struct{})}
	var once sync.Once
	adapter.SetConnectHandler(func(d bluetooth.Device, connected bool) {
		if !connected && d.Address.String() == found.Address.String() {
			once.Do(func() { close(c.done) })
		}
	})

	status("connecting to %s", found.Address)
	dev, err := adapter.Connect(found.Address, bluetooth.ConnectionParams{})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer dev.Disconnect()

	rx, tx, err := openNUS(dev)
	if err != nil {
		return err
	}
	c.rx = rx
	if mtu, err := rx.GetMTU(); err == nil && mtu > 3 {
		c.size = int(mtu) - 3
	}
	if err := tx.EnableNotifications(func(buf []byte) { os.Stdout.Write(buf) }); err != nil {
		return fmt.Errorf("TX notifications: %w", err)
	}
	status("connected, Ctrl-] to quit")
	return session(c, os.Stdin, true)
}

// openNUS discovers the NUS service of dev and returns its RX characteristic,
// written by the central, and its TX characteristic, notified by the board.
func openNUS(dev bluetooth.Device) (rx, tx bluetooth.DeviceCharacteristic, err error) {
	svcs, err := dev.DiscoverServices([]bluetooth.UUID{bluetooth.ServiceUUIDNordicUART})
	if err != nil || len(svcs) == 0 {
		return rx, tx, fmt.Errorf("no NUS service: %v", err)
	}
	chars, err := svcs[0].DiscoverCharacteristics([]bluetooth.UUID{
		bluetooth.CharacteristicUUIDUARTRX, bluetooth.CharacteristicUUIDUARTTX,
	})
	if err != nil {
		return rx, tx, fmt.Errorf("NUS characteristics: %w", err)
	}
	var haveRX, haveTX bool
	for _, c := range chars {
		switch c.UUID() {
		case bluetooth.CharacteristicUUIDUARTRX:
			rx, haveRX = c, true
		case bluetooth.CharacteristicUUIDUARTTX:
			tx, haveTX = c, true
		}
	}
	if !haveRX || !haveTX {
		return rx, tx, errors.New("NUS service lacks its RX or TX characteristic")
	}
	return rx, tx, nil
}
