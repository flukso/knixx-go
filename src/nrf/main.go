//go:build tinygo

package main

import (
	"bytes"
	"knixx/bus"
	"knixx/cli"
	"machine"
)

const baud = 38400

func main() {
	var log bytes.Buffer
	log.WriteString("=== knixx thunder ===\n")

	machine.UART0.Configure(machine.UARTConfig{BaudRate: baud})
	bus.Out = console

	dev := loadConfig(&log)

	cmds := make(chan bus.Command, bus.QueueLen)
	done := make(chan struct{})
	go bus.Run(machine.UART0, dev, cmds, done)
	go runSensor(cmds)

	runConsole(append(cli.Commands(dev, flashStore{}, cmds), sensorCmd()), log.Bytes())
}
