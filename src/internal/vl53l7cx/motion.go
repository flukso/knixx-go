package vl53l7cx

import "encoding/binary"

// Firmware data indexes of the motion indicator and detection thresholds
// plugins.
const (
	dciDetThreshConfig       = 0x5488
	dciDetThreshGlobalConfig = 0xB6E0
	dciDetThreshStart        = 0xB6E8
	dciDetThreshValidStatus  = 0xB9F0

	nbThresholds   = 64
	thresholdSize  = 12
	lastThreshold  = 128
	measureMotion  = 19
	greaterThanMax = 3

	// motionConfigSize is the size of VL53L7CX_Motion_Configuration.
	motionConfigSize = 156
)

// Limits of the motion indicator's distance window, in mm.
const (
	MinMotionDistance = 400
	MaxMotionDistance = 4000
	// MaxMotionSpan is the widest window.
	MaxMotionSpan = 1500
)

// MotionIndicatorInit programs the motion indicator for resolution, with
// the ULD's default settings: motion is detected between 400 and 1500 mm.
// The resolution must match the one set with SetResolution.
func (d *Device) MotionIndicatorInit(resolution int) error {
	if resolution != Resolution4x4 && resolution != Resolution8x8 {
		return ErrInvalid
	}
	d.motion.resolution = resolution
	d.motion.refBinOffset = 13633
	d.motion.featureLength = 9
	return d.writeMotionConfig()
}

// ValidMotionDistance reports whether SetMotionDistance accepts the window
// from min to max mm.
func ValidMotionDistance(min, max uint16) bool {
	return min >= MinMotionDistance && max <= MaxMotionDistance && max > min &&
		max-min <= MaxMotionSpan
}

// motionFields computes the ref_bin_offset and feature_length of the window
// from min to max mm like the ULD, in single precision. The explicit
// conversions round each step, as C's float_t does, and prevent fusing.
func motionFields(min, max uint16) (int32, uint8) {
	t := float32(float32(min) / float32(37.5348))
	t = float32(t - float32(4.0))
	t = float32(t * float32(2048.5))
	refBinOffset := int32(t)

	t = float32(float32(max) - float32(min))
	t = float32(t / float32(10.0))
	t = float32(t + float32(30.02784))
	t = float32(t / float32(15.01392))
	t = float32(t + float32(0.5))
	return refBinOffset, uint8(t)
}

// SetMotionDistance sets the distance window of the motion indicator to min
// to max mm, see ValidMotionDistance. It must be called after
// MotionIndicatorInit.
func (d *Device) SetMotionDistance(min, max uint16) error {
	if d.motion.resolution == 0 || !ValidMotionDistance(min, max) {
		return ErrInvalid
	}
	d.motion.refBinOffset, d.motion.featureLength = motionFields(min, max)
	return d.writeMotionConfig()
}

// writeMotionConfig writes the motion indicator configuration.
func (d *Device) writeMotionConfig() error {
	// The VL53L7CX_Motion_Configuration structure, in host format, is built
	// in place in the temporary buffer.
	cfg := d.buf()[4 : 4+motionConfigSize]
	clear(cfg)
	binary.LittleEndian.PutUint32(cfg[0:], uint32(d.motion.refBinOffset)) // ref_bin_offset
	binary.LittleEndian.PutUint32(cfg[4:], 2883584)                       // detection_threshold
	// extra_noise_sigma and null_den_clip_value stay 0.
	copy(cfg[16:], []byte{
		6,                      // mem_update_mode
		2,                      // mem_update_choice
		4,                      // sum_span
		d.motion.featureLength, // feature_length
		16,                     // nb_of_aggregates
		16,                     // nb_of_temporal_accumulations
		1,                      // min_nb_for_global_detection
		8,                      // global_indicator_format_1
		// global_indicator_format_2 and the spares stay 0.
	})

	mapID := cfg[28 : 28+64]
	if d.motion.resolution == Resolution4x4 {
		for i := range mapID {
			if i < Resolution4x4 {
				mapID[i] = byte(i)
			} else {
				mapID[i] = 0xff // -1
			}
		}
	} else {
		for i := range mapID {
			mapID[i] = byte((i%8)/2 + 4*(i/16))
		}
	}
	// indicator_format_1 and indicator_format_2 stay 0.
	return d.dciWriteBuf(dciMotionConfig, motionConfigSize)
}

// SetMotionThresholds programs a detection threshold on the motion
// indicator of each of the 16 zones of resolution 4x4: once detection
// thresholds are enabled, the sensor pulls its INT pin low for a frame in
// which the motion of any zone exceeds threshold, 1 to MaxMotionThreshold.
func (d *Device) SetMotionThresholds(threshold uint16) error {
	if threshold < 1 || threshold > MaxMotionThreshold {
		return ErrInvalid
	}

	// Set the valid target list
	valid := [8]byte{0x05, 0x05, 0x05, 0x05, 0x05, 0x05, 0x05, 0x05}
	if err := d.dciWrite(dciDetThreshValidStatus, valid[:]); err != nil {
		return err
	}

	// The array of VL53L7CX_DetectionThresholds, in host format, is built in
	// place in the temporary buffer.
	t := d.buf()[4 : 4+nbThresholds*thresholdSize]
	clear(t)
	v := uint32(threshold) * 65535
	for zone := 0; zone < Resolution4x4; zone++ {
		e := t[zone*thresholdSize:]
		binary.LittleEndian.PutUint32(e[0:], v) // param_low_thresh
		binary.LittleEndian.PutUint32(e[4:], v) // param_high_thresh
		e[8] = measureMotion
		e[9] = greaterThanMax
		e[10] = byte(zone)
		if zone == Resolution4x4-1 {
			e[10] |= lastThreshold
		}
		e[11] = 0 // no mathematic operation
	}
	return d.dciWriteBuf(dciDetThreshStart, len(t))
}

// EnableDetectionThresholds enables or disables the interrupt on the
// programmed thresholds. When disabled, INT pulses on every frame.
func (d *Device) EnableDetectionThresholds(enable bool) error {
	global := [4]byte{0x01, 0x00, 0x01, 0x00}
	cfg := byte(0x0C)
	if enable {
		global[1] = 0x01
		cfg = 0x04
	}

	// Set the global interrupt config
	if err := d.dciReplace(dciDetThreshGlobalConfig, 8, global[:], 0x00); err != nil {
		return err
	}
	// Update the interrupt config
	return d.dciReplace(dciDetThreshConfig, 20, []byte{cfg}, 0x11)
}
