// Command ota flashes a DFU package over BLE to a board whose Adafruit
// bootloader runs its BLE DFU mode, which the ota console command starts.
// It uses BlueZ through tinygo.org/x/bluetooth.
package main

import (
	"errors"
	"flag"
	"fmt"
	"knixx/internal/legacydfu"
	"os"
	"strings"
	"time"

	"tinygo.org/x/bluetooth"
)

func main() {
	zipPath := flag.String("zip", "../build/knixx-dfu.zip", "DFU package built by make dfu")
	name := flag.String("name", "AdaDFU", "advertised name of the bootloader")
	addr := flag.String("addr", "", "address of the bootloader, instead of matching -name")
	wait := flag.Duration("wait", 60*time.Second, "how long to wait for the bootloader")
	flag.Parse()

	if err := run(*zipPath, *name, *addr, *wait); err != nil {
		fmt.Fprintln(os.Stderr, "ota:", err)
		os.Exit(1)
	}
}

func run(zipPath, name, addr string, wait time.Duration) error {
	pkg, err := legacydfu.Open(zipPath)
	if err != nil {
		return err
	}
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return fmt.Errorf("bluetooth: %w", err)
	}

	target := name
	if addr != "" {
		target = addr
	}
	fmt.Printf("waiting for %s (run ota on the knixx console)...\n", target)
	found, err := scan(adapter, name, addr, wait)
	if err != nil {
		return err
	}
	fmt.Printf("connecting to %s\n", found.Address)
	dev, err := adapter.Connect(found.Address, bluetooth.ConnectionParams{})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer dev.Disconnect()

	link, err := open(dev)
	if err != nil {
		return err
	}
	start := time.Now()
	shown := -1
	err = legacydfu.Update(link, pkg, func(sent, total int) {
		if pct := sent * 100 / total; pct != shown {
			shown = pct
			fmt.Printf("\rsending %d/%d bytes (%d%%)", sent, total, pct)
		}
	})
	fmt.Println()
	if err != nil {
		return err
	}
	fmt.Printf("done in %.0f s, the board restarts with the new firmware\n", time.Since(start).Seconds())
	return nil
}

// scan returns the first advertiser named name, or with address addr when
// that is set.
func scan(adapter *bluetooth.Adapter, name, addr string, wait time.Duration) (bluetooth.ScanResult, error) {
	var found bluetooth.ScanResult
	ok := false
	timer := time.AfterFunc(wait, func() { adapter.StopScan() })
	defer timer.Stop()
	err := adapter.Scan(func(a *bluetooth.Adapter, r bluetooth.ScanResult) {
		match := r.LocalName() == name
		if addr != "" {
			match = strings.EqualFold(r.Address.String(), addr)
		}
		if match && !ok {
			found, ok = r, true
			a.StopScan()
		}
	})
	if err != nil {
		return found, fmt.Errorf("scan: %w", err)
	}
	if !ok {
		return found, fmt.Errorf("no bootloader seen within %v", wait)
	}
	return found, nil
}

// link adapts the characteristics of the DFU service to legacydfu.Link.
type link struct {
	control, packet bluetooth.DeviceCharacteristic
	notify          chan []byte
	mtu             int
}

func (l *link) WriteControl(p []byte) error {
	_, err := l.control.Write(p)
	return err
}

func (l *link) WritePacket(p []byte) error {
	_, err := l.packet.WriteWithoutResponse(p)
	return err
}

func (l *link) Notifications() <-chan []byte { return l.notify }

// PacketSize is the payload of an ATT write with the negotiated MTU.
func (l *link) PacketSize() int { return max(l.mtu-3, 0) }

// open discovers the DFU service of dev and subscribes to its control point.
func open(dev bluetooth.Device) (*link, error) {
	svcUUID, _ := bluetooth.ParseUUID(legacydfu.ServiceUUID)
	cpUUID, _ := bluetooth.ParseUUID(legacydfu.ControlPointUUID)
	pktUUID, _ := bluetooth.ParseUUID(legacydfu.PacketUUID)

	svcs, err := dev.DiscoverServices([]bluetooth.UUID{svcUUID})
	if err != nil || len(svcs) == 0 {
		return nil, fmt.Errorf("no DFU service: %v", err)
	}
	chars, err := svcs[0].DiscoverCharacteristics([]bluetooth.UUID{cpUUID, pktUUID})
	if err != nil {
		return nil, fmt.Errorf("DFU characteristics: %w", err)
	}
	l := &link{notify: make(chan []byte, 16)}
	var haveCP, havePkt bool
	for _, c := range chars {
		switch c.UUID() {
		case cpUUID:
			l.control, haveCP = c, true
		case pktUUID:
			l.packet, havePkt = c, true
		}
	}
	if !haveCP || !havePkt {
		return nil, errors.New("DFU service lacks its control point or packet characteristic")
	}
	if err := l.control.EnableNotifications(func(buf []byte) {
		l.notify <- append([]byte(nil), buf...)
	}); err != nil {
		return nil, fmt.Errorf("control point notifications: %w", err)
	}
	// BlueZ exchanges the MTU on connecting; without it, writes stay at
	// the default size.
	if mtu, err := l.packet.GetMTU(); err == nil {
		l.mtu = int(mtu)
		fmt.Printf("ATT MTU %d\n", mtu)
	}
	return l, nil
}
