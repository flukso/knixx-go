//go:build tinygo

package main

import (
	"bytes"
	"fmt"
	"io"
	"knixx/bus"
	"knixx/cli"
	"knixx/internal/i2cbus"
	"knixx/led"
	"knixx/version"
	"machine"
)

const baud = 38400

func main() {
	startLEDs()
	startOutput()
	signal(led.Init)

	var log bytes.Buffer
	log.WriteString("=== knixx thunder ===\n")
	log.WriteString(version.String() + "\n")

	machine.UART0.Configure(machine.UARTConfig{BaudRate: baud})
	bus.Out = console
	bus.OnError = func() { signal(led.Error) }

	dev := loadConfig(&log)
	configureI2C(&log)

	cmds := make(chan bus.Command, bus.QueueLen)
	done := make(chan struct{})
	go bus.Run(machine.UART0, dev, cmds, done)
	go runSensor(cmds)
	go runToF(dev, cmds)
	go runSwitches(dev, cmds)

	runConsole(append(cli.Commands(dev, flashStore{}, cmds), sensorCmd(), tofCmd(dev), nusCmd(), otaCmd(), versionCmd(), sysCmd()), log.Bytes())
}

// i2c1 is I2C1 for the drivers: the sensor and ToF goroutines share it,
// one transfer at a time.
var i2c1 = i2cbus.New(machine.I2C1)

// configureI2C sets up I2C1, which the SHT20 and the VL53L7CX share. I2C0
// shares its peripheral instance with SPI0, which drives the flash. At
// 400 kHz the VL53L7CX firmware upload takes about 2 s instead of 8 s.
func configureI2C(log io.Writer) {
	if err := machine.I2C1.Configure(machine.I2CConfig{
		SDA:       machine.SDA_PIN,
		SCL:       machine.SCL_PIN,
		Frequency: 400 * machine.KHz,
	}); err != nil {
		fmt.Fprintln(log, "i2c:", err)
		signal(led.Error)
	}
}
