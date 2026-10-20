package outq

import (
	"bytes"
	"strings"
	"testing"
)

// chunkWriter records the writes it gets.
type chunkWriter struct {
	bytes.Buffer
	writes int
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.Buffer.Write(p)
}

func TestWriteSend(t *testing.T) {
	var q Queue
	q.Write([]byte("hello "))
	q.WriteCRLF([]byte("a\nb\n"))
	if q.Buffered() != 12 || q.Free() != Len-12 {
		t.Fatalf("Buffered %d, Free %d", q.Buffered(), q.Free())
	}
	var w chunkWriter
	if !q.Send(&w) {
		t.Error("Send reported nothing sent")
	}
	if got := w.String(); got != "hello a\r\nb\r\n" {
		t.Errorf("sent %q", got)
	}
	if q.Buffered() != 0 || q.Send(&w) {
		t.Errorf("after Send: Buffered %d", q.Buffered())
	}
}

func TestDropWhole(t *testing.T) {
	var q Queue
	fill := bytes.Repeat([]byte{'x'}, Len-4)
	q.Write(fill)
	// Neither fits whole; neither is cut.
	if n, err := q.Write([]byte("12345")); n != 5 || err != nil {
		t.Errorf("Write = %d, %v", n, err)
	}
	q.WriteCRLF([]byte("ab\n\n")) // 6 bytes with the CRs
	if q.Buffered() != Len-4 {
		t.Fatalf("Buffered %d after drops", q.Buffered())
	}
	q.Write([]byte("1234")) // fits exactly
	if q.Free() != 0 {
		t.Fatalf("Free %d", q.Free())
	}

	var w chunkWriter
	q.Send(&w)
	got := w.String()
	want := string(fill) + "1234" + "\r\n[11 bytes of output dropped]\r\n"
	if got != want {
		t.Errorf("sent %q...%q, want ...%q", got[:8], got[len(got)-40:], want[len(want)-40:])
	}
	if q.Dropped() != 0 {
		t.Error("dropped count not cleared by the note")
	}
}

// TestWrapAround sends across the end of the buffer: in two contiguous
// parts, in order.
func TestWrapAround(t *testing.T) {
	var q Queue
	var w chunkWriter
	q.Write(bytes.Repeat([]byte{'-'}, Len-3))
	q.Send(&w)
	w.Reset()
	w.writes = 0

	q.Write([]byte("abcdefgh"))
	if p := q.Peek(); string(p) != "abc" {
		t.Errorf("Peek = %q", p)
	}
	q.Send(&w)
	if w.String() != "abcdefgh" || w.writes != 2 {
		t.Errorf("sent %q in %d writes", w.String(), w.writes)
	}
}

// slowWriter writes to the queue while it is being sent, as a writer
// goroutine would while the sender waits for the port.
type slowWriter struct {
	q    *Queue
	out  strings.Builder
	more []string
}

func (w *slowWriter) Write(p []byte) (int, error) {
	w.out.Write(p)
	if len(w.more) > 0 {
		w.q.Write([]byte(w.more[0]))
		w.more = w.more[1:]
	}
	return len(p), nil
}

func TestWriteWhileSending(t *testing.T) {
	var q Queue
	q.Write([]byte("one "))
	w := slowWriter{q: &q, more: []string{"two ", "three"}}
	q.Send(&w)
	if got := w.out.String(); got != "one two three" {
		t.Errorf("sent %q", got)
	}
}

func TestReset(t *testing.T) {
	var q Queue
	q.Write([]byte("stale"))
	q.Reset()
	var w chunkWriter
	if q.Send(&w) || w.Len() != 0 {
		t.Errorf("sent %q after Reset", w.String())
	}
}
