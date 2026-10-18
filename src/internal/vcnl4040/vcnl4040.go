// Package vcnl4040 drives the ambient light sensor (ALS) of the Vishay
// VCNL4040 proximity and ambient light sensor. The proximity sensor is kept
// shut down.
//
// The ALS has no single measurement mode: while powered on it measures
// continuously and draws about 260 µA, against 0.2 µA when shut down. To keep
// the sensor low-power, ReadIlluminance powers the ALS on for one
// measurement and shuts it down again.
//
// Datasheet: https://www.vishay.com/docs/84274/vcnl4040.pdf
package vcnl4040

import (
	"errors"
	"time"

	"tinygo.org/x/drivers"
)

// Address is the fixed I2C address of the VCNL4040.
const Address = 0x60

// Command codes of the 16-bit registers, accessed LSB first.
const (
	regALSConf  = 0x00 // ALS_CONF in the low byte
	regPSConf12 = 0x03 // PS_CONF1 low, PS_CONF2 high
	regPSConf3  = 0x04 // PS_CONF3 low, PS_MS high
	regALSData  = 0x09
	regID       = 0x0c
)

// ALS_CONF bits.
const (
	alsIT80ms = 0x00 << 6 // integration time 80 ms: 0.1 lx/step
	alsSD     = 0x01      // shut down
)

// psSD shuts the proximity sensor down, in PS_CONF1.
const psSD = 0x01

// deviceID is the content of the ID register: ID_L 0x86, version 1.
const deviceID = 0x0186

const (
	// integration is the ALS integration time of alsIT80ms.
	integration = 80 * time.Millisecond
	// settle is the wait from powering the ALS on to reading its data. The
	// datasheet does not specify one; two integration times leave margin
	// for the first measurement to complete.
	settle = 2 * integration
)

// MaxLux10 is the reading of a saturated sensor, 6553.5 lx.
const MaxLux10 = 65535

// ErrID reports a device on the address that is not a VCNL4040.
var ErrID = errors.New("vcnl4040: unexpected device id")

// Device wraps an I2C connection to a VCNL4040.
type Device struct {
	bus     drivers.I2C
	Address uint16
	// Sleep waits for d; it defaults to time.Sleep.
	Sleep func(d time.Duration)
}

// New returns a device on bus. The bus must already be configured.
func New(bus drivers.I2C) Device {
	return Device{bus: bus, Address: Address, Sleep: time.Sleep}
}

// Configure checks the device id and shuts both the proximity and the
// ambient light sensor down, with the ALS interrupt disabled. The sensor
// keeps its registers across a reset of the host, so they are all written.
func (d *Device) Configure() error {
	id, err := d.read(regID)
	if err != nil {
		return err
	}
	if id != deviceID {
		return ErrID
	}
	if err := d.write(regPSConf12, psSD); err != nil {
		return err
	}
	if err := d.write(regPSConf3, 0); err != nil {
		return err
	}
	return d.write(regALSConf, alsIT80ms|alsSD)
}

// ReadIlluminance powers the ALS on, waits for a measurement, reads it and
// shuts the ALS down again, also when the read fails. It returns the
// illuminance in tenths of a lux, at most MaxLux10.
func (d *Device) ReadIlluminance() (uint32, error) {
	if err := d.write(regALSConf, alsIT80ms); err != nil {
		// The ALS may have been powered on all the same.
		d.write(regALSConf, alsIT80ms|alsSD)
		return 0, err
	}
	d.Sleep(settle)
	raw, err := d.read(regALSData)
	if sdErr := d.write(regALSConf, alsIT80ms|alsSD); err == nil {
		err = sdErr
	}
	if err != nil {
		return 0, err
	}
	// At 80 ms one step is 0.1 lx.
	return uint32(raw), nil
}

// read reads a 16-bit register.
func (d *Device) read(reg byte) (uint16, error) {
	var data [2]byte
	if err := d.bus.Tx(d.Address, []byte{reg}, data[:]); err != nil {
		return 0, err
	}
	return uint16(data[0]) | uint16(data[1])<<8, nil
}

// write writes a 16-bit register.
func (d *Device) write(reg byte, v uint16) error {
	return d.bus.Tx(d.Address, []byte{reg, byte(v), byte(v >> 8)}, nil)
}
