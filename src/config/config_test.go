package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vapourismo/knx-go/knx/cemi"
)

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "knixx.ini")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	dev, err := Load(write(t, `
[device]
individual = 7.8.1

[A]
group = 0/0/2

[B]
group = 0/0/3
`))
	if err != nil {
		t.Fatal(err)
	}
	if want := uint16(cemi.NewIndividualAddr3(7, 8, 1)); dev.Individual != want {
		t.Errorf("Individual = %#04x, want %#04x", dev.Individual, want)
	}
	for i, want := range []struct {
		name  string
		group uint16
	}{
		{"A", uint16(cemi.NewGroupAddr3(0, 0, 2))},
		{"B", uint16(cemi.NewGroupAddr3(0, 0, 3))},
	} {
		sw := dev.Switches[i]
		if sw.Name != want.name || sw.Group != want.group {
			t.Errorf("Switches[%d] = %+v, want %+v", i, sw, want)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	tests := map[string]string{
		"missing device section": "[A]\ngroup = 0/0/2\n[B]\ngroup = 0/0/3\n",
		"missing switch section": "[device]\nindividual = 7.8.1\n[A]\ngroup = 0/0/2\n",
		"missing group key":      "[device]\nindividual = 7.8.1\n[A]\ngroup = 0/0/2\n[B]\nfoo = bar\n",
		"bad individual address": "[device]\nindividual = 7/8/1\n[A]\ngroup = 0/0/2\n[B]\ngroup = 0/0/3\n",
		"bad group address":      "[device]\nindividual = 7.8.1\n[A]\ngroup = nope\n[B]\ngroup = 0/0/3\n",
	}
	for name, content := range tests {
		if _, err := Load(write(t, content)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := Load(filepath.Join(t.TempDir(), "absent.ini")); err == nil {
		t.Error("absent file: expected an error")
	}
}
