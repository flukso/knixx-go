//go:build tinygo

package main

import (
	"device/arm"
	"fmt"
	"io"
	"knixx/cli"
	"machine"
	"time"
)

const (
	// dfuMagicOTA in GPREGRET makes the Adafruit bootloader start its BLE
	// DFU mode at reset.
	dfuMagicOTA = 0xA8

	// The SVC numbers of sd_power_gpregret_set and sd_power_gpregret_clr in
	// the S140 6.1.1 nrf_soc.h: SOC_SVC_BASE_NOT_AVAILABLE + 16 and + 17.
	sdPowerGPREGRETSet = 0x2C + 16
	sdPowerGPREGRETClr = 0x2C + 17

	// otaFlush lets the last console line reach the terminal, over USB or
	// as notifications, before the reset drops the link.
	otaFlush = 200 * time.Millisecond
)

// otaCmd reboots into the bootloader's BLE DFU mode.
func otaCmd() cli.Cmd {
	return cli.Cmd{
		Name: "ota",
		Help: "reboot into the bootloader for a BLE firmware update (DFU)",
		Func: func(args []string, w io.Writer) {
			if len(args) != 0 {
				fmt.Fprintln(w, "usage: ota")
				return
			}
			fmt.Fprintln(w, "ota: rebooting into the BLE DFU bootloader (AdaDFU); run make ota on the host")
			time.Sleep(otaFlush)
			if err := enterOTA(); err != nil {
				fail("ota:", err)
			}
		},
	}
}

// enterOTA resets into the bootloader's BLE DFU mode. It returns only when
// the SoftDevice refused to set GPREGRET.
func enterOTA() error {
	if !bleEnabled {
		machine.EnterOTABootloader()
	}
	// POWER belongs to the SoftDevice once it is enabled. Its SVCs must be
	// called with interrupts enabled; set only ORs bits in.
	if code := arm.SVCall2(sdPowerGPREGRETClr, uint32(0), uint32(0xff)); code != 0 {
		return fmt.Errorf("gpregret clear: error %#x", code)
	}
	if code := arm.SVCall2(sdPowerGPREGRETSet, uint32(0), uint32(dfuMagicOTA)); code != 0 {
		return fmt.Errorf("gpregret set: error %#x", code)
	}
	arm.SystemReset()
	return nil
}
