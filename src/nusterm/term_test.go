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
