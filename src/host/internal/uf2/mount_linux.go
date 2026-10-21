//go:build linux

package uf2

import (
	"errors"
	"fmt"

	"github.com/godbus/dbus/v5"
)

const (
	udisks        = "org.freedesktop.UDisks2"
	udisksManager = "/org/freedesktop/UDisks2/Manager"
)

// MountVolume asks udisks2, over the system D-Bus, to mount the FAT drive
// labelled one of names, as a desktop automounter or udisksctl mount does.
// It returns the mount point, or "" when the drive was mounted already. The
// mount is done for the user at the machine as polkit allows it; a refusal
// is an error rather than a password prompt. It returns ErrNoDrive when
// there is no such drive.
func MountVolume(names []string) (string, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return "", fmt.Errorf("uf2: system bus: %w", err)
	}
	defer conn.Close()

	manager := conn.Object(udisks, udisksManager)
	for _, name := range names {
		var blocks []dbus.ObjectPath
		spec := map[string]dbus.Variant{"label": dbus.MakeVariant(name)}
		err := manager.Call(udisks+".Manager.ResolveDevice", 0, spec, map[string]dbus.Variant{}).Store(&blocks)
		if err != nil {
			return "", fmt.Errorf("uf2: udisks: %w", err)
		}
		for _, path := range blocks {
			block := conn.Object(udisks, path)
			// Only a FAT file system, not another drive with the same label.
			if t, err := block.GetProperty(udisks + ".Block.IdType"); err != nil || t.Value() != "vfat" {
				continue
			}
			var mount string
			opts := map[string]dbus.Variant{"auth.no_user_interaction": dbus.MakeVariant(true)}
			err := block.Call(udisks+".Filesystem.Mount", 0, opts).Store(&mount)
			var derr dbus.Error
			if errors.As(err, &derr) && derr.Name == udisks+".Error.AlreadyMounted" {
				return "", nil
			}
			if err != nil {
				return "", fmt.Errorf("uf2: mount %s: %w", name, err)
			}
			return mount, nil
		}
	}
	return "", ErrNoDrive
}
