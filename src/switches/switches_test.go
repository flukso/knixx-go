package switches

import (
	"math"
	"testing"
	"time"
)

func TestEdges(t *testing.T) {
	var e Edges
	for _, tt := range []struct {
		n       uint32
		changed bool
	}{
		{0, false},
		{1, true},
		{1, false},
		{3, true},
		{math.MaxUint32, true},
		{0, true},
		{0, false},
	} {
		if got := e.Changed(tt.n); got != tt.changed {
			t.Errorf("Changed(%d) = %v, want %v", tt.n, got, tt.changed)
		}
	}
}

func TestHold(t *testing.T) {
	t0 := time.Unix(1000, 0)
	at := func(d time.Duration) time.Time { return t0.Add(d) }
	ms := time.Millisecond

	var h Hold
	for _, tt := range []struct {
		at     time.Duration
		active bool
		fire   bool
	}{
		{0, false, false},
		// A hold released before HoldTime does not fire and restarts.
		{0, true, false},
		{4900 * ms, true, false},
		{4920 * ms, false, false},
		{5100 * ms, true, false},
		{10100 * ms, true, false},
		// More than HoldTime fires, once.
		{10120 * ms, true, true},
		{10140 * ms, true, false},
		{20000 * ms, true, false},
		// After a release it fires again.
		{20020 * ms, false, false},
		{20040 * ms, true, false},
		{25060 * ms, true, true},
	} {
		if got := h.Update(at(tt.at), tt.active); got != tt.fire {
			t.Errorf("Update(%v, %v) = %v, want %v", tt.at, tt.active, got, tt.fire)
		}
	}
}
