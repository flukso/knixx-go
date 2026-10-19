// Package nus holds the transport logic of the console on the Nordic UART
// Service (NUS) of BLE: the receive ring that the BLE event interrupt fills,
// the writer that splits console output into notifications, and the idle
// timer that gives the console back to USB when no central connects.
package nus

import (
	"io"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// RingLen is the capacity of Ring. It holds several writes of a central,
// each at most 20 bytes with the default ATT MTU.
const RingLen = 256

// Ring is a single-producer, single-consumer byte queue. Put may be called
// from an interrupt handler, Buffered and ReadByte from one goroutine.
type Ring struct {
	buf [RingLen]byte
	// head counts the bytes read, tail the bytes written; both wrap.
	head, tail atomic.Uint32
}

// Put queues p and reports how much of it fit. It does not allocate.
func (r *Ring) Put(p []byte) int {
	head, tail := r.head.Load(), r.tail.Load()
	n := 0
	for _, b := range p {
		if tail-head == RingLen {
			break
		}
		r.buf[tail%RingLen] = b
		tail++
		n++
	}
	r.tail.Store(tail)
	return n
}

// Buffered returns the number of bytes queued.
func (r *Ring) Buffered() int {
	return int(r.tail.Load() - r.head.Load())
}

// ReadByte returns the oldest queued byte, or io.EOF when there is none.
func (r *Ring) ReadByte() (byte, error) {
	head := r.head.Load()
	if head == r.tail.Load() {
		return 0, io.EOF
	}
	b := r.buf[head%RingLen]
	r.head.Store(head + 1)
	return b, nil
}

// Reset drops the queued bytes. It must not race with Put.
func (r *Ring) Reset() {
	r.head.Store(r.tail.Load())
}

// Chunk is the payload of a notification with the default ATT MTU of 23.
const Chunk = 20

const (
	// retryDelay separates the attempts to queue a notification while the
	// SoftDevice has no free transmit buffer.
	retryDelay = 5 * time.Millisecond
	// retryLimit bounds the wait for a transmit buffer per notification.
	retryLimit = time.Second
)

// Writer sends console output as notifications of at most Chunk bytes. It
// never blocks for long: output is dropped while no central is connected,
// and the rest of a write once a notification could not be queued within
// retryLimit.
type Writer struct {
	// Send queues one notification.
	Send func(p []byte) error
	// Connected reports whether a central is connected.
	Connected func() bool
	// Sleep waits for d; it defaults to time.Sleep.
	Sleep func(d time.Duration)
}

// Write sends p and always reports it as written: console output is best
// effort.
func (w *Writer) Write(p []byte) (int, error) {
	for rest := p; len(rest) > 0 && w.Connected(); {
		n := split(rest)
		if !w.send(rest[:n]) {
			break
		}
		rest = rest[n:]
	}
	return len(p), nil
}

// split returns the length of the next notification of p: at most Chunk
// bytes, ending before a UTF-8 sequence that would not fit whole, so that a
// central that decodes each notification by itself sees whole characters.
func split(p []byte) int {
	if len(p) <= Chunk {
		return len(p)
	}
	n := Chunk
	// Back off over continuation bytes to the start of the sequence that
	// p[Chunk] continues.
	for n > Chunk-utf8.UTFMax && p[n]&0xc0 == 0x80 {
		n--
	}
	if p[n]&0xc0 == 0x80 {
		// Not UTF-8; split anywhere.
		return Chunk
	}
	return n
}

// send queues chunk, retrying while the transmit buffers are full.
func (w *Writer) send(chunk []byte) bool {
	sleep := w.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	for waited := time.Duration(0); ; waited += retryDelay {
		if w.Send(chunk) == nil {
			return true
		}
		if waited >= retryLimit || !w.Connected() {
			return false
		}
		sleep(retryDelay)
	}
}

// IdleTimeout is how long NUS waits for a central before the console goes
// back to USB.
const IdleTimeout = 60 * time.Second

// Timer is the idle timer: it runs while no central is connected.
type Timer struct {
	armed    bool
	deadline time.Time
}

// Arm starts the timer at now.
func (t *Timer) Arm(now time.Time) {
	t.armed, t.deadline = true, now.Add(IdleTimeout)
}

// Disarm stops the timer.
func (t *Timer) Disarm() {
	t.armed = false
}

// Expired reports whether the armed timer has run out at now.
func (t *Timer) Expired(now time.Time) bool {
	return t.armed && !now.Before(t.deadline)
}

// Left returns the time left at now, or 0 when the timer is not armed.
func (t *Timer) Left(now time.Time) time.Duration {
	if !t.armed {
		return 0
	}
	return max(t.deadline.Sub(now), 0)
}
