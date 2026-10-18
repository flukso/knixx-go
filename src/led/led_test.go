package led

import (
	"image/color"
	"reflect"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

const ms = time.Millisecond

// edge is a change of LED1 at a time after t0.
type edge struct {
	at time.Duration
	on bool
}

// play follows the frames of p from t0 until it is idle and returns the
// changes of LED1.
func play(t *testing.T, p *Player) []edge {
	t.Helper()
	var edges []edge
	now := t0
	for i := 0; ; i++ {
		if i > 1000 {
			t.Fatal("player does not end")
		}
		led1, next, busy := p.Frame(now)
		if !busy {
			if led1 != (color.RGBA{}) {
				t.Errorf("idle LED1 %v", led1)
			}
			return append(edges, edge{now.Sub(t0), false})
		}
		on := led1 != (color.RGBA{})
		if on && led1 != patterns[p.cur].colour {
			t.Fatalf("mode %d lit in %v", p.cur, led1)
		}
		if len(edges) == 0 || edges[len(edges)-1].on != on {
			edges = append(edges, edge{now.Sub(t0), on})
		}
		if !next.After(now) {
			t.Fatalf("next %v not after %v", next, now)
		}
		now = next
	}
}

// blinks returns the edges of n cycles of on and period from start.
func blinks(start, on, period time.Duration, n int) []edge {
	var e []edge
	for i := 0; i < n; i++ {
		c := start + time.Duration(i)*period
		e = append(e, edge{c, true}, edge{c + on, false})
	}
	return e
}

func TestModes(t *testing.T) {
	for _, tc := range []struct {
		mode Mode
		want []edge
	}{
		{Occupancy, []edge{{0, true}, {50 * ms, false}, {100 * ms, false}}},
		{Error, append(blinks(0, 10*ms, 100*ms, 5), edge{500 * ms, false})},
		{Init, append(blinks(0, 100*ms, 200*ms, 50), edge{10 * time.Second, false})},
	} {
		var p Player
		p.Apply(Trigger{Mode: tc.mode}, t0)
		if got := play(t, &p); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("mode %d: got %v, want %v", tc.mode, got, tc.want)
		}
	}
}

func TestSequence(t *testing.T) {
	var p Player
	for _, m := range []Mode{Error, Occupancy, Occupancy} {
		p.Apply(Trigger{Mode: m}, t0)
	}
	want := append(blinks(0, 10*ms, 100*ms, 5), blinks(500*ms, 50*ms, 100*ms, 2)...)
	want = append(want, edge{700 * ms, false})
	if got := play(t, &p); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestLateFrame(t *testing.T) {
	var p Player
	p.Apply(Trigger{Mode: Occupancy}, t0)
	p.Apply(Trigger{Mode: Error}, t0)
	// Error started when Occupancy ended, at 100 ms: at 205 ms it is in
	// its second flash.
	led1, next, busy := p.Frame(t0.Add(205 * ms))
	if led1 != Red || !next.Equal(t0.Add(210*ms)) || !busy {
		t.Errorf("got %v %v %v", led1, next.Sub(t0), busy)
	}
}

func TestCancel(t *testing.T) {
	var p Player
	p.Apply(Trigger{Mode: Init}, t0)
	p.Apply(Trigger{Mode: Occupancy}, t0)
	p.Apply(Trigger{Mode: Init}, t0)
	p.Apply(Trigger{Mode: Init, Cancel: true}, t0.Add(150*ms))
	// The queued Init is dropped as well; Occupancy starts at once.
	led1, next, busy := p.Frame(t0.Add(150 * ms))
	if led1 != Green || !next.Equal(t0.Add(200*ms)) || !busy {
		t.Errorf("got %v %v %v", led1, next.Sub(t0), busy)
	}
	if _, _, busy := p.Frame(t0.Add(250 * ms)); busy {
		t.Error("busy after the occupancy pulse")
	}
}

func TestCancelOther(t *testing.T) {
	var p Player
	p.Apply(Trigger{Mode: Error}, t0)
	p.Apply(Trigger{Mode: Init, Cancel: true}, t0)
	p.Apply(Trigger{Mode: numModes}, t0)
	p.Apply(Trigger{Mode: numModes, Cancel: true}, t0)
	want := append(blinks(0, 10*ms, 100*ms, 5), edge{500 * ms, false})
	if got := play(t, &p); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestQueueFull(t *testing.T) {
	var p Player
	// One playing and QueueLen queued; the rest is dropped.
	for i := 0; i < QueueLen+5; i++ {
		p.Apply(Trigger{Mode: Occupancy}, t0)
	}
	got := play(t, &p)
	if end := got[len(got)-1].at; end != (QueueLen+1)*100*ms {
		t.Errorf("played until %v, want %v", end, (QueueLen+1)*100*ms)
	}
}

func TestNotify(t *testing.T) {
	ch := make(chan Trigger, 1)
	Notify(ch, Trigger{Mode: Error})
	Notify(ch, Trigger{Mode: Init}) // dropped, does not block
	if got := <-ch; got.Mode != Error {
		t.Errorf("got %v", got)
	}
	Notify(nil, Trigger{Mode: Error})
}

// fakeStrip records the colours written.
type fakeStrip struct {
	mu     sync.Mutex
	writes [][2]color.RGBA
}

func (f *fakeStrip) WriteColors(buf []color.RGBA) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, [2]color.RGBA{buf[0], buf[1]})
	return nil
}

func TestRun(t *testing.T) {
	var strip fakeStrip
	ch := make(chan Trigger, QueueLen)
	done := make(chan struct{})
	go func() {
		Run(ch, &strip)
		close(done)
	}()
	Notify(ch, Trigger{Mode: Occupancy})
	time.Sleep(300 * ms)
	close(ch)
	<-done

	off := [2]color.RGBA{}
	want := [][2]color.RGBA{off, {Green, {}}, off}
	if !reflect.DeepEqual(strip.writes, want) {
		t.Errorf("got %v, want %v", strip.writes, want)
	}
}
