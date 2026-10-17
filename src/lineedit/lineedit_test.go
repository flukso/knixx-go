package lineedit

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

const (
	up    = "\x1b[A"
	down  = "\x1b[B"
	left  = "\x1b[D"
	right = "\x1b[C"
	del   = "\x1b[3~"
)

func newEditor() (*Editor, *bytes.Buffer) {
	var out bytes.Buffer
	return &Editor{Prompt: "> ", Out: &out}, &out
}

// feed sends input to e and returns the lines it completed.
func feed(e *Editor, input string) []string {
	var lines []string
	for i := 0; i < len(input); i++ {
		if line, done := e.Feed(input[i]); done {
			lines = append(lines, string(line))
		}
	}
	return lines
}

// current returns the line being edited, without ending it.
func current(e *Editor) string {
	return string(e.line[:e.n])
}

func TestHistoryBrowse(t *testing.T) {
	e, out := newEditor()
	if got := feed(e, "switch A on\rstat\r"); strings.Join(got, "|") != "switch A on|stat" {
		t.Fatalf("lines %q", got)
	}
	feed(e, "con")

	steps := []struct {
		key, want string
	}{
		{up, "stat"},
		{up, "switch A on"},
		{up, "switch A on"},
		{down, "stat"},
		{down, "con"},
		{down, "con"},
	}
	for i, s := range steps {
		out.Reset()
		feed(e, s.key)
		if got := current(e); got != s.want {
			t.Errorf("step %d: line %q, want %q", i, got, s.want)
		}
	}

	out.Reset()
	feed(e, up)
	if want := "\r> stat\x1b[K"; out.String() != want {
		t.Errorf("redraw %q, want %q", out.String(), want)
	}
	if got := feed(e, "\r"); len(got) != 1 || got[0] != "stat" {
		t.Errorf("recalled line %q", got)
	}
}

func TestApplicationCursorKeys(t *testing.T) {
	e, _ := newEditor()
	feed(e, "stat\r\x1bOA")
	if got := current(e); got != "stat" {
		t.Errorf("line %q", got)
	}
	feed(e, "\x1bOB")
	if got := current(e); got != "" {
		t.Errorf("line %q", got)
	}
}

func TestCtrlPN(t *testing.T) {
	e, _ := newEditor()
	feed(e, "stat\r\x10")
	if got := current(e); got != "stat" {
		t.Errorf("Ctrl-P: line %q", got)
	}
	feed(e, "\x0e")
	if got := current(e); got != "" {
		t.Errorf("Ctrl-N: line %q", got)
	}
}

func TestOtherEscapesIgnored(t *testing.T) {
	e, out := newEditor()
	got := feed(e, "st"+left+right+del+"\x1b[1;5A"+"at\r")
	if len(got) != 1 || got[0] != "stat" {
		t.Errorf("lines %q", got)
	}
	if out.String() != "stat" {
		t.Errorf("echo %q", out.String())
	}
}

func TestHistoryFilter(t *testing.T) {
	e, _ := newEditor()
	feed(e, "stat\rstat\r\r   \rsniff on\rstat\r")
	var got []string
	for k := uint8(1); k <= e.count; k++ {
		got = append(got, string(e.entry(k)))
	}
	if want := "stat|sniff on|stat"; strings.Join(got, "|") != want {
		t.Errorf("history %q, want %q", got, want)
	}
}

func TestHistoryWraps(t *testing.T) {
	e, _ := newEditor()
	for i := 0; i < HistoryLen+3; i++ {
		feed(e, fmt.Sprintf("cmd %d\r", i))
	}
	for i := 0; i < HistoryLen+2; i++ {
		feed(e, up)
	}
	if got, want := current(e), "cmd 3"; got != want {
		t.Errorf("oldest %q, want %q", got, want)
	}
}

func TestEditRecalled(t *testing.T) {
	e, _ := newEditor()
	feed(e, "switch A on\r"+up+"\x7f\x7foff\r")
	feed(e, up+up)
	if got := current(e); got != "switch A on" {
		t.Errorf("history entry changed: %q", got)
	}
	feed(e, down)
	if got := current(e); got != "switch A off" {
		t.Errorf("newest %q", got)
	}
}

func TestLineBasics(t *testing.T) {
	e, out := newEditor()
	got := feed(e, "a\r\nb\ncx\bd\r")
	if want := "a|b|cd"; strings.Join(got, "|") != want {
		t.Errorf("lines %q, want %q", got, want)
	}
	if !strings.Contains(out.String(), "x\b \b") {
		t.Errorf("backspace echo %q", out.String())
	}

	got = feed(e, strings.Repeat("y", MaxLine+5)+"\r")
	if len(got) != 1 || len(got[0]) != MaxLine {
		t.Errorf("long line %d bytes, want %d", len(got[0]), MaxLine)
	}
}

// countingWriter counts the calls to Write.
type countingWriter struct {
	bytes.Buffer
	writes int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	c.writes++
	return c.Buffer.Write(p)
}

func TestRedrawSingleWrite(t *testing.T) {
	var out countingWriter
	e := &Editor{Prompt: ">>> ", Out: &out}
	feed(e, "switch A on\r")
	out.Reset()
	out.writes = 0
	feed(e, up)
	if out.writes != 1 {
		t.Errorf("redraw took %d writes, want 1", out.writes)
	}
	if want := "\r>>> switch A on\x1b[K"; out.String() != want {
		t.Errorf("redraw %q, want %q", out.String(), want)
	}
	e.Out = io.Discard
	if allocs := testing.AllocsPerRun(100, func() { feed(e, up+down) }); allocs != 0 {
		t.Errorf("redraw allocates %.0f times per run", allocs)
	}
}
