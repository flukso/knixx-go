package main

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeConn records the writes of a session.
type fakeConn struct {
	mu     sync.Mutex
	writes [][]byte
	size   int
	done   chan struct{}
}

func newFakeConn(size int) *fakeConn { return &fakeConn{size: size, done: make(chan struct{})} }

func (c *fakeConn) write(p []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, bytes.Clone(p))
	return nil
}

func (c *fakeConn) chunk() int            { return c.size }
func (c *fakeConn) gone() <-chan struct{} { return c.done }

func (c *fakeConn) sent() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(bytes.Join(c.writes, nil))
}

func (c *fakeConn) maxWrite() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, w := range c.writes {
		n = max(n, len(w))
	}
	return n
}

func init() { eofWait = 10 * time.Millisecond }

// runSession runs a session and fails when it does not end within 2 s.
func runSession(t *testing.T, c conn, in io.Reader, greet bool) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- session(c, in, greet) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the session did not end")
	}
}

func TestSessionEOF(t *testing.T) {
	c := newFakeConn(4)
	runSession(t, c, strings.NewReader("version\r"), true)
	if got := c.sent(); got != "\rversion\r" {
		t.Errorf("sent %q, want the greeting CR and the line", got)
	}
	if n := c.maxWrite(); n > 4 {
		t.Errorf("a write of %d bytes, chunk is 4", n)
	}
}

func TestSessionNoGreet(t *testing.T) {
	c := newFakeConn(usbChunk)
	runSession(t, c, strings.NewReader("sys\r"), false)
	if got := c.sent(); got != "sys\r" {
		t.Errorf("sent %q", got)
	}
}

func TestSessionEscape(t *testing.T) {
	// The input stays open, so only Ctrl-] ends the session.
	r, w := io.Pipe()
	defer w.Close()
	go w.Write([]byte("ab\x1dcd"))
	c := newFakeConn(usbChunk)
	runSession(t, c, r, false)
	if got := c.sent(); got != "ab" {
		t.Errorf("sent %q, want the bytes before Ctrl-]", got)
	}
}

func TestSessionGone(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	c := newFakeConn(usbChunk)
	close(c.done)
	runSession(t, c, r, false)
}

func TestTermMode(t *testing.T) {
	for _, tc := range []struct {
		usb, ble   bool
		port, addr string
		want       string
		err        bool
	}{
		{want: modeAuto},
		{usb: true, want: modeUSB},
		{ble: true, want: modeBLE},
		{port: "/dev/ttyACM0", want: modeUSB},
		{addr: "AA:BB", want: modeBLE},
		{usb: true, port: "/dev/ttyACM0", want: modeUSB},
		{ble: true, addr: "AA:BB", want: modeBLE},
		{usb: true, ble: true, err: true},
		{port: "/dev/ttyACM0", ble: true, err: true},
		{port: "/dev/ttyACM0", addr: "AA:BB", err: true},
		{usb: true, addr: "AA:BB", err: true},
	} {
		got, err := termMode(tc.usb, tc.ble, tc.port, tc.addr)
		if (err != nil) != tc.err || got != tc.want {
			t.Errorf("termMode(%v, %v, %q, %q) = %q, %v", tc.usb, tc.ble, tc.port, tc.addr, got, err)
		}
	}
}
