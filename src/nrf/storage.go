//go:build tinygo

package main

import (
	"errors"
	"fmt"
	"io"
	"knixx/config"
	"knixx/thunder"
	"machine"
	"os"
	"time"

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
	filesystem = littlefs.New(sectorFlash{blockDevice})

	configBuf [1024]byte

	// mounted records that the littlefs is usable for saving.
	mounted bool
)

// sectorFlash fixes the erase of flash.Device (tinygo.org/x/drivers v0.36.0)
// for littlefs. littlefs uses EraseBlockSize, the 4 KiB sector, as its block
// size, but EraseBlocks erases a 64 KiB block at block × 64 KiB: it wiped
// 16 times the requested area at the wrong address, corrupting the
// filesystem. The driver also busy-waits for the erase to complete, without
// yielding, freezing every goroutine for up to seconds.
//
// EraseBlocks here erases sectors and waits for each erase while yielding,
// so the driver's own busy-wait before the next operation finds the flash
// ready.
type sectorFlash struct {
	*flash.Device
}

const (
	// flashBusy masks the WIP (write in progress) and WEL (write enable
	// latch) bits of status register 1, as the driver does.
	flashBusy = 0x03
	// eraseTimeout bounds a sector erase, which takes tens of ms typically
	// and some hundreds at most.
	eraseTimeout = 2 * time.Second
	erasePoll    = time.Millisecond
)

func (f sectorFlash) EraseBlocks(start, n int64) error {
	for i := start; i < start+n; i++ {
		if err := f.EraseSector(uint32(i)); err != nil {
			return err
		}
		if err := f.waitReady(); err != nil {
			return err
		}
	}
	return nil
}

// waitReady waits for the flash to finish an erase, letting the other
// goroutines run meanwhile.
func (f sectorFlash) waitReady() error {
	start := time.Now()
	for {
		s, err := f.ReadStatus()
		if err != nil {
			return err
		}
		if s&flashBusy == 0 {
			return nil
		}
		if time.Since(start) > eraseTimeout {
			return flash.ErrWaitExpired
		}
		time.Sleep(erasePoll)
	}
}

// loadConfig mounts the littlefs on the external flash, formatting it and
// writing the default configuration when there is none, and parses
// knixx.ini. Progress is reported on log. When the flash or the file is
// unusable it falls back to the built-in default.
func loadConfig(log io.Writer) *thunder.Device {
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
	mounted = true

	if _, err := filesystem.Stat(configPath); err != nil {
		if err := writeConfig(config.Default); err != nil {
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

func writeConfig(data []byte) error {
	f, err := filesystem.OpenFile(configPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("%s: %w", configPath, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("%s: %w", configPath, err)
	}
	return f.Close()
}

// flashStore saves the configuration to knixx.ini on the littlefs.
type flashStore struct{}

func (flashStore) Save(data []byte) error {
	if !mounted {
		return errors.New("flash unavailable")
	}
	return writeConfig(data)
}
