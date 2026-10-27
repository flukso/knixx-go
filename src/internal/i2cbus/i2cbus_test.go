package i2cbus

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// yieldingBus yields in the middle of each transfer, as TinyGo's does, and
// counts the transfers that overlapped another.
type yieldingBus struct {
	active, overlaps, done atomic.Int32
}

func (b *yieldingBus) Tx(addr uint16, w, r []byte) error {
	if b.active.Add(1) > 1 {
		b.overlaps.Add(1)
	}
	for i := 0; i < 10; i++ {
		runtime.Gosched()
	}
	b.active.Add(-1)
	b.done.Add(1)
	return nil
}

func TestSharedSerializes(t *testing.T) {
	bus := &yieldingBus{}
	s := New(bus)
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var buf [2]byte
			for i := 0; i < 200; i++ {
				s.Tx(0x29, buf[:1], buf[1:])
			}
		}()
	}
	wg.Wait()
	if n := bus.overlaps.Load(); n != 0 {
		t.Errorf("%d transfers overlapped another", n)
	}
	if n := bus.done.Load(); n != 800 {
		t.Errorf("%d transfers, want 800", n)
	}
}

func TestSharedAllocs(t *testing.T) {
	s := New(&yieldingBus{})
	var buf [2]byte
	if n := testing.AllocsPerRun(100, func() { s.Tx(0x29, buf[:1], buf[1:]) }); n != 0 {
		t.Errorf("Tx allocates %v times", n)
	}
}
