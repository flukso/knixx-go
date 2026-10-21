package blescan

import "testing"

func TestMatch(t *testing.T) {
	for _, c := range []struct {
		localName, address, name, addr string
		want                           bool
	}{
		{"knixx", "F1:2C:00:11:22:33", "knixx", "", true},
		{"AdaDFU", "F1:2C:00:11:22:33", "knixx", "", false},
		{"", "F1:2C:00:11:22:33", "knixx", "", false},
		// The address wins over the name, case-insensitively.
		{"AdaDFU", "F1:2C:00:11:22:33", "knixx", "f1:2c:00:11:22:33", true},
		{"knixx", "F1:2C:00:11:22:34", "knixx", "F1:2C:00:11:22:33", false},
		// macOS identifies devices by UUID.
		{"knixx", "6e1f4b0a-2c3d-4e5f-8a9b-0c1d2e3f4a5b", "knixx", "6E1F4B0A-2C3D-4E5F-8A9B-0C1D2E3F4A5B", true},
	} {
		if got := Match(c.localName, c.address, c.name, c.addr); got != c.want {
			t.Errorf("Match(%q, %q, %q, %q) = %v", c.localName, c.address, c.name, c.addr, got)
		}
	}
}
