// Package i2cbus shares an I2C bus between goroutines.
//
// TinyGo's nRF52 I2C transfer yields to the scheduler while it waits for the
// bus, so a goroutine can start a transfer on the peripheral while another
// one is still in progress: it reprograms the peripheral under the running
// transfer and clears its events, which corrupts both and can leave them
// waiting for an event that never comes.
package i2cbus

import (
	"sync"

	"tinygo.org/x/drivers"
)

// Shared runs one transfer at a time on its bus. Each transfer is a whole
// I2C transaction, from START to STOP, so devices on the bus keep their
// state between transfers of other devices.
type Shared struct {
	bus drivers.I2C
	mu  sync.Mutex
}

// New returns bus shared between goroutines.
func New(bus drivers.I2C) *Shared {
	return &Shared{bus: bus}
}

// Tx performs a transaction with addr on the bus, after any transfer in
// progress.
func (s *Shared) Tx(addr uint16, w, r []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bus.Tx(addr, w, r)
}
