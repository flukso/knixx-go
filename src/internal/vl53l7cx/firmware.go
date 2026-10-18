package vl53l7cx

import _ "embed"

// The arrays of the ULD's vl53l7cx_buffers.h, extracted by internal/genfw.
// They are strings so that they stay in flash.

//go:generate go run ./internal/genfw VL53L7CX_ULD_driver_2.0.1/VL53L7CX_ULD_API/inc/vl53l7cx_buffers.h firmware

var (
	//go:embed firmware/firmware.bin
	firmware string
	//go:embed firmware/default_configuration.bin
	defaultConfiguration string
	//go:embed firmware/default_xtalk.bin
	defaultXtalk string
	//go:embed firmware/get_nvm_cmd.bin
	getNVMCmd string
)
