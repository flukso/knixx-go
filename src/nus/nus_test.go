package nus

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestRing(t *testing.T) {
	var r Ring
	if _, err := r.ReadByte(); err != io.EOF {
		t.Errorf("empty ring: %v", err)
	}
	// Wrap around a few times.
	for round := 0; round < 3*RingLen/100; round++ {
		in := make([]byte, 100)
		for i := range in {
			in[i] = byte(round + i)
		}
		if n := r.Put(in); n != len(in) {
			t.Fatalf("put %d of %d", n, len(in))
		}
		if r.Buffered() != len(in) {
			t.Fatalf("buffered %d", r.Buffered())
		}
		for i := range in {
			b, err := r.ReadByte()
			if err != nil || b != in[i] {
				t.Fatalf("round %d byte %d: %d %v", round, i, b, err)
			}
		}
	}
}

func TestRingFull(t *testing.T) {
	var r Ring
	if n := r.Put(make([]byte, RingLen+10)); n != RingLen {
		t.Errorf("put %d, want %d", n, RingLen)
	}
	if n := r.Put([]byte{1}); n != 0 {
		t.Errorf("put into a full ring: %d", n)
	}
	r.ReadByte()
	if n := r.Put([]byte{1, 2}); n != 1 {
		t.Errorf("put %d, want 1", n)
	}
	r.Reset()
	if r.Buffered() != 0 {
		t.Errorf("buffered %d after reset", r.Buffered())
	}
}

// fakeLink records the notifications and fails the first fails sends.
type fakeLink struct {
	sent      [][]byte
	fails     int
	connected bool
	slept     time.Duration
}

var errBusy = errors.New("no tx buffers")

func (f *fakeLink) writer() *Writer {
	return &Writer{
		Send: func(p []byte) error {
			if f.fails > 0 {
				f.fails--
				return errBusy
			}
			f.sent = append(f.sent, append([]byte(nil), p...))
			return nil
		},
		Connected: func() bool { return f.connected },
		Sleep:     func(d time.Duration) { f.slept += d },
	}
}

func TestWriterChunks(t *testing.T) {
	f := &fakeLink{connected: true, fails: 3}
	p := bytes.Repeat([]byte("0123456789"), 5)
	if n, err := f.writer().Write(p); n != len(p) || err != nil {
		t.Errorf("Write: %d %v", n, err)
	}
	if len(f.sent) != 3 || len(f.sent[0]) != Chunk || len(f.sent[2]) != 10 {
		t.Errorf("chunks %q", f.sent)
	}
	if !bytes.Equal(bytes.Join(f.sent, nil), p) {
		t.Errorf("sent %q", bytes.Join(f.sent, nil))
	}
	if f.slept != 3*retryDelay {
		t.Errorf("slept %v", f.slept)
	}
}

func TestWriterGivesUp(t *testing.T) {
	f := &fakeLink{connected: true, fails: 1 << 30}
	if n, _ := f.writer().Write(make([]byte, 3*Chunk)); n != 3*Chunk {
		t.Errorf("n = %d", n)
	}
	if len(f.sent) != 0 || f.slept != retryLimit {
		t.Errorf("sent %d, slept %v", len(f.sent), f.slept)
	}
}

func TestWriterDisconnected(t *testing.T) {
	f := &fakeLink{}
	if n, _ := f.writer().Write([]byte("hello")); n != 5 {
		t.Errorf("n = %d", n)
	}
	if len(f.sent) != 0 {
		t.Errorf("sent %q while disconnected", f.sent)
	}
}

func TestTimer(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tm Timer
	if tm.Expired(t0.Add(time.Hour)) || tm.Left(t0) != 0 {
		t.Error("an unarmed timer expired")
	}
	tm.Arm(t0)
	if tm.Expired(t0.Add(IdleTimeout-time.Millisecond)) || !tm.Expired(t0.Add(IdleTimeout)) {
		t.Error("expiry not at IdleTimeout")
	}
	if left := tm.Left(t0.Add(IdleTimeout / 4)); left != IdleTimeout-IdleTimeout/4 {
		t.Errorf("left %v", left)
	}
	if left := tm.Left(t0.Add(IdleTimeout + time.Second)); left != 0 {
		t.Errorf("left after expiry %v", left)
	}
	tm.Disarm()
	if tm.Expired(t0.Add(time.Hour)) {
		t.Error("a disarmed timer expired")
	}
}

func TestWriterKeepsUTF8Whole(t *testing.T) {
	// The first ° starts at bytes 18 to 21, around the end of a chunk.
	for pad := 0; pad < 4; pad++ {
		f := &fakeLink{connected: true}
		p := []byte(strings.Repeat("x", 12+pad) + "20.44 °C, 5 °C")
		f.writer().Write(p)
		for i, c := range f.sent {
			if !utf8.Valid(c) || len(c) > Chunk {
				t.Errorf("pad %d: chunk %d %q", pad, i, c)
			}
		}
		if !bytes.Equal(bytes.Join(f.sent, nil), p) {
			t.Errorf("pad %d: sent %q", pad, bytes.Join(f.sent, nil))
		}
	}
	// Bytes that are not UTF-8 are still split at Chunk.
	if n := split(bytes.Repeat([]byte{0x80}, 30)); n != Chunk {
		t.Errorf("split %d", n)
	}
}
