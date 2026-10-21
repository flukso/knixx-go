// Command nusterm is a terminal for the BLE console of a knixx board: it
// connects to the Nordic UART Service (NUS) that the nus on console command
// starts, with the computer's own Bluetooth adapter. It runs on Linux
// (BlueZ) and macOS (CoreBluetooth) through tinygo.org/x/bluetooth.
//
// Keys go to the board as typed, as on the USB console; Ctrl-] quits.
package main

import (
	"errors"
	"flag"
	"fmt"
	"knixx/host/internal/blescan"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
	"tinygo.org/x/bluetooth"
)

func main() {
	name := flag.String("name", "knixx", "advertised name of the board")
	addr := flag.String("addr", "", "address of the board, instead of matching -name: the MAC address on Linux, the device UUID on macOS")
	wait := flag.Duration("wait", 30*time.Second, "how long to wait for the board")
	flag.Parse()

	if err := run(*name, *addr, *wait); err != nil {
		fmt.Fprintln(os.Stderr, "nusterm:", err)
		os.Exit(1)
	}
}

func run(name, addr string, wait time.Duration) error {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return fmt.Errorf("bluetooth: %w", err)
	}

	target := name
	if addr != "" {
		target = addr
	}
	fmt.Fprintf(os.Stderr, "waiting for %s (run nus on on the knixx console)...\n", target)
	found, err := blescan.Find(adapter, name, addr, wait)
	if err != nil {
		return err
	}

	// The handler sees the connections of every device on the adapter.
	gone := make(chan struct{})
	var once sync.Once
	adapter.SetConnectHandler(func(d bluetooth.Device, connected bool) {
		if !connected && d.Address.String() == found.Address.String() {
			once.Do(func() { close(gone) })
		}
	})

	fmt.Fprintf(os.Stderr, "connecting to %s\n", found.Address)
	dev, err := adapter.Connect(found.Address, bluetooth.ConnectionParams{})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer dev.Disconnect()

	rx, tx, err := open(dev)
	if err != nil {
		return err
	}
	size := defaultChunk
	if mtu, err := rx.GetMTU(); err == nil && mtu > 3 {
		size = int(mtu) - 3
	}
	if err := tx.EnableNotifications(func(buf []byte) { os.Stdout.Write(buf) }); err != nil {
		return fmt.Errorf("TX notifications: %w", err)
	}

	fmt.Fprintln(os.Stderr, "connected, Ctrl-] to quit")
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		state, err := term.MakeRaw(fd)
		if err != nil {
			return fmt.Errorf("terminal: %w", err)
		}
		defer term.Restore(fd, state)
	}

	// The board shows its banner and prompt on the first key.
	if _, err := rx.Write([]byte{'\r'}); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	input := newPending()
	go read(input)
	for {
		select {
		case <-gone:
			fmt.Fprint(os.Stderr, "\r\n[disconnected]\r\n")
			return nil
		case <-input.ready:
			p, eof := input.take()
			out, quit := chunks(p, size)
			// Writes with response pace the input to what the board
			// takes, so that a paste does not overflow its receive ring.
			// What is typed meanwhile goes out with the next write.
			for _, c := range out {
				if _, err := rx.Write(c); err != nil {
					return fmt.Errorf("write: %w", err)
				}
			}
			if quit {
				fmt.Fprint(os.Stderr, "\r\n")
				return nil
			}
			if eof {
				// stdin ended, as with piped input: leave the board time
				// to answer.
				time.Sleep(time.Second)
				return nil
			}
		}
	}
}

// read queues what is read from stdin in input, and closes it at the end.
func read(input *pending) {
	buf := make([]byte, 256)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			input.add(buf[:n])
		}
		if err != nil {
			input.close()
			return
		}
	}
}

// open discovers the NUS service of dev and returns its RX characteristic,
// written by the central, and its TX characteristic, notified by the board.
func open(dev bluetooth.Device) (rx, tx bluetooth.DeviceCharacteristic, err error) {
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
