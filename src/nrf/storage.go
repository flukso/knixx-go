//go:build tinygo

package main

import (
	"errors"
	"fmt"
	"io"
	"knixx/config"
	"knixx/light"
	"machine"
	"os"

	"tinygo.org/x/drivers/flash"
	"tinygo.org/x/tinyfs/littlefs"
)

const configPath = "/knixx.ini"

var (
	blockDevice = flash.NewSPI(
		machine.SPI0,
		machine.P0_17, // SDO = QSPI DATA0
		machine.P0_22, // SDI = QSPI DATA1
		machine.P0_19, // SCK
		machine.P0_20, // CS
	)
	filesystem = littlefs.New(blockDevice)

	configBuf [1024]byte
)

// loadConfig mounts the littlefs on the external flash, formatting it and
// writing the default configuration when there is none, and parses
// knixx.ini. Progress is reported on log. When the flash or the file is
// unusable it falls back to the built-in default.
func loadConfig(log io.Writer) *light.Device {
	data, err := readConfig(log)
	if err == nil {
		dev, err := config.Parse(data)
		if err == nil {
			return dev
		}
		fmt.Fprintln(log, configPath+":", err)
	} else {
		fmt.Fprintln(log, err)
	}
	fmt.Fprintln(log, "using the built-in default configuration")
	dev, err := config.Parse(config.Default)
	if err != nil {
		// The default is embedded and covered by tests; this cannot happen.
		panic(err)
	}
	return dev
}

func readConfig(log io.Writer) ([]byte, error) {
	// WP# and HOLD# must stay high while the flash is driven in single-line
	// SPI mode.
	for _, p := range [...]machine.Pin{machine.P0_23, machine.P0_21} {
		p.Configure(machine.PinConfig{Mode: machine.PinOutput})
		p.High()
	}

	if err := blockDevice.Configure(&flash.DeviceConfig{Identifier: flash.DefaultDeviceIdentifier}); err != nil {
		return nil, fmt.Errorf("flash: %w", err)
	}
	filesystem.Configure(&littlefs.Config{
		CacheSize:     512,
		LookaheadSize: 512,
		BlockCycles:   100,
	})

	if err := filesystem.Mount(); err != nil {
		fmt.Fprintln(log, "no littlefs found, formatting flash")
		if err := filesystem.Format(); err != nil {
			return nil, fmt.Errorf("littlefs format: %w", err)
		}
		if err := filesystem.Mount(); err != nil {
			return nil, fmt.Errorf("littlefs mount: %w", err)
		}
	} else {
		fmt.Fprintln(log, "mounted littlefs")
	}

	if _, err := filesystem.Stat(configPath); err != nil {
		if err := writeDefault(); err != nil {
			return nil, err
		}
		fmt.Fprintln(log, "wrote default", configPath)
	}

	// littlefs only maps os.O_RDWR to a readable mode; os.O_RDONLY is 0.
	f, err := filesystem.OpenFile(configPath, os.O_RDWR)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", configPath, err)
	}
	defer f.Close()

	n := 0
	for n < len(configBuf) {
		m, err := f.Read(configBuf[n:])
		n += m
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", configPath, err)
		}
	}
	if n == len(configBuf) {
		return nil, fmt.Errorf("%s: larger than %d bytes", configPath, len(configBuf))
	}
	return configBuf[:n], nil
}

func writeDefault() error {
	f, err := filesystem.OpenFile(configPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("%s: %w", configPath, err)
	}
	if _, err := f.Write(config.Default); err != nil {
		f.Close()
		return fmt.Errorf("%s: %w", configPath, err)
	}
	return f.Close()
}
