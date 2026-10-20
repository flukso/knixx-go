//go:build tinygo

package main

import (
	"io"
	"knixx/cli"
	"knixx/lineedit"
	"knixx/outq"
	"machine"
	"time"
)

const (
	prompt      = ">>> "
	consolePoll = 10 * time.Millisecond
	// outputPoll is the period at which the senders look for queued output.
	outputPoll = 5 * time.Millisecond
)

// port is a terminal the console runs on: USB CDC or the BLE NUS. Output
// goes to its queue, which a sender goroutine drains, so that no writer
// waits for the terminal: the USB CDC write waits for room as long as the
// host does not read, forever once the cable is pulled while a terminal
// was open, and the NUS write for each notification the SoftDevice cannot
// take yet.
type port interface {
	Buffered() int
	ReadByte() (byte, error)
	out() *outq.Queue
}

// usbPort is the console port on USB CDC.
type usbPort struct{}

var usbOut outq.Queue

func (usbPort) Buffered() int           { return machine.Serial.Buffered() }
func (usbPort) ReadByte() (byte, error) { return machine.Serial.ReadByte() }
func (usbPort) out() *outq.Queue        { return &usbOut }

// startOutput starts the senders of the console ports.
func startOutput() {
	go sendOutput(&usbOut, machine.Serial)
	go sendOutput(&nusOutQ, &nusOut)
}

// sendOutput drains q to w. It is the only goroutine that waits for w.
func sendOutput(q *outq.Queue, w io.Writer) {
	for {
		if !q.Send(w) {
			time.Sleep(outputPoll)
		}
	}
}

var (
	// active is the port the console reads and writes. Only the console
	// goroutine changes it, see attach.
	active port = usbPort{}

	editor = lineedit.Editor{Prompt: prompt, Out: activeOut{}}

	// greeted records whether the banner was shown on the active port. It is
	// shown on the first key, so that it is not lost before a terminal is
	// attached.
	greeted bool
)

// activeOut queues raw output for the active port.
type activeOut struct{}

func (activeOut) Write(p []byte) (int, error) { return active.out().Write(p) }

// crlf queues output for the active port, translating "\n" into "\r\n"
// for serial terminals. A write that does not fit the queue is dropped
// whole.
type crlf struct{}

func (crlf) Write(p []byte) (int, error) { return active.out().WriteCRLF(p) }

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
