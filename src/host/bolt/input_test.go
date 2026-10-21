package main

import (
	"bytes"
	"testing"
)

func TestChunks(t *testing.T) {
	long := bytes.Repeat([]byte{'x'}, 45)
	for _, c := range []struct {
		name string
		in   []byte
		size int
		want []string
		quit bool
	}{
		{"empty", nil, 20, nil, false},
		{"key", []byte("a"), 20, []string{"a"}, false},
		{"line", []byte("version\r"), 20, []string{"version\r"}, false},
		{"exact", long[:20], 20, []string{string(long[:20])}, false},
		{"split", long, 20, []string{string(long[:20]), string(long[:20]), string(long[:5])}, false},
		{"bigger MTU", long, 244, []string{string(long)}, false},
		{"default size", long, 0, []string{string(long[:20]), string(long[:20]), string(long[:5])}, false},
		{"escape alone", []byte{Escape}, 20, nil, true},
		{"escape inside", []byte("ab\x1dcd"), 20, []string{"ab"}, true},
	} {
		got, quit := chunks(c.in, c.size)
		var gs []string
		for _, g := range got {
			gs = append(gs, string(g))
		}
		if quit != c.quit || len(gs) != len(c.want) {
			t.Errorf("%s: chunks = %q, quit %v", c.name, gs, quit)
			continue
		}
		for i := range gs {
			if gs[i] != c.want[i] {
				t.Errorf("%s: chunk %d = %q, want %q", c.name, i, gs[i], c.want[i])
			}
		}
	}
}

func TestPending(t *testing.T) {
	q := newPending()
	if p, eof := q.take(); p != nil || eof {
		t.Errorf("empty take = %q, %v", p, eof)
	}
	// Keys typed while a write is in flight come out together, with one
	// token.
	q.add([]byte("a"))
	q.add([]byte("bc"))
	select {
	case <-q.ready:
	default:
		t.Fatal("no token after add")
	}
	select {
	case <-q.ready:
		t.Error("second token")
	default:
	}
	if p, eof := q.take(); string(p) != "abc" || eof {
		t.Errorf("take = %q, %v", p, eof)
	}
	// What is left at the end of the input still comes out, with eof.
	q.add([]byte("d\x1d"))
	q.close()
	<-q.ready
	p, eof := q.take()
	if string(p) != "d\x1d" || !eof {
		t.Errorf("take at eof = %q, %v", p, eof)
	}
	if out, quit := chunks(p, 20); len(out) != 1 || string(out[0]) != "d" || !quit {
		t.Errorf("chunks = %q, %v", out, quit)
	}
}
