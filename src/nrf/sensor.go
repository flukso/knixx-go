//go:build tinygo

package main

import (
	"fmt"
	"io"
	"knixx/bus"
	"knixx/cli"
	"knixx/internal/sht2x"
	"knixx/internal/vcnl4040"
	"time"
)

const (
	sensorInterval = time.Minute
	// sensorFirst delays the first reading after boot.
	sensorFirst = 5 * time.Second
)

var (
	boot = time.Now()

	sensor           sht2x.Device
	sensorConfigured bool

	light           vcnl4040.Device
	lightConfigured bool

	// The latest readings, for the sensor command. at is the uptime at which
	// the temperature measurement was triggered, lightAt the one at which
	// the illuminance measurement was.
	reading struct {
		valid       bool
		at          time.Duration
		temperature int32 // m°C
		humidity    int32 // c%

		lightValid bool
		lightAt    time.Duration
		lux10      uint32 // dlx
	}
)

// runSensor reads the SHT20 and the ambient light sensor of the VCNL4040 on
// I2C1, see configureI2C, every sensorInterval and queues the readings on
// cmds for publication on the bus.
//
// The readings follow a time.Ticker, which TinyGo re-arms at an absolute
// deadline (when += period): the measurement time and any delay in handling
// a tick do not accumulate, so the readings stay sensorInterval apart.
func runSensor(cmds chan<- bus.Command) {
	sensor = sht2x.New(i2c1)
	light = vcnl4040.New(i2c1)

	time.Sleep(sensorFirst)
	ticker := time.NewTicker(sensorInterval)
	defer ticker.Stop()
	for {
		measureClimate(cmds)
		measureLight(cmds)
		<-ticker.C
	}
}

// measureClimate reads the SHT20 and queues the reading. A sensor that
// cannot be configured or read is reported and retried at the next tick.
func measureClimate(cmds chan<- bus.Command) {
	if !sensorConfigured {
		if err := sensor.Configure(); err != nil {
			fail("sht20:", err)
			return
		}
		sensorConfigured = true
	}

	at := time.Since(boot)
	t, err := sensor.ReadTemperature()
	if err != nil {
		fail("sht20: temperature:", err)
		return
	}
	h, err := sensor.ReadHumidity()
	if err != nil {
		fail("sht20: humidity:", err)
		return
	}
	reading.valid, reading.at, reading.temperature, reading.humidity = true, at, t, h

	select {
	case cmds <- bus.Command{Kind: bus.CmdSensor, Temperature: t, Humidity: h}:
	default:
		fail("tx queue full")
	}
}

// measureLight takes a single ALS measurement, which powers the ALS on for
// about 160 ms, and queues it. A sensor that cannot be configured or read is
// reported, and configured again at the next tick.
func measureLight(cmds chan<- bus.Command) {
	if !lightConfigured {
		if err := light.Configure(); err != nil {
			fail("vcnl4040:", err)
			return
		}
		lightConfigured = true
	}

	at := time.Since(boot)
	lux10, err := light.ReadIlluminance()
	if err != nil {
		lightConfigured = false
		fail("vcnl4040:", err)
		return
	}
	reading.lightValid, reading.lightAt, reading.lux10 = true, at, lux10

	select {
	case cmds <- bus.Command{Kind: bus.CmdLight, Illuminance: int32(lux10)}:
	default:
		fail("tx queue full")
	}
}

// sensorCmd reports the latest reading on the console.
func sensorCmd() cli.Cmd {
	return cli.Cmd{
		Name: "sensor",
		Help: "show the latest temperature, humidity and illuminance reading",
		Func: func(args []string, w io.Writer) {
			if len(args) != 0 {
				fmt.Fprintln(w, "usage: sensor")
				return
			}
			now := time.Since(boot)
			if !reading.valid {
				fmt.Fprintln(w, "no temperature and humidity reading yet")
			} else {
				fmt.Fprintf(w, "temperature %.2f C, humidity %.2f %%RH at %d ms (%d s ago)\n",
					float32(reading.temperature)/1000, float32(reading.humidity)/100,
					reading.at.Milliseconds(), int64((now-reading.at)/time.Second))
			}
			if !reading.lightValid {
				fmt.Fprintln(w, "no illuminance reading yet")
				return
			}
			saturated := ""
			if reading.lux10 >= vcnl4040.MaxLux10 {
				saturated = " (saturated)"
			}
			fmt.Fprintf(w, "illuminance %d.%d lx%s at %d ms (%d s ago)\n",
				reading.lux10/10, reading.lux10%10, saturated,
				reading.lightAt.Milliseconds(), int64((now-reading.lightAt)/time.Second))
		},
	}
}
