package main

import "sync"

// pending collects what is typed while a write to the board is in flight,
// so that it goes out together in the next write instead of one write per
// key, each waiting for its response.
type pending struct {
	mu  sync.Mutex
	buf []byte
	eof bool
	// ready holds a token while there is something to take.
	ready chan struct{}
}

func newPending() *pending {
	return &pending{ready: make(chan struct{}, 1)}
}

// add queues p, read from the terminal.
func (q *pending) add(p []byte) {
	q.mu.Lock()
	q.buf = append(q.buf, p...)
	q.mu.Unlock()
	q.signal()
}

// close records the end of the input.
func (q *pending) close() {
	q.mu.Lock()
	q.eof = true
	q.mu.Unlock()
	q.signal()
}

func (q *pending) signal() {
	select {
	case q.ready <- struct{}{}:
	default:
	}
}

// take returns everything queued since the last take, and whether the
// input ended.
func (q *pending) take() ([]byte, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	p := q.buf
	q.buf = nil
	return p, q.eof
}

// Escape ends the session: Ctrl-], as in telnet.
const Escape = 0x1d

// defaultChunk is the payload of an ATT write with the default MTU of 23.
const defaultChunk = 20

// chunks splits p, read from the terminal, into writes of at most size
// bytes for the RX characteristic. It stops at Escape, which it reports;
// the bytes before it are still sent. A size below 1 means defaultChunk.
func chunks(p []byte, size int) (out [][]byte, quit bool) {
	if size < 1 {
		size = defaultChunk
	}
	for i, b := range p {
		if b == Escape {
			p, quit = p[:i], true
			break
		}
	}
	for len(p) > 0 {
		n := min(size, len(p))
		out = append(out, p[:n])
		p = p[n:]
	}
	return out, quit
}
