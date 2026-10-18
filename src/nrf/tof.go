//go:build tinygo

package main

import (
	"fmt"
	"io"
	"knixx/bus"
	"knixx/cli"
	"knixx/internal/vl53l7cx"
	"knixx/led"
	"knixx/thunder"
	"machine"
	"sync/atomic"
	"time"
)

// VL53L7CX pins on the thunder board, on the Feather's SPI header.
const (
	tofInt = machine.P0_14 // SCK: INT, open drain, 47 kΩ pull-up
	tofLPn = machine.P0_13 // MOSI: LPn, high enables I2C
	tofRst = machine.P0_15 // MISO: I2C_RST, active high
)

const (
	// tofPoll is the period at which motion interrupts are picked up and
	// setting changes applied.
	tofPoll = 50 * time.Millisecond
	// tofRetry delays a new attempt after the sensor failed.
	tofRetry = time.Minute
)

var (
	tof vl53l7cx.Device

	// tofPulses counts the INT pulses; the interrupt handler only
	// increments it.
	tofPulses atomic.Uint32

	// tofStatus is what the tof command reports. Only runToF writes it.
	tofStatus struct {
		err      error
		ranging  bool
		bootTime time.Duration
		// applied holds the ranging settings the sensor runs with.
		applied rangingSettings
		motions uint32
		// lastMotion is the uptime of the last motion, and motion the
		// per zone intensities of its frame.
		lastMotion time.Duration
		motion     vl53l7cx.Motion
	}
)

// rangingSettings are the settings of dev.Occupancy the sensor is programmed
// with.
type rangingSettings struct {
	threshold, frequency, integration uint16
	minDistance, maxDistance          uint16
}

// settingsOf returns the settings of dev. An invalid distance window is
// replaced by the one of last, or by the default one when last has none.
func settingsOf(dev *thunder.Device, last rangingSettings) rangingSettings {
	o := &dev.Occupancy
	s := rangingSettings{o.Threshold, o.Frequency, o.Integration, o.MinDistance, o.MaxDistance}
	if !o.DistanceValid() {
		s.minDistance, s.maxDistance = last.minDistance, last.maxDistance
		if s.minDistance == 0 {
			s.minDistance, s.maxDistance = thunder.DefaultMinDistance, thunder.DefaultMaxDistance
		}
	}
	return s
}

// runToF runs the VL53L7CX in autonomous ranging mode at resolution 4x4, with
// the ranging frequency, integration time and distance window of
// dev.Occupancy and detection thresholds on its motion indicator: the sensor pulses INT for each frame
// in which a zone's motion exceeds dev.Occupancy.Threshold, and each pulse
// queues a CmdMotion on cmds.
//
// A change of the settings by the console is picked up by polling dev,
// which is safe on the single core, cooperative scheduler: the console
// changes it between yields.
func runToF(dev *thunder.Device, cmds chan<- bus.Command) {
	tofLPn.Configure(machine.PinConfig{Mode: machine.PinOutput})
	tofLPn.High()
	tofRst.Configure(machine.PinConfig{Mode: machine.PinOutput})
	tofRst.Low()
	tofInt.Configure(machine.PinConfig{Mode: machine.PinInput})
	if err := tofInt.SetInterrupt(machine.PinFalling, func(machine.Pin) {
		tofPulses.Add(1)
	}); err != nil {
		endInit()
		fail("vl53l7cx: interrupt:", err)
		return
	}
	tof = vl53l7cx.New(machine.I2C1)

	for first := true; ; time.Sleep(tofRetry) {
		s := settingsOf(dev, tofStatus.applied)
		err := startToF(s)
		if first {
			// The device has started once the sensor ranges or failed to.
			first = false
			endInit()
		}
		if err != nil {
			tofStatus.err, tofStatus.ranging = err, false
			fail("vl53l7cx:", err)
			continue
		}
		tofStatus.err, tofStatus.ranging, tofStatus.applied = nil, true, s
		if err := watchToF(dev, cmds); err != nil {
			tofStatus.err, tofStatus.ranging = err, false
			fail("vl53l7cx:", err)
		}
	}
}

// startToF resets the sensor's I2C interface, boots and configures the
// sensor and starts ranging.
func startToF(s rangingSettings) error {
	tofRst.High()
	time.Sleep(time.Millisecond)
	tofRst.Low()
	time.Sleep(time.Millisecond)

	start := time.Now()
	alive, err := tof.IsAlive()
	if err != nil {
		return err
	}
	if !alive {
		return vl53l7cx.ErrNotAlive
	}
	if err := tof.Init(); err != nil {
		return err
	}
	tofStatus.bootTime = time.Since(start)
	for _, step := range []func() error{
		func() error { return tof.SetResolution(vl53l7cx.Resolution4x4) },
		func() error { return tof.SetRangingFrequency(uint8(s.frequency)) },
		func() error { return tof.SetRangingMode(vl53l7cx.RangingAutonomous) },
		func() error { return tof.SetIntegrationTime(uint32(s.integration)) },
		func() error { return tof.MotionIndicatorInit(vl53l7cx.Resolution4x4) },
		func() error { return tof.SetMotionDistance(s.minDistance, s.maxDistance) },
		func() error { return tof.SetMotionThresholds(s.threshold) },
		func() error { return tof.EnableDetectionThresholds(true) },
	} {
		if err := step(); err != nil {
			return err
		}
	}
	tofPulses.Store(0)
	return tof.StartRanging()
}

// watchToF turns INT pulses into motion commands and applies setting
// changes, until the sensor fails.
func watchToF(dev *thunder.Device, cmds chan<- bus.Command) error {
	var seen uint32
	for {
		time.Sleep(tofPoll)

		if n := tofPulses.Load(); n != seen {
			tofStatus.motions += n - seen
			seen = n
			tofStatus.lastMotion = time.Since(boot)
			signal(led.Occupancy)
			select {
			case cmds <- bus.Command{Kind: bus.CmdMotion}:
			default:
				fail("tx queue full")
			}
			// The frame that raised INT, for the tof command.
			if m, err := tof.Motion(); err == nil {
				tofStatus.motion = m
			}
		}

		// Settings cannot change while ranging.
		if s := settingsOf(dev, tofStatus.applied); s != tofStatus.applied {
			if err := applyToF(tofStatus.applied, s); err != nil {
				return err
			}
			tofStatus.applied = s
		}
	}
}

// applyToF stops ranging, programs the settings of s that differ from old
// and starts ranging again.
func applyToF(old, s rangingSettings) error {
	if err := tof.StopRanging(); err != nil {
		return err
	}
	if s.frequency != old.frequency {
		if err := tof.SetRangingFrequency(uint8(s.frequency)); err != nil {
			return err
		}
	}
	if s.integration != old.integration {
		if err := tof.SetIntegrationTime(uint32(s.integration)); err != nil {
			return err
		}
	}
	if s.minDistance != old.minDistance || s.maxDistance != old.maxDistance {
		if err := tof.SetMotionDistance(s.minDistance, s.maxDistance); err != nil {
			return err
		}
	}
	if s.threshold != old.threshold {
		if err := tof.SetMotionThresholds(s.threshold); err != nil {
			return err
		}
	}
	return tof.StartRanging()
}

// tofCmd reports the state of the VL53L7CX on the console.
func tofCmd(dev *thunder.Device) cli.Cmd {
	return cli.Cmd{
		Name: "tof",
		Help: "show the time-of-flight sensor state and its latest motion",
		Func: func(args []string, w io.Writer) {
			if len(args) != 0 {
				fmt.Fprintln(w, "usage: tof")
				return
			}
			s := &tofStatus
			switch {
			case s.err != nil:
				fmt.Fprintln(w, "error:", s.err)
				return
			case !s.ranging:
				fmt.Fprintln(w, "starting")
				return
			}
			occupied := "no"
			if bus.Occupied.Load() {
				occupied = "yes"
			}
			a := s.applied
			fmt.Fprintf(w, "ranging 4x4 at %d Hz, integration %d ms, threshold %d, booted in %d ms\n",
				a.frequency, a.integration, a.threshold, s.bootTime.Milliseconds())
			if (thunder.Occupancy{Frequency: a.frequency, Integration: a.integration}).PeriodStretched() {
				fmt.Fprintln(w, "warning: integration time + 1 ms exceeds the ranging period; the sensor lowers the frame rate")
			}
			fmt.Fprintf(w, "motion window %d-%d mm\n", a.minDistance, a.maxDistance)
			if !dev.Occupancy.DistanceValid() {
				fmt.Fprintf(w, "warning: min_distance/max_distance invalid (need min < max, span <= %d mm); keeping %d-%d mm\n",
					thunder.MaxMotionSpan, a.minDistance, a.maxDistance)
			}
			fmt.Fprintf(w, "occupied %s, %d motion frames\n", occupied, s.motions)
			if s.motions == 0 {
				return
			}
			fmt.Fprintf(w, "last motion %d s ago, per zone:\n",
				int64((time.Since(boot)-s.lastMotion)/time.Second))
			for row := 0; row < 4; row++ {
				for col := 0; col < 4; col++ {
					fmt.Fprintf(w, " %6d", s.motion.Values[4*row+col])
				}
				fmt.Fprintln(w)
			}
		},
	}
}
