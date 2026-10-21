package usbserial

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// usbTTY adds the tty name under root for a USB device with vid and pid, as
// Linux lays it out in sysfs.
func usbTTY(t *testing.T, root, name, dev, vid, pid string) {
	t.Helper()
	usb := filepath.Join(root, "sys/devices/pci0000:00/usb1", dev)
	intf := filepath.Join(usb, dev+":1.0")
	if err := os.MkdirAll(filepath.Join(intf, "tty", name), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(usb, "idVendor"), []byte(vid+"\n"), 0o644)
	os.WriteFile(filepath.Join(usb, "idProduct"), []byte(pid+"\n"), 0o644)
	class := filepath.Join(root, "sys/class/tty", name)
	if err := os.MkdirAll(class, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(intf, filepath.Join(class, "device")); err != nil {
		t.Fatal(err)
	}
}

func TestSysfsPorts(t *testing.T) {
	root := t.TempDir()
	usbTTY(t, root, "ttyACM0", "1-1", "239a", "802a") // the app
	usbTTY(t, root, "ttyACM1", "1-2", "2341", "0043") // another board
	usbTTY(t, root, "ttyACM2", "1-3", "239A", "8029") // a bootloader, upper case
	got, err := sysfsPorts(root, []string{"239a:802a", "239a:8029"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/dev/ttyACM0", "/dev/ttyACM2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sysfsPorts = %q, want %q", got, want)
	}
	if got, _ := sysfsPorts(root, []string{"1234:5678"}); len(got) != 0 {
		t.Errorf("no match: %q", got)
	}
}
