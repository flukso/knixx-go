// Command bolt is the host tool for a knixx board, over BLE with the
// computer's own Bluetooth adapter through tinygo.org/x/bluetooth (BlueZ on
// Linux, CoreBluetooth on macOS) and over USB. Its subcommands:
//
//	bolt genpkg  build a DFU package from the application's hex file
//	bolt ota     send a DFU package to the board's bootloader over BLE
//	bolt term    a terminal on the board's BLE console
//	bolt uf2     flash a UF2 file over USB, through the UF2 bootloader
//
// Status messages go to stderr.
package main

import (
	"flag"
	"fmt"
	"knixx/host/internal/blescan"
	"knixx/host/internal/legacydfu"
	"os"
	"time"

	"tinygo.org/x/bluetooth"
)

const usage = `usage: bolt <command> [flags]

commands:
  genpkg  build a DFU package from the application's hex file
  ota     send a DFU package over BLE to the bootloader (run ota on the knixx console)
  term    a terminal on the BLE console (run nus on on the knixx console)
  uf2     flash a UF2 file over USB through the UF2 bootloader

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
	case "genpkg":
		err = genpkgCmd(args)
	case "ota":
		err = otaCmd(args)
	case "term":
		err = termCmd(args)
	case "uf2":
		err = uf2Cmd(args)
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

// genpkgCmd parses the flags of bolt genpkg and runs it. The defaults are
// those of the Adafruit bootloader of the Feather nRF52840: device type
// 0x0052 and the firmware id 0x00B6 of its S140 6.1.1.
func genpkgCmd(args []string) error {
	fs := flag.NewFlagSet("bolt genpkg", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: bolt genpkg [flags] <app.hex> <package.zip>")
		fs.PrintDefaults()
	}
	devType := fs.Uint("dev-type", 0x0052, "device type of the init packet")
	devRev := fs.Uint("dev-rev", 0xFFFF, "device revision of the init packet")
	appVer := fs.Uint("app-ver", 0xFFFFFFFF, "application version of the init packet")
	sdReq := fs.String("sd-req", "0x00B6", "comma separated SoftDevice firmware ids the application runs on")
	fs.Parse(args)
	if fs.NArg() != 2 {
		fs.Usage()
		os.Exit(2)
	}
	if *devType > 0xFFFF || *devRev > 0xFFFF || *appVer > 0xFFFFFFFF {
		return fmt.Errorf("-dev-type and -dev-rev take 16 bits, -app-ver 32")
	}
	sds, err := parseIDs(*sdReq)
	if err != nil {
		return fmt.Errorf("-sd-req: %w", err)
	}
	ip := legacydfu.InitPacket{DevType: uint16(*devType), DevRev: uint16(*devRev), AppVer: uint32(*appVer), SDReq: sds}
	return runGenpkg(fs.Arg(0), fs.Arg(1), ip)
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

// uf2Cmd parses the flags of bolt uf2 and runs it.
func uf2Cmd(args []string) error {
	fs := flag.NewFlagSet("bolt uf2", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: bolt uf2 [flags] [file.uf2]  (default build/knixx.uf2)")
		fs.PrintDefaults()
	}
	port := fs.String("port", "", "serial port of the running board, instead of finding it")
	volume := fs.String("volume", "FTHR840BOOT", "name of the UF2 bootloader's USB drive")
	expect := fs.String("expect", "", "the line the board's version command must print after the flash")
	wait := fs.Duration("wait", 20*time.Second, "how long to wait for the drive and for the board to come back")
	fs.Parse(args)
	file := "build/knixx.uf2"
	switch fs.NArg() {
	case 0:
	case 1:
		file = fs.Arg(0)
	default:
		fs.Usage()
		os.Exit(2)
	}
	return runUF2(file, *port, *volume, *expect, *wait)
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
