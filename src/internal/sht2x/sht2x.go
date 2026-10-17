// Package sht2x provides a driver for the SHT2x (SHT20, SHT21, SHT25)
// digital humidity and temperature sensors by Sensirion.
//
// Datasheet:
// https://sensirion.com/media/documents/CCDE1377/635000A2/Sensirion_Datasheet_Humidity_Sensor_SHT20.pdf
package sht2x

import (
	"errors"
	"time"

	"tinygo.org/x/drivers"
)

// Address is the fixed I2C address of the SHT2x.
const Address = 0x40

// Commands in no hold master mode: the sensor releases the bus while it
// measures.
const (
	cmdTriggerT  = 0xf3
	cmdTriggerRH = 0xf5
	cmdSoftReset = 0xfe
)

// Worst case durations from the datasheet, at the default resolution of
// 14 bit for temperature and 12 bit for humidity.
const (
	resetTime = 15 * time.Millisecond
	tTime     = 85 * time.Millisecond
	rhTime    = 29 * time.Millisecond
)

// ErrChecksum reports a measurement whose CRC does not match.
var ErrChecksum = errors.New("sht2x: checksum mismatch")

// Device wraps an I2C connection to an SHT2x.
type Device struct {
	bus     drivers.I2C
	Address uint16
}

// New returns a device on bus. The bus must already be configured.
func New(bus drivers.I2C) Device {
	return Device{bus: bus, Address: Address}
}

// Configure soft resets the sensor, restoring the default resolution.
func (d *Device) Configure() error {
	if err := d.bus.Tx(d.Address, []byte{cmdSoftReset}, nil); err != nil {
		return err
	}
	time.Sleep(resetTime)
	return nil
}

// ReadTemperature returns the temperature in milli degrees Celsius.
func (d *Device) ReadTemperature() (int32, error) {
	raw, err := d.measure(cmdTriggerT, tTime)
	if err != nil {
		return 0, err
	}
	return int32(-46850 + 175720*int64(raw)/65536), nil
}

// ReadHumidity returns the relative humidity in hundredths of a percent,
// clamped to 0..10000.
func (d *Device) ReadHumidity() (int32, error) {
	raw, err := d.measure(cmdTriggerRH, rhTime)
	if err != nil {
		return 0, err
	}
	rh := int32(-600 + 12500*int64(raw)/65536)
	switch {
	case rh < 0:
		rh = 0
	case rh > 10000:
		rh = 10000
	}
	return rh, nil
}

// measure triggers a measurement, waits for it to complete and returns the
// raw value with the status bits cleared.
func (d *Device) measure(cmd byte, wait time.Duration) (uint16, error) {
	if err := d.bus.Tx(d.Address, []byte{cmd}, nil); err != nil {
		return 0, err
	}
	time.Sleep(wait)
	var data [3]byte
	if err := d.bus.Tx(d.Address, nil, data[:]); err != nil {
		return 0, err
	}
	if crc8(data[:2]) != data[2] {
		return 0, ErrChecksum
	}
	return (uint16(data[0])<<8 | uint16(data[1])) &^ 3, nil
}

// crc8 computes the SHT2x checksum: polynomial x^8+x^5+x^4+1, initial 0.
func crc8(data []byte) byte {
	var crc byte
	for _, b := range data {
		crc ^= b
		for i := 0; i < 8; i++ {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ 0x31
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
