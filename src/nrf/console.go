//go:build tinygo

package main

import (
	"io"
	"knixx/cli"
	"knixx/lineedit"
	"machine"
	"time"
)

const (
	prompt      = ">>> "
	consolePoll = 10 * time.Millisecond
)

// port is a terminal the console runs on: USB CDC or the BLE NUS.
type port interface {
	Buffered() int
	ReadByte() (byte, error)
	io.Writer
}

var (
	// active is the port the console reads and writes. Only the console
	// goroutine changes it, see attach.
	active port = machine.Serial

	editor = lineedit.Editor{Prompt: prompt, Out: activeOut{}}

	// greeted records whether the banner was shown on the active port. It is
	// shown on the first key, so that it is not lost before a terminal is
	// attached.
	greeted bool
)

// activeOut writes raw output to the active port.
type activeOut struct{}

func (activeOut) Write(p []byte) (int, error) { return active.Write(p) }

// crlf translates "\n" into "\r\n" for serial terminals and writes to the
// active port.
type crlf struct{}

func (crlf) Write(p []byte) (int, error) {
	start := 0
	for i, b := range p {
		if b == '\n' {
			active.Write(p[start:i])
			active.Write([]byte{'\r', '\n'})
			start = i + 1
		}
	}
	active.Write(p[start:])
	return len(p), nil
}

var console crlf

// attach moves the console to p, dropping the line being typed. The banner
// is shown again on the first key unless greet is false.
func attach(p port, greet bool) {
	active = p
	editor.Discard()
	greeted = !greet
}

// runConsole reads command lines from the active port and executes them.
func runConsole(cmds []cli.Cmd, banner []byte) {
	for {
		pollNUS()
		pollSwitchNUS()
		if active.Buffered() == 0 {
			time.Sleep(consolePoll)
			continue
		}
		b, err := active.ReadByte()
		if err != nil {
			continue
		}
		if !greeted {
			greeted = true
			console.Write(banner)
			console.Write([]byte(prompt))
			if b == '\r' || b == '\n' {
				// Let the editor see the CR so that a following LF is
				// taken as part of the pair; the empty line is dropped.
				editor.Feed(b)
				continue
			}
		}

		if line, done := editor.Feed(b); done {
			console.Write([]byte{'\n'})
			cli.Exec(cmds, string(line), console)
			console.Write([]byte(prompt))
		}
	}
}
