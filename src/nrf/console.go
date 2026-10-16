//go:build tinygo

package main

import (
	"knixx/cli"
	"knixx/lineedit"
	"machine"
	"time"
)

const (
	prompt      = ">>> "
	consolePoll = 10 * time.Millisecond
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
	ed := lineedit.Editor{Prompt: prompt, Out: machine.Serial}
	greeted := false

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
				// Let the editor see the CR so that a following LF is
				// taken as part of the pair; the empty line is dropped.
				ed.Feed(b)
				continue
			}
		}

		if line, done := ed.Feed(b); done {
			console.Write([]byte{'\n'})
			cli.Exec(cmds, string(line), console)
			console.Write([]byte(prompt))
		}
	}
}
