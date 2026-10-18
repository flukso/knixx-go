// Package led drives the two status LEDs, a WS2812B string. One goroutine,
// Run, owns the LEDs; the only way in is its channel of triggers. Each
// trigger queues a mode, and the modes play one after another, each for its
// length, unless a cancel trigger ends one early. LED2 is kept off.
package led

import (
	"image/color"
	"time"
)

// Mode is a pattern of LED1.
type Mode uint8

const (
	// Init blinks at 5 Hz for at most 10 s, while the device starts.
	Init Mode = iota
	// Occupancy is a single 50 ms pulse for a motion of the ToF sensor.
	Occupancy
	// Error flashes 10 ms every 100 ms for 0.5 s.
	Error
	numModes
)

// Trigger queues Mode, or with Cancel set ends it: a playing Mode stops and
// queued ones are dropped.
type Trigger struct {
	Mode   Mode
	Cancel bool
}

// QueueLen is the capacity of both the trigger channel and the queue of
// modes waiting to play.
const QueueLen = 8

// The colours of LED1, dimmed: full brightness is glaring.
var (
	Yellow = color.RGBA{R: 0x20, G: 0x10}
	Green  = color.RGBA{G: 0x20}
	Red    = color.RGBA{R: 0x20}
)

// pattern lights the LED in colour for on at the start of every period,
// until length has passed.
type pattern struct {
	colour             color.RGBA
	on, period, length time.Duration
}

var patterns = [numModes]pattern{
	Init:      {colour: Yellow, on: 100 * time.Millisecond, period: 200 * time.Millisecond, length: 10 * time.Second},
	Occupancy: {colour: Green, on: 50 * time.Millisecond, period: 100 * time.Millisecond, length: 100 * time.Millisecond},
	Error:     {colour: Red, on: 10 * time.Millisecond, period: 100 * time.Millisecond, length: 500 * time.Millisecond},
}

// Notify sends t on ch, or drops it when ch is full: the sender never
// blocks.
func Notify(ch chan<- Trigger, t Trigger) {
	select {
	case ch <- t:
	default:
	}
}

// Player sequences the modes. It does not read the clock: the caller passes
// the time.
type Player struct {
	playing bool
	cur     Mode
	start   time.Time
	// The modes waiting to play, a ring of n from head.
	pending [QueueLen]Mode
	head, n uint8
}

// Apply handles the trigger t received at now. A mode that arrives with the
// queue full is dropped.
func (p *Player) Apply(t Trigger, now time.Time) {
	if t.Mode >= numModes {
		return
	}
	p.settle(now)
	if t.Cancel {
		p.cancel(t.Mode, now)
		return
	}
	if !p.playing {
		p.playing, p.cur, p.start = true, t.Mode, now
		return
	}
	if p.n == QueueLen {
		return
	}
	p.pending[(p.head+p.n)%QueueLen] = t.Mode
	p.n++
}

// Frame returns the colour of LED1 at now and the time of its next change,
// or busy false when no mode is left to play.
func (p *Player) Frame(now time.Time) (led1 color.RGBA, next time.Time, busy bool) {
	p.settle(now)
	if !p.playing {
		return color.RGBA{}, time.Time{}, false
	}
	pat := patterns[p.cur]
	elapsed := now.Sub(p.start)
	phase := elapsed % pat.period
	cycle := p.start.Add(elapsed - phase)
	if phase < pat.on {
		led1, next = pat.colour, cycle.Add(pat.on)
	} else {
		next = cycle.Add(pat.period)
	}
	if end := p.start.Add(pat.length); next.After(end) {
		next = end
	}
	return led1, next, true
}

// settle moves past the modes that ended by now. Each next mode starts when
// the previous one ended, so late calls keep the sequence gapless.
func (p *Player) settle(now time.Time) {
	for p.playing {
		end := p.start.Add(patterns[p.cur].length)
		if now.Before(end) {
			return
		}
		p.advance(end)
	}
}

// advance starts the next queued mode at at, if any.
func (p *Player) advance(at time.Time) {
	if p.n == 0 {
		p.playing = false
		return
	}
	p.cur, p.start = p.pending[p.head], at
	p.head = (p.head + 1) % QueueLen
	p.n--
}

// cancel drops the queued entries of m and ends m if it is playing.
func (p *Player) cancel(m Mode, now time.Time) {
	var kept uint8
	for i := uint8(0); i < p.n; i++ {
		if q := p.pending[(p.head+i)%QueueLen]; q != m {
			p.pending[(p.head+kept)%QueueLen] = q
			kept++
		}
	}
	p.n = kept
	if p.playing && p.cur == m {
		p.advance(now)
	}
}

// Strip writes the colours of a LED string, as ws2812.Device does.
type Strip interface {
	WriteColors(buf []color.RGBA) error
}

// Run plays the triggers received on triggers on strip, whose first LED is
// LED1 and second LED2. It writes the strip only when LED1 changes and,
// without a mode to play, only waits for triggers. It returns once triggers
// is closed.
func Run(triggers <-chan Trigger, strip Strip) {
	var p Player
	// The LEDs keep their colour across a reset of the MCU.
	var leds [2]color.RGBA
	strip.WriteColors(leds[:])

	timer := time.NewTimer(time.Hour)
	stop := func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
	stop()

	for {
		now := time.Now()
		led1, next, busy := p.Frame(now)
		if led1 != leds[0] {
			leds[0] = led1
			strip.WriteColors(leds[:])
		}

		var wake <-chan time.Time
		if busy {
			timer.Reset(next.Sub(now))
			wake = timer.C
		}
		select {
		case t, ok := <-triggers:
			if !ok {
				return
			}
			p.Apply(t, time.Now())
		case <-wake:
		}
		if busy {
			stop()
		}
	}
}
