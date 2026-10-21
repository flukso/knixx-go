// Package blescan finds a BLE peripheral by its advertised name or its
// address, for bolt, the host tool that connects to a board.
package blescan

import (
	"fmt"
	"strings"
	"time"

	"tinygo.org/x/bluetooth"
)

// Find scans for up to wait and returns the first advertiser named name, or
// with address addr when that is set. On Linux the address is the MAC
// address; on macOS, which hides it, the UUID that CoreBluetooth assigns to
// the device on this host.
func Find(adapter *bluetooth.Adapter, name, addr string, wait time.Duration) (bluetooth.ScanResult, error) {
	var found bluetooth.ScanResult
	ok := false
	timer := time.AfterFunc(wait, func() { adapter.StopScan() })
	defer timer.Stop()
	err := adapter.Scan(func(a *bluetooth.Adapter, r bluetooth.ScanResult) {
		if Match(r.LocalName(), r.Address.String(), name, addr) && !ok {
			found, ok = r, true
			a.StopScan()
		}
	})
	if err != nil {
		return found, fmt.Errorf("scan: %w", err)
	}
	if !ok {
		target := name
		if addr != "" {
			target = addr
		}
		return found, fmt.Errorf("%s not seen within %v", target, wait)
	}
	return found, nil
}

// Match reports whether an advertiser with localName and address is the
// one asked for: by address when addr is set, by name otherwise.
func Match(localName, address, name, addr string) bool {
	if addr != "" {
		return strings.EqualFold(address, addr)
	}
	return localName == name
}
