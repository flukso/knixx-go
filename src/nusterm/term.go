package main

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
