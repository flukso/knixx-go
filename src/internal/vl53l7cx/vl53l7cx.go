// Package vl53l7cx drives the VL53L7CX multizone time-of-flight sensor by
// STMicroelectronics, for its motion indicator in autonomous ranging mode.
//
// It is a port of the parts of ST's ultra lite driver (ULD) 2.0.1 that this
// use needs: initialisation, which uploads the sensor firmware, the ranging
// settings, the motion indicator and detection threshold plugins, and
// reading the motion indicator results. The C sources are kept in
// VL53L7CX_ULD_driver_2.0.1; the functions here follow them step by step,
// and a test compares their I2C traffic with that of the C driver.
//
// Only the motion indicator output is enabled, with one target per zone.
//
// User manual UM3038 and datasheet DS13865: see the docs folder.
package vl53l7cx

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"time"

	"tinygo.org/x/drivers"
)

// Address is the default 7 bit I2C address (0x52 in 8 bit notation).
const Address = 0x29

// Resolutions, in zones.
const (
	Resolution4x4 = 16
	Resolution8x8 = 64
)

// Ranging modes.
const (
	RangingContinuous = 1
	RangingAutonomous = 3
)

// MaxMotionThreshold is the largest motion threshold, which the sensor
// stores multiplied by 65535 in a signed 32 bit value.
const MaxMotionThreshold = 32767

var (
	ErrNotAlive   = errors.New("vl53l7cx: no sensor found")
	ErrTimeout    = errors.New("vl53l7cx: timeout")
	ErrMCU        = errors.New("vl53l7cx: MCU error")
	ErrInvalid    = errors.New("vl53l7cx: invalid parameter")
	ErrDataSize   = errors.New("vl53l7cx: unexpected output data size")
	ErrCorrupted  = errors.New("vl53l7cx: corrupted frame")
	errBufferSize = errors.New("vl53l7cx: temporary buffer too small")
)

const (
	nvmDataSize   = 492
	offsetSize    = 488
	xtalkSize     = 776
	nbTargetsZone = 1

	// bufSize is the size of the ULD's temporary buffer.
	bufSize = 1024

	uiCmdStatus = 0x2C00
	uiCmdStart  = 0x2C04
	uiCmdEnd    = 0x2FFF

	dciZoneConfig    = 0x5450
	dciFreqHz        = 0x5458
	dciIntTime       = 0x545C
	dciRangingMode   = 0xAD30
	dciDSSConfig     = 0xAD38
	dciMotionConfig  = 0xBFAC
	dciSingleRange   = 0xD964
	dciOutputConfig  = 0xD968
	dciOutputEnables = 0xD970
	dciOutputList    = 0xD980
	dciPipeControl   = 0xDB80
	dciGlareFilter   = 0xE108

	motionIdx = 0xD858
)

// outputs lists the block headers of the possible outputs, for one target
// per zone, in the order of the ULD.
var outputs = [12]uint32{
	0x0000000D, // start
	0x54B400C0, // metadata
	0x54C00040, // common data
	0x54D00104, // ambient rate
	0x55D00404, // SPAD count
	0xDB840401, // number of targets detected
	0xDBC40404, // signal rate
	0xDEC40402, // range sigma
	0xDF440402, // distance
	0xE0440401, // reflectance
	0xE0840401, // target status
	0xD85808C0, // motion indicator
}

// outputEnables enables the mandatory outputs (start, metadata and common
// data) and the motion indicator.
var outputEnables = [4]uint32{0x00000007 | 1<<11, 0, 0, 0xC0000000}

// Motion holds the motion indicator results of a frame.
type Motion struct {
	Global1, Global2 uint32
	Status           uint8
	NbDetected       uint8
	NbAggregates     uint8
	// Values holds the motion intensity per aggregate. In resolution 4x4
	// aggregate i is zone i.
	Values [32]uint32
}

// Device is a VL53L7CX on an I2C bus. Its zero value is not usable; use New.
// A Device holds about 2.3 KB of buffers, so keep it off the stack.
type Device struct {
	bus     drivers.I2C
	Address uint16
	// Sleep waits for d; it defaults to time.Sleep.
	Sleep func(d time.Duration)

	offset       [offsetSize]byte
	xtalk        [xtalkSize]byte
	streamCount  uint8
	dataReadSize uint32

	// motion holds the fields of the motion indicator configuration that
	// vary; the rest is constant.
	motion struct {
		resolution    int // 0 until MotionIndicatorInit
		refBinOffset  int32
		featureLength uint8
	}

	// tx holds a register address followed by the ULD's temporary buffer,
	// so that the buffer is written without copying it.
	tx [2 + bufSize]byte
	// small holds the transfers of single bytes.
	small [3]byte
}

// New returns a device on bus at the default address.
func New(bus drivers.I2C) Device {
	return Device{bus: bus, Address: Address}
}

// buf returns the temporary buffer.
func (d *Device) buf() []byte {
	return d.tx[2:]
}

func (d *Device) wait(ms uint32) {
	sleep := d.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	sleep(time.Duration(ms) * time.Millisecond)
}

func (d *Device) wrByte(reg uint16, v byte) error {
	d.small = [3]byte{byte(reg >> 8), byte(reg), v}
	return d.bus.Tx(d.Address, d.small[:], nil)
}

func (d *Device) rdByte(reg uint16) (byte, error) {
	d.small[0], d.small[1] = byte(reg>>8), byte(reg)
	err := d.bus.Tx(d.Address, d.small[:2], d.small[2:3])
	return d.small[2], err
}

func (d *Device) rdMulti(reg uint16, p []byte) error {
	var a [2]byte
	a[0], a[1] = byte(reg>>8), byte(reg)
	return d.bus.Tx(d.Address, a[:], p)
}

// wrBuf writes the first n bytes of the temporary buffer to reg.
func (d *Device) wrBuf(reg uint16, n int) error {
	d.tx[0], d.tx[1] = byte(reg>>8), byte(reg)
	return d.bus.Tx(d.Address, d.tx[:2+n], nil)
}

// wrString writes data, which may be larger than the temporary buffer, to
// reg and the registers following it, in chunks that go through the
// temporary buffer. The data stays in flash. The scheduler runs between
// chunks.
func (d *Device) wrString(reg uint16, data string) error {
	for len(data) > 0 {
		n := copy(d.buf(), data)
		if err := d.wrBuf(reg, n); err != nil {
			return err
		}
		data = data[n:]
		reg += uint16(n)
		runtime.Gosched()
	}
	return nil
}

// swap reverses the byte order of each 32 bit word of b, converting between
// the host format of the ULD and the sensor's.
func swap(b []byte) {
	for i := 0; i+4 <= len(b); i += 4 {
		b[i], b[i+1], b[i+2], b[i+3] = b[i+3], b[i+2], b[i+1], b[i]
	}
}

func (d *Device) pollForAnswer(size, pos int, reg uint16, mask, want byte) error {
	b := d.buf()
	for timeout := 0; ; {
		if err := d.rdMulti(reg, b[:size]); err != nil {
			return err
		}
		d.wait(10)
		if timeout >= 200 {
			return ErrTimeout
		}
		if size >= 4 && b[2] >= 0x7f {
			return ErrMCU
		}
		timeout++
		if b[pos]&mask == want {
			return nil
		}
	}
}

func (d *Device) pollForMCUBoot() error {
	for timeout := 0; timeout < 500; {
		s0, err := d.rdByte(0x06)
		if err != nil {
			return err
		}
		if s0&0x80 != 0 {
			s1, err := d.rdByte(0x07)
			if err != nil {
				return err
			}
			if s1 != 0 {
				return fmt.Errorf("%w %#02x", ErrMCU, s1)
			}
			return nil
		}
		d.wait(1)
		timeout++
		if s0&0x01 != 0 {
			break
		}
	}
	return nil
}

// writes performs register writes of single bytes in order.
func (d *Device) writes(regs ...uint16) error {
	for i := 0; i+1 < len(regs); i += 2 {
		if err := d.wrByte(regs[i], byte(regs[i+1])); err != nil {
			return err
		}
	}
	return nil
}

// IsAlive reports whether a VL53L7CX answers at d.Address.
func (d *Device) IsAlive() (bool, error) {
	if err := d.wrByte(0x7fff, 0x00); err != nil {
		return false, err
	}
	id, err := d.rdByte(0x00)
	if err != nil {
		return false, err
	}
	rev, err := d.rdByte(0x01)
	if err != nil {
		return false, err
	}
	if err := d.wrByte(0x7fff, 0x02); err != nil {
		return false, err
	}
	return id == 0xF0 && rev == 0x02, nil
}

// Init boots the sensor: it uploads the firmware, which takes about 2 s at
// 400 kHz, loads the offset calibration from the sensor's NVM and sends the
// default configuration. The sensor is then in resolution 4x4, at 1 Hz.
func (d *Device) Init() error {
	steps := []func() error{
		// SW reboot sequence
		func() error {
			if err := d.writes(0x7fff, 0x00, 0x0009, 0x04, 0x000F, 0x40, 0x000A, 0x03); err != nil {
				return err
			}
			if _, err := d.rdByte(0x7FFF); err != nil {
				return err
			}
			if err := d.writes(0x000C, 0x01,
				0x0101, 0x00, 0x0102, 0x00, 0x010A, 0x01, 0x4002, 0x01,
				0x4002, 0x00, 0x010A, 0x03, 0x0103, 0x01, 0x000C, 0x00,
				0x000F, 0x43); err != nil {
				return err
			}
			d.wait(1)
			if err := d.writes(0x000F, 0x40, 0x000A, 0x01); err != nil {
				return err
			}
			d.wait(100)
			return nil
		},
		// Wait for the sensor to boot
		func() error {
			if err := d.wrByte(0x7fff, 0x00); err != nil {
				return err
			}
			return d.pollForAnswer(1, 0, 0x06, 0xff, 1)
		},
		func() error {
			if err := d.writes(0x000E, 0x01, 0x7fff, 0x02,
				// Enable FW access
				0x03, 0x0D, 0x7fff, 0x01); err != nil {
				return err
			}
			if err := d.pollForAnswer(1, 0, 0x21, 0x10, 0x10); err != nil {
				return err
			}
			if err := d.wrByte(0x7fff, 0x00); err != nil {
				return err
			}
			// Enable host access to GO1
			if _, err := d.rdByte(0x7fff); err != nil {
				return err
			}
			if err := d.writes(0x0C, 0x01,
				// Power ON status
				0x7fff, 0x00, 0x101, 0x00, 0x102, 0x00, 0x010A, 0x01,
				0x4002, 0x01, 0x4002, 0x00, 0x010A, 0x03, 0x103, 0x01,
				0x400F, 0x00, 0x21A, 0x43, 0x21A, 0x03, 0x21A, 0x01,
				0x21A, 0x00, 0x219, 0x00, 0x21B, 0x00,
				// Wake up MCU
				0x7fff, 0x00); err != nil {
				return err
			}
			if _, err := d.rdByte(0x7fff); err != nil {
				return err
			}
			return d.writes(0x0C, 0x00, 0x7fff, 0x01, 0x20, 0x07, 0x20, 0x06)
		},
		// Download the firmware into the VL53L7CX
		func() error {
			for i, page := range [...]uint16{0x09, 0x0a, 0x0b} {
				if err := d.wrByte(0x7fff, byte(page)); err != nil {
					return err
				}
				end := min((i+1)*0x8000, len(firmware))
				if err := d.wrString(0, firmware[i*0x8000:end]); err != nil {
					return err
				}
			}
			// Check that the firmware was downloaded correctly
			if err := d.writes(0x7fff, 0x01, 0x7fff, 0x02, 0x03, 0x0D, 0x7fff, 0x01); err != nil {
				return err
			}
			return d.pollForAnswer(1, 0, 0x21, 0x10, 0x10)
		},
		// Reset the MCU and wait for it to boot
		func() error {
			if err := d.wrByte(0x7fff, 0x00); err != nil {
				return err
			}
			if _, err := d.rdByte(0x7fff); err != nil {
				return err
			}
			if err := d.writes(0x0C, 0x01, 0x7FFF, 0x00, 0x114, 0x00, 0x115, 0x00,
				0x116, 0x42, 0x117, 0x00, 0x0B, 0x00); err != nil {
				return err
			}
			if _, err := d.rdByte(0x7fff); err != nil {
				return err
			}
			if err := d.writes(0x0C, 0x00, 0x0B, 0x01); err != nil {
				return err
			}
			return d.pollForMCUBoot()
		},
		// Get the offset NVM data and send it
		func() error {
			if err := d.wrByte(0x7fff, 0x02); err != nil {
				return err
			}
			if err := d.wrString(0x2fd8, getNVMCmd); err != nil {
				return err
			}
			if err := d.pollForAnswer(4, 0, uiCmdStatus, 0xff, 2); err != nil {
				return err
			}
			if err := d.rdMulti(uiCmdStart, d.buf()[:nvmDataSize]); err != nil {
				return err
			}
			copy(d.offset[:], d.buf())
			return d.sendOffsetData(Resolution4x4)
		},
		// Send the default Xtalk shape
		func() error {
			copy(d.xtalk[:], defaultXtalk)
			return d.sendXtalkData(Resolution4x4)
		},
		// Send the default configuration
		func() error {
			if err := d.wrString(0x2c34, defaultConfiguration); err != nil {
				return err
			}
			if err := d.pollForAnswer(4, 1, uiCmdStatus, 0xff, 0x03); err != nil {
				return err
			}
			pipeCtrl := [4]byte{nbTargetsZone, 0x00, 0x01, 0x00}
			if err := d.dciWrite(dciPipeControl, pipeCtrl[:]); err != nil {
				return err
			}
			var singleRange [4]byte
			binary.LittleEndian.PutUint32(singleRange[:], 1)
			if err := d.dciWrite(dciSingleRange, singleRange[:]); err != nil {
				return err
			}
			if err := d.dciReplace(dciGlareFilter, 40, []byte{1}, 0x26); err != nil {
				return err
			}
			return d.dciReplace(dciGlareFilter, 40, []byte{1}, 0x25)
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// averageGrid replaces the first 16 of the 64 words of the 8x8 grid at g,
// whose words have size bytes, by the averages of the 2x2 blocks of zones,
// and zeroes the other 48. It follows the ULD's 4x4 extrapolation.
func averageGrid(g []byte, size int) {
	get := func(i int) int64 {
		if size == 4 {
			return int64(binary.LittleEndian.Uint32(g[4*i:]))
		}
		return int64(int16(binary.LittleEndian.Uint16(g[2*i:])))
	}
	put := func(i int, v int64) {
		if size == 4 {
			binary.LittleEndian.PutUint32(g[4*i:], uint32(v))
		} else {
			binary.LittleEndian.PutUint16(g[2*i:], uint16(int16(v)))
		}
	}
	for j := 0; j < 4; j++ {
		for i := 0; i < 4; i++ {
			k := 2*i + 16*j
			sum := get(k) + get(k+1) + get(k+8) + get(k+9)
			if size == 4 {
				// unsigned 32 bit arithmetic, as in C
				put(i+4*j, int64(uint32(sum)/4))
			} else {
				put(i+4*j, sum/4)
			}
		}
	}
	clear(g[16*size : 64*size])
}

func (d *Device) sendOffsetData(resolution int) error {
	b := d.buf()
	copy(b, d.offset[:])

	// Data extrapolation is required for 4x4 offset
	if resolution == Resolution4x4 {
		dss := [8]byte{0x0F, 0x04, 0x04, 0x00, 0x08, 0x10, 0x10, 0x07}
		copy(b[0x10:], dss[:])
		swap(b[:offsetSize])
		averageGrid(b[0x3C:], 4)
		averageGrid(b[0x140:], 2)
		swap(b[:offsetSize])
	}

	copy(b, b[8:offsetSize+4])
	footer := [8]byte{0x00, 0x00, 0x00, 0x0F, 0x03, 0x01, 0x01, 0xE4}
	copy(b[0x1E0:], footer[:])
	if err := d.wrBuf(0x2e18, offsetSize); err != nil {
		return err
	}
	return d.pollForAnswer(4, 1, uiCmdStatus, 0xff, 0x03)
}

func (d *Device) sendXtalkData(resolution int) error {
	b := d.buf()
	copy(b, d.xtalk[:])

	// Data extrapolation is required for 4x4 Xtalk
	if resolution == Resolution4x4 {
		res := [8]byte{0x0F, 0x04, 0x04, 0x17, 0x08, 0x10, 0x10, 0x07}
		dss := [8]byte{0x00, 0x78, 0x00, 0x08, 0x00, 0x00, 0x00, 0x08}
		profile := [4]byte{0xA0, 0xFC, 0x01, 0x00}
		copy(b[0x08:], res[:])
		copy(b[0x20:], dss[:])
		swap(b[:xtalkSize])
		averageGrid(b[0x34:], 4)
		swap(b[:xtalkSize])
		copy(b[0x134:], profile[:])
		clear(b[0x78:0x7C])
	}

	if err := d.wrBuf(0x2cf8, xtalkSize); err != nil {
		return err
	}
	return d.pollForAnswer(4, 1, uiCmdStatus, 0xff, 0x03)
}

// dciRead reads size bytes of firmware data at index into the start of the
// temporary buffer, in host format.
func (d *Device) dciRead(index uint16, size int) error {
	if size+12 > bufSize {
		return errBufferSize
	}
	b := d.buf()
	cmd := [12]byte{byte(index >> 8), byte(index),
		byte((size & 0xff0) >> 4), byte((size & 0xf) << 4),
		0x00, 0x00, 0x00, 0x0f, 0x00, 0x02, 0x00, 0x08}
	copy(b, cmd[:])

	// Request data reading from FW
	if err := d.wrBuf(uiCmdEnd-11, len(cmd)); err != nil {
		return err
	}
	if err := d.pollForAnswer(4, 1, uiCmdStatus, 0xff, 0x03); err != nil {
		return err
	}

	// Read the data sent (4 bytes header + data + 8 bytes footer)
	if err := d.rdMulti(uiCmdStart, b[:size+12]); err != nil {
		return err
	}
	swap(b[:size+12])
	copy(b, b[4:4+size])
	return nil
}

// dciWrite writes data, in host format, to the firmware at index. data may
// be the start of the temporary buffer.
func (d *Device) dciWrite(index uint16, data []byte) error {
	size := len(data)
	if size+12 > bufSize {
		return errBufferSize
	}
	b := d.buf()
	copy(b[4:], data)
	return d.dciWriteBuf(index, size)
}

// dciWriteBuf writes the size bytes at offset 4 of the temporary buffer, in
// host format, to the firmware at index.
func (d *Device) dciWriteBuf(index uint16, size int) error {
	b := d.buf()
	swap(b[4 : 4+size])
	b[0], b[1] = byte(index>>8), byte(index)
	b[2], b[3] = byte((size&0xff0)>>4), byte((size&0xf)<<4)
	footer := [8]byte{0x00, 0x00, 0x00, 0x0f, 0x05, 0x01,
		byte((size + 8) >> 8), byte(size + 8)}
	copy(b[4+size:], footer[:])

	if err := d.wrBuf(uint16(uiCmdEnd-(size+12)+1), size+12); err != nil {
		return err
	}
	return d.pollForAnswer(4, 1, uiCmdStatus, 0xff, 0x03)
}

// dciReplace reads size bytes at index, replaces those at pos by data and
// writes them back.
func (d *Device) dciReplace(index uint16, size int, data []byte, pos int) error {
	if err := d.dciRead(index, size); err != nil {
		return err
	}
	copy(d.buf()[pos:], data)
	return d.dciWrite(index, d.buf()[:size])
}

// SetResolution sets the resolution to Resolution4x4 or Resolution8x8. It
// must be called before SetRangingFrequency.
func (d *Device) SetResolution(resolution int) error {
	b := d.buf()
	var dss, zones, size byte
	switch resolution {
	case Resolution4x4:
		dss, zones, size = 64, 4, 8
	case Resolution8x8:
		dss, zones, size = 16, 8, 4
	default:
		return ErrInvalid
	}
	if err := d.dciRead(dciDSSConfig, 16); err != nil {
		return err
	}
	b[0x04], b[0x06] = dss, dss
	b[0x09] = 4
	if resolution == Resolution8x8 {
		b[0x09] = 1
	}
	if err := d.dciWrite(dciDSSConfig, b[:16]); err != nil {
		return err
	}
	if err := d.dciRead(dciZoneConfig, 8); err != nil {
		return err
	}
	b[0x00], b[0x01] = zones, zones
	b[0x04], b[0x05] = size, size
	if err := d.dciWrite(dciZoneConfig, b[:8]); err != nil {
		return err
	}
	if err := d.sendOffsetData(resolution); err != nil {
		return err
	}
	return d.sendXtalkData(resolution)
}

// SetRangingFrequency sets the frame rate: 1 to 60 Hz in resolution 4x4, 1
// to 15 Hz in 8x8. Only the 4x4 range is checked.
func (d *Device) SetRangingFrequency(hz uint8) error {
	if hz < 1 || hz > 60 {
		return ErrInvalid
	}
	return d.dciReplace(dciFreqHz, 4, []byte{hz}, 0x01)
}

// SetIntegrationTime sets the time the VCSEL emits per frame in autonomous
// mode, from 2 to 1000 ms.
func (d *Device) SetIntegrationTime(ms uint32) error {
	if ms < 2 || ms > 1000 {
		return ErrInvalid
	}
	var v [4]byte
	binary.LittleEndian.PutUint32(v[:], ms*1000)
	return d.dciReplace(dciIntTime, 20, v[:], 0x00)
}

// SetRangingMode sets RangingContinuous or RangingAutonomous.
func (d *Device) SetRangingMode(mode int) error {
	var mode1, mode3 byte
	var singleRange uint32
	switch mode {
	case RangingContinuous:
		mode1, mode3, singleRange = 0x1, 0x3, 0x00
	case RangingAutonomous:
		mode1, mode3, singleRange = 0x3, 0x2, 0x01
	default:
		return ErrInvalid
	}
	if err := d.dciRead(dciRangingMode, 8); err != nil {
		return err
	}
	b := d.buf()
	b[0x01], b[0x03] = mode1, mode3
	if err := d.dciWrite(dciRangingMode, b[:8]); err != nil {
		return err
	}
	var v [4]byte
	binary.LittleEndian.PutUint32(v[:], singleRange)
	return d.dciWrite(dciSingleRange, v[:])
}

// StartRanging starts a ranging session. The settings cannot be changed
// while it runs.
func (d *Device) StartRanging() error {
	// The ULD reads the resolution, which only sizes per zone outputs.
	if err := d.dciRead(dciZoneConfig, 8); err != nil {
		return err
	}
	d.dataReadSize = 0
	d.streamCount = 255

	for i := range outputs {
		if outputs[i] == 0 || outputEnables[i/32]&(1<<(i%32)) == 0 {
			continue
		}
		typ, size := outputs[i]&0xf, (outputs[i]>>4)&0xfff
		if typ >= 0x1 && typ < 0x0d {
			// Per zone outputs are resized to the resolution; none is
			// enabled.
			return ErrInvalid
		}
		d.dataReadSize += size + 4
	}
	d.dataReadSize += 24

	b := d.buf()
	for k, v := range outputs {
		binary.LittleEndian.PutUint32(b[4+4*k:], v)
	}
	if err := d.dciWriteBuf(dciOutputList, 4*len(outputs)); err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(b[4:], d.dataReadSize)
	binary.LittleEndian.PutUint32(b[8:], uint32(len(outputs)+1))
	if err := d.dciWriteBuf(dciOutputConfig, 8); err != nil {
		return err
	}
	for k, v := range outputEnables {
		binary.LittleEndian.PutUint32(b[4+4*k:], v)
	}
	if err := d.dciWriteBuf(dciOutputEnables, 4*len(outputEnables)); err != nil {
		return err
	}

	// Start xshut bypass (interrupt mode)
	if err := d.writes(0x7fff, 0x00, 0x09, 0x05, 0x7fff, 0x02); err != nil {
		return err
	}

	// Start the ranging session
	cmd := [4]byte{0x00, 0x03, 0x00, 0x00}
	copy(b, cmd[:])
	if err := d.wrBuf(uiCmdEnd-(4-1), len(cmd)); err != nil {
		return err
	}
	if err := d.pollForAnswer(4, 1, uiCmdStatus, 0xff, 0x03); err != nil {
		return err
	}

	// Check that the firmware agrees on the data size
	if err := d.dciRead(0x5440, 12); err != nil {
		return err
	}
	if uint32(binary.LittleEndian.Uint16(b[0x8:])) != d.dataReadSize {
		return ErrDataSize
	}
	return nil
}

// StopRanging stops the ranging session.
func (d *Device) StopRanging() error {
	b := d.buf()
	if err := d.rdMulti(0x2FFC, b[:4]); err != nil {
		return err
	}
	if binary.LittleEndian.Uint32(b) != 0x4FF {
		if err := d.writes(0x7fff, 0x00,
			// Provoke MCU stop
			0x15, 0x16, 0x14, 0x01); err != nil {
			return err
		}
		// Poll for G02 status 0 MCU stop
		for timeout, tmp := 0, byte(0); tmp&0x80 == 0; {
			var err error
			if tmp, err = d.rdByte(0x6); err != nil {
				return err
			}
			d.wait(10)
			timeout++
			if timeout > 500 {
				return ErrTimeout
			}
		}
	}

	// Check GO2 status 1 if status is still OK
	tmp, err := d.rdByte(0x6)
	if err != nil {
		return err
	}
	if tmp&0x80 != 0 {
		tmp, err := d.rdByte(0x7)
		if err != nil {
			return err
		}
		if tmp != 0x84 && tmp != 0x85 {
			return fmt.Errorf("%w %#02x", ErrMCU, tmp)
		}
	}

	// Undo MCU stop, and stop xshut bypass
	return d.writes(0x7fff, 0x00, 0x14, 0x00, 0x15, 0x00, 0x09, 0x04, 0x7fff, 0x02)
}

// Motion reads the motion indicator results of the latest frame.
func (d *Device) Motion() (Motion, error) {
	var m Motion
	size := int(d.dataReadSize)
	if size < 24 || size > bufSize {
		return m, ErrInvalid
	}
	b := d.buf()
	if err := d.rdMulti(0x0, b[:size]); err != nil {
		return m, err
	}
	d.streamCount = b[0]
	swap(b[:size])

	// Block headers follow the 16 byte frame header.
	for i := 16; i < size; i += 4 {
		bh := binary.LittleEndian.Uint32(b[i:])
		typ, msize, idx := bh&0xf, int(bh>>4)&0xfff, uint16(bh>>16)
		if typ > 0x1 && typ < 0xd {
			msize *= int(typ)
		}
		if idx == motionIdx && i+4+140 <= size {
			p := b[i+4:]
			m.Global1 = binary.LittleEndian.Uint32(p)
			m.Global2 = binary.LittleEndian.Uint32(p[4:])
			m.Status, m.NbDetected, m.NbAggregates = p[8], p[9], p[10]
			for k := range m.Values {
				m.Values[k] = binary.LittleEndian.Uint32(p[12+4*k:]) / 65535
			}
		}
		i += msize
	}

	// Matching header and footer ids detect corrupted frames.
	if b[0x8] != b[size-4] || b[0x9] != b[size-3] {
		return m, ErrCorrupted
	}
	return m, nil
}
