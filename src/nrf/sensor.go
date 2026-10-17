//go:build tinygo

package main

import (
	"fmt"
	"io"
	"knixx/bus"
	"knixx/cli"
	"knixx/internal/sht2x"
	"machine"
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

	// The latest reading, for the sensor command. at is the uptime at which
	// the temperature measurement was triggered.
	reading struct {
		valid       bool
		at          time.Duration
		temperature int32 // m°C
		humidity    int32 // c%
	}
)

// runSensor reads the SHT20 every sensorInterval and queues the reading on
// cmds for publication on the bus.
//
// The readings follow a time.Ticker, which TinyGo re-arms at an absolute
// deadline (when += period): the measurement time and any delay in handling
// a tick do not accumulate, so the readings stay sensorInterval apart.
func runSensor(cmds chan<- bus.Command) {
	// I2C0 shares its peripheral instance with SPI0, which drives the flash.
	if err := machine.I2C1.Configure(machine.I2CConfig{
		SDA:       machine.SDA_PIN,
		SCL:       machine.SCL_PIN,
		Frequency: 100 * machine.KHz,
	}); err != nil {
		fmt.Fprintln(console, "i2c:", err)
		return
	}
	sensor = sht2x.New(machine.I2C1)

	time.Sleep(sensorFirst)
	ticker := time.NewTicker(sensorInterval)
	defer ticker.Stop()
	for {
		measure(cmds)
		<-ticker.C
	}
}

// measure reads the sensor and queues the reading. A sensor that cannot be
// configured or read is reported and retried at the next tick.
func measure(cmds chan<- bus.Command) {
	if !sensorConfigured {
		if err := sensor.Configure(); err != nil {
			fmt.Fprintln(console, "sht20:", err)
			return
		}
		sensorConfigured = true
	}

	at := time.Since(boot)
	t, err := sensor.ReadTemperature()
	if err != nil {
		fmt.Fprintln(console, "sht20: temperature:", err)
		return
	}
	h, err := sensor.ReadHumidity()
	if err != nil {
		fmt.Fprintln(console, "sht20: humidity:", err)
		return
	}
	reading.valid, reading.at, reading.temperature, reading.humidity = true, at, t, h

	select {
	case cmds <- bus.Command{Kind: bus.CmdSensor, Temperature: t, Humidity: h}:
	default:
		fmt.Fprintln(console, "tx queue full")
	}
}

// sensorCmd reports the latest reading on the console.
func sensorCmd() cli.Cmd {
	return cli.Cmd{
		Name: "sensor",
		Help: "show the latest temperature and humidity reading",
		Func: func(args []string, w io.Writer) {
			if len(args) != 0 {
				fmt.Fprintln(w, "usage: sensor")
				return
			}
			if !reading.valid {
				fmt.Fprintln(w, "no reading yet")
				return
			}
			fmt.Fprintf(w, "temperature %.2f °C, humidity %.2f %%RH at %d ms (%d s ago)\n",
				float32(reading.temperature)/1000, float32(reading.humidity)/100,
				reading.at.Milliseconds(), int64((time.Since(boot)-reading.at)/time.Second))
		},
	}
}
