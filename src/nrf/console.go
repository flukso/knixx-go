//go:build tinygo

package main

import (
	"knixx/cli"
	"machine"
	"time"
)

const (
	prompt       = ">>> "
	consolePoll  = 10 * time.Millisecond
	maxLineLen   = 80
	keyBackspace = 0x08
	keyDelete    = 0x7f
)

// crlf translates "\n" into "\r\n" for serial terminals.
type crlf struct{}

func (crlf) Write(p []byte) (int, error) {
	start := 0
	for i, b := range p {
		if b == '\n' {
			machine.Serial.Write(p[start:i])
			machine.Serial.Write([]byte{'\r', '\n'})
			start = i + 1
		}
	}
	machine.Serial.Write(p[start:])
	return len(p), nil
}

var console crlf

// runConsole reads command lines from the USB CDC port and executes them.
// The banner is printed once the first key arrives, so that it is not lost
// before a terminal is attached.
func runConsole(cmds []cli.Cmd, banner []byte) {
	var line [maxLineLen]byte
	n := 0
	greeted := false
	lastCR := false

	for {
		if machine.Serial.Buffered() == 0 {
			time.Sleep(consolePoll)
			continue
		}
		b, err := machine.Serial.ReadByte()
		if err != nil {
			continue
		}
		if !greeted {
			greeted = true
			console.Write(banner)
			console.Write([]byte(prompt))
			if b == '\r' || b == '\n' {
				lastCR = b == '\r'
				continue
			}
		}

		switch {
		case b == '\n' && lastCR:
			// second half of a CR LF pair
		case b == '\r' || b == '\n':
			console.Write([]byte{'\n'})
			cli.Exec(cmds, string(line[:n]), console)
			n = 0
			console.Write([]byte(prompt))
		case b == keyBackspace || b == keyDelete:
			if n > 0 {
				n--
				machine.Serial.Write([]byte{'\b', ' ', '\b'})
			}
		case b >= ' ' && b < keyDelete && n < len(line):
			line[n] = b
			n++
			machine.Serial.WriteByte(b)
		}
		lastCR = b == '\r'
	}
}
