// Package switches holds the logic of the wall switches that does not touch
// the hardware: press detection from interrupt counters and the hold
// gestures.
package switches

import "time"

// HoldTime is how long a gesture must be held before it fires.
const HoldTime = 5 * time.Second

// Edges follows a counter that an interrupt handler increments on each
// press.
type Edges struct {
	seen uint32
}

// Changed reports whether the counter moved to n since the last call. A
// counter that wrapped around counts as changed.
func (e *Edges) Changed(n uint32) bool {
	if n == e.seen {
		return false
	}
	e.seen = n
	return true
}

// Hold detects a gesture that is held for longer than HoldTime. It fires
// once per hold, and again only after the gesture was released.
type Hold struct {
	held  bool
	fired bool
	since time.Time
}

// Update records whether the gesture is active at now and reports whether
// it fires.
func (h *Hold) Update(now time.Time, active bool) bool {
	if !active {
		h.held, h.fired = false, false
		return false
	}
	if !h.held {
		h.held, h.since = true, now
	}
	if h.fired || now.Sub(h.since) <= HoldTime {
		return false
	}
	h.fired = true
	return true
}
