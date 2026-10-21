// Command bolt is the host tool for a knixx board over BLE, with the
// computer's own Bluetooth adapter through tinygo.org/x/bluetooth: BlueZ on
// Linux, CoreBluetooth on macOS. Its subcommands:
//
//	bolt ota   send a DFU package to the board's bootloader
//	bolt term  a terminal on the board's BLE console
//
// Status messages go to stderr.
package main

import (
	"flag"
	"fmt"
	"knixx/host/internal/blescan"
	"os"
	"time"

	"tinygo.org/x/bluetooth"
)

const usage = `usage: bolt <command> [flags]

commands:
  ota   send a DFU package over BLE to the bootloader (run ota on the knixx console)
  term  a terminal on the BLE console (run nus on on the knixx console)

Run bolt <command> -h for the flags of a command.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "ota":
		err = otaCmd(args)
	case "term":
		err = termCmd(args)
	case "help", "-h", "-help", "--help":
		fmt.Fprint(os.Stderr, usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "bolt: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "bolt %s: %v\n", cmd, err)
		os.Exit(1)
	}
}

// otaCmd parses the flags of bolt ota and runs it.
func otaCmd(args []string) error {
	fs := flag.NewFlagSet("bolt ota", flag.ExitOnError)
	zipPath := fs.String("zip", "build/knixx-dfu.zip", "DFU package built by make dfu")
	name := fs.String("name", "AdaDFU", "advertised name of the bootloader")
	addr := fs.String("addr", "", "address of the bootloader, instead of matching -name: the MAC address on Linux, the device UUID on macOS")
	wait := fs.Duration("wait", 60*time.Second, "how long to wait for the bootloader")
	fs.Parse(args)
	return runOTA(*zipPath, *name, *addr, *wait)
}

// termCmd parses the flags of bolt term and runs it.
func termCmd(args []string) error {
	fs := flag.NewFlagSet("bolt term", flag.ExitOnError)
	name := fs.String("name", "knixx", "advertised name of the board")
	addr := fs.String("addr", "", "address of the board, instead of matching -name: the MAC address on Linux, the device UUID on macOS")
	wait := fs.Duration("wait", 30*time.Second, "how long to wait for the board")
	fs.Parse(args)
	return runTerm(*name, *addr, *wait)
}

// find enables the Bluetooth adapter and scans for up to wait for the
// advertiser named name, or with address addr when that is set. hint is the
// console command that makes the board advertise.
func find(name, addr string, wait time.Duration, hint string) (*bluetooth.Adapter, bluetooth.ScanResult, error) {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return nil, bluetooth.ScanResult{}, fmt.Errorf("bluetooth: %w", err)
	}
	target := name
	if addr != "" {
		target = addr
	}
	fmt.Fprintf(os.Stderr, "waiting for %s (run %s on the knixx console)...\n", target, hint)
	found, err := blescan.Find(adapter, name, addr, wait)
	return adapter, found, err
}
