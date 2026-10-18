//go:build tinygo

package main

import (
	"fmt"
	"knixx/led"
	"machine"

	"tinygo.org/x/drivers/ws2812"
)

// ledPin drives the data input of the WS2812B string: LED1, then LED2.
const ledPin = machine.A3 // P0.28

// leds is the only way to the status LEDs; see led.Run.
var leds = make(chan led.Trigger, led.QueueLen)

// startLEDs starts the goroutine that owns the status LEDs.
func startLEDs() {
	ledPin.Configure(machine.PinConfig{Mode: machine.PinOutput})
	ledPin.Low()
	go led.Run(leds, ws2812.NewWS2812(ledPin))
}

// signal triggers mode on the status LEDs, without blocking.
func signal(mode led.Mode) {
	led.Notify(leds, led.Trigger{Mode: mode})
}

// endInit ends the init blink.
func endInit() {
	led.Notify(leds, led.Trigger{Mode: led.Init, Cancel: true})
}

// fail prints an error on the console and flashes it on the status LEDs.
func fail(a ...any) {
	fmt.Fprintln(console, a...)
	signal(led.Error)
}
