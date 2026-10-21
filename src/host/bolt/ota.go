package main

import (
	"errors"
	"fmt"
	"knixx/host/internal/legacydfu"
	"os"
	"time"

	"tinygo.org/x/bluetooth"
)

// runOTA flashes the DFU package at zipPath over BLE to a board whose
// Adafruit bootloader runs its BLE DFU mode, which the ota console command
// starts.
func runOTA(zipPath, name, addr string, wait time.Duration) error {
	pkg, err := legacydfu.Open(zipPath)
	if err != nil {
		return err
	}
	adapter, found, err := find(name, addr, wait, "ota")
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "connecting to %s\n", found.Address)
	dev, err := adapter.Connect(found.Address, bluetooth.ConnectionParams{})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer dev.Disconnect()

	link, err := openDFU(dev)
	if err != nil {
		return err
	}
	start := time.Now()
	shown := -1
	err = legacydfu.Update(link, pkg, func(sent, total int) {
		if pct := sent * 100 / total; pct != shown {
			shown = pct
			fmt.Fprintf(os.Stderr, "\rsending %d/%d bytes (%d%%)", sent, total, pct)
		}
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "done in %.0f s, the board restarts with the new firmware\n", time.Since(start).Seconds())
	return nil
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

// openDFU discovers the DFU service of dev and subscribes to its control point.
func openDFU(dev bluetooth.Device) (*link, error) {
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
		fmt.Fprintf(os.Stderr, "ATT MTU %d\n", mtu)
	}
	return l, nil
}
