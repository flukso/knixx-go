// Package outq queues console output, so that writers never wait for a
// terminal: a slow, absent or unplugged one only loses output. One sender
// goroutine per port drains the queue to the port and is the only one that
// waits.
package outq

import (
	"io"
	"strconv"
)

// Len is the capacity of a Queue: a few dozen lines of sniffer trace.
const Len = 2048

// Queue is a byte queue for writers and one sender. It is not safe for
// preemptive concurrency: on TinyGo's cooperative scheduler a Write runs
// without yielding, so writers and the sender cannot interleave inside it.
type Queue struct {
	buf [Len]byte
	// head counts the bytes sent, tail the bytes queued; both wrap.
	head, tail uint32
	// dropped counts the bytes dropped since the last Dropped.
	dropped uint32
}

// Free returns the room left.
func (q *Queue) Free() int {
	return Len - int(q.tail-q.head)
}

// Buffered returns the number of bytes queued.
func (q *Queue) Buffered() int {
	return int(q.tail - q.head)
}

// Write queues p whole, or drops it whole when it does not fit, so that
// output is not cut inside a write. It always reports p as written.
func (q *Queue) Write(p []byte) (int, error) {
	if len(p) > q.Free() {
		q.dropped += uint32(len(p))
		return len(p), nil
	}
	q.put(p)
	return len(p), nil
}

// WriteCRLF queues p with each "\n" turned into "\r\n", for serial
// terminals, whole or not at all like Write.
func (q *Queue) WriteCRLF(p []byte) (int, error) {
	n := len(p)
	for _, b := range p {
		if b == '\n' {
			n++
		}
	}
	if n > q.Free() {
		q.dropped += uint32(n)
		return len(p), nil
	}
	start := 0
	for i, b := range p {
		if b == '\n' {
			q.put(p[start:i])
			q.put([]byte{'\r', '\n'})
			start = i + 1
		}
	}
	q.put(p[start:])
	return len(p), nil
}

func (q *Queue) put(p []byte) {
	for _, b := range p {
		q.buf[q.tail%Len] = b
		q.tail++
	}
}

// Peek returns the oldest queued bytes that are contiguous in the buffer,
// without removing them. It is empty when nothing is queued.
func (q *Queue) Peek() []byte {
	start := q.head % Len
	end := start + uint32(q.Buffered())
	if end > Len {
		end = Len
	}
	return q.buf[start:end]
}

// Discard removes the n oldest queued bytes, after the sender sent them.
func (q *Queue) Discard(n int) {
	q.head += uint32(min(n, q.Buffered()))
}

// Reset drops the queued bytes without counting them.
func (q *Queue) Reset() {
	q.head = q.tail
}

// Dropped returns the number of bytes dropped since its last call and
// clears it.
func (q *Queue) Dropped() uint32 {
	n := q.dropped
	q.dropped = 0
	return n
}

// Send writes the queued bytes to w, one contiguous part per call of
// w.Write, until the queue is empty. Once the queue ran empty after output
// was dropped, it queues and sends a note with the number of bytes lost,
// so that the gap is visible on the terminal. It reports whether anything
// was sent.
func (q *Queue) Send(w io.Writer) bool {
	sent := false
	for {
		p := q.Peek()
		if len(p) == 0 {
			n := q.Dropped()
			if n == 0 {
				return sent
			}
			q.WriteCRLF([]byte("\n[" + strconv.FormatUint(uint64(n), 10) + " bytes of output dropped]\n"))
			continue
		}
		w.Write(p)
		q.Discard(len(p))
		sent = true
	}
}
