// Package lineedit is a minimal line editor for serial consoles: echo,
// backspace and a command history browsed with the up and down keys.
package lineedit

import (
	"bytes"
	"io"
)

const (
	MaxLine    = 80
	HistoryLen = 8
	// MaxPrompt is the longest prompt redrawn without allocating.
	MaxPrompt = 16

	keyCtrlN     = 0x0e
	keyCtrlP     = 0x10
	keyBackspace = 0x08
	keyEscape    = 0x1b
	keyDelete    = 0x7f
)

// Escape sequence decoder states.
const (
	escNone  uint8 = iota
	escStart       // after ESC
	escCSI         // after ESC [ or ESC O
	escParam       // after a parameter byte; the final byte is ignored
)

// Editor turns terminal input into lines. Its zero value with Prompt and Out
// set is ready to use. All storage is fixed size.
type Editor struct {
	Prompt string
	Out    io.Writer // raw terminal output for echo and redraws

	line [MaxLine]byte
	n    uint8

	// draft holds the line being typed while browsing the history.
	draft  [MaxLine]byte
	draftN uint8

	// hist is a ring of the most recent lines; the newest is at head-1.
	hist        [HistoryLen][MaxLine]byte
	histN       [HistoryLen]uint8
	head, count uint8
	// browse is 0 while editing the draft and k when showing the k-th
	// newest history entry.
	browse uint8

	esc    uint8
	lastCR bool

	// redraw assembles a redrawn line so that it is written at once.
	redraw [1 + MaxPrompt + MaxLine + 3]byte
}

// Feed processes one input byte. It returns the completed line when b ends
// one; the slice is valid until the next call to Feed.
func (e *Editor) Feed(b byte) (line []byte, done bool) {
	lastCR := e.lastCR
	e.lastCR = b == '\r'

	switch e.esc {
	case escStart:
		if b == '[' || b == 'O' {
			e.esc = escCSI
		} else {
			e.esc = escNone
		}
		return nil, false
	case escCSI, escParam:
		switch {
		case b >= 0x30 && b <= 0x3f:
			e.esc = escParam
		case b >= 0x40 && b <= 0x7e:
			if e.esc == escCSI {
				switch b {
				case 'A':
					e.up()
				case 'B':
					e.down()
				}
			}
			e.esc = escNone
		default:
			e.esc = escNone
		}
		return nil, false
	}

	switch {
	case b == keyEscape:
		e.esc = escStart
	case b == '\n' && lastCR:
		// second half of a CR LF pair
	case b == '\r' || b == '\n':
		return e.enter(), true
	case b == keyCtrlP:
		e.up()
	case b == keyCtrlN:
		e.down()
	case b == keyBackspace || b == keyDelete:
		if e.n > 0 {
			e.n--
			e.Out.Write([]byte{'\b', ' ', '\b'})
		}
	case b >= ' ' && b < keyDelete && int(e.n) < len(e.line):
		e.line[e.n] = b
		e.n++
		e.Out.Write([]byte{b})
	}
	return nil, false
}

// enter records the current line in the history and hands it out.
func (e *Editor) enter() []byte {
	line := e.line[:e.n]
	e.n = 0
	e.browse = 0
	if len(bytes.TrimSpace(line)) > 0 && (e.count == 0 || !bytes.Equal(line, e.entry(1))) {
		e.histN[e.head] = uint8(copy(e.hist[e.head][:], line))
		e.head = (e.head + 1) % HistoryLen
		if e.count < HistoryLen {
			e.count++
		}
	}
	return line
}

// entry returns the k-th newest history line, 1 being the newest.
func (e *Editor) entry(k uint8) []byte {
	i := (e.head + HistoryLen - k) % HistoryLen
	return e.hist[i][:e.histN[i]]
}

func (e *Editor) up() {
	if e.browse == e.count {
		return
	}
	if e.browse == 0 {
		e.draftN = uint8(copy(e.draft[:], e.line[:e.n]))
	}
	e.browse++
	e.show(e.entry(e.browse))
}

func (e *Editor) down() {
	if e.browse == 0 {
		return
	}
	e.browse--
	if e.browse == 0 {
		e.show(e.draft[:e.draftN])
	} else {
		e.show(e.entry(e.browse))
	}
}

// show replaces the line being edited with s and redraws it. The redraw is
// handed to Out in a single write, so that a USB serial port sends it in as
// few packets as possible instead of a burst of small ones.
func (e *Editor) show(s []byte) {
	e.n = uint8(copy(e.line[:], s))
	b := append(e.redraw[:0], '\r')
	b = append(b, e.Prompt...)
	b = append(b, e.line[:e.n]...)
	b = append(b, "\x1b[K"...)
	e.Out.Write(b)
}
