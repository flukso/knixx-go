package config

import (
	"knixx/thunder"
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
		if sw.Name != want.name || sw.NGroups != 1 || sw.Groups[0] != want.group {
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

func TestDefault(t *testing.T) {
	dev, err := Parse(Default)
	if err != nil {
		t.Fatal(err)
	}
	if dev.Switch("A") == nil || dev.Switch("B") == nil {
		t.Errorf("default config lacks switch A or B: %+v", dev)
	}
}

func TestFormatMatchesDefault(t *testing.T) {
	dev, err := Parse(Default)
	if err != nil {
		t.Fatal(err)
	}
	if got := Format(dev); string(got) != string(Default) {
		t.Errorf("Format = %q, want %q", got, Default)
	}
}

func TestGetSet(t *testing.T) {
	dev, err := Parse(Default)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range Keys {
		if _, err := Get(dev, key); err != nil {
			t.Errorf("Get(%s): %v", key, err)
		}
	}

	if err := Set(dev, "b.GROUP", "1/2/3"); err != nil {
		t.Fatal(err)
	}
	if err := Set(dev, "Device.Individual", "1.1.12"); err != nil {
		t.Fatal(err)
	}
	if v, _ := Get(dev, "B.group"); v != "1/2/3" {
		t.Errorf("B.group = %s", v)
	}
	if v, _ := Get(dev, "device.individual"); v != "1.1.12" {
		t.Errorf("device.individual = %s", v)
	}

	before := *dev
	for _, tt := range [][2]string{
		{"C.group", "0/0/1"},
		{"A.individual", "1.1.1"},
		{"device", "1.1.1"},
		{"A.group", "1.1.1"},
		{"device.individual", "0/0/1"},
		{"A.group", "nope"},
	} {
		if err := Set(dev, tt[0], tt[1]); err == nil {
			t.Errorf("Set(%s, %s): expected an error", tt[0], tt[1])
		}
	}
	if *dev != before {
		t.Errorf("failed Set modified the device: %+v", dev)
	}

	again, err := Parse(Format(dev))
	if err != nil {
		t.Fatal(err)
	}
	if *again != *dev {
		t.Errorf("round trip: got %+v, want %+v", again, dev)
	}
}

func TestFileStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knixx.ini")
	if err := (FileStore{Path: path}).Save(Default); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
}

func TestGroupLists(t *testing.T) {
	dev, err := Parse([]byte("[device]\nindividual = 7.8.1\n[A]\ngroup = 0/0/1,0/0/5 , 1/2/3\n[B]\ngroup = 0/0/2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := Get(dev, "A.group"); v != "0/0/1, 0/0/5, 1/2/3" {
		t.Errorf("A.group = %q", v)
	}
	if v, _ := Get(dev, "B.group"); v != "0/0/2" {
		t.Errorf("B.group = %q", v)
	}
	again, err := Parse(Format(dev))
	if err != nil || *again != *dev {
		t.Errorf("round trip: %+v, %v", again, err)
	}

	if err := Set(dev, "B.group", "0/0/3,0/0/4"); err != nil {
		t.Fatal(err)
	}
	if v, _ := Get(dev, "B.group"); v != "0/0/3, 0/0/4" {
		t.Errorf("B.group = %q", v)
	}

	before := *dev
	for _, value := range []string{
		"",
		"0/0/1,",
		",0/0/1",
		"0/0/1,,0/0/2",
		"0/0/1, 0/0/1",
		"0/0/1, nope",
		"0/0/1,0/0/2,0/0/3,0/0/4,0/0/5,0/0/6,0/0/7,0/0/8,0/0/9",
	} {
		if err := Set(dev, "A.group", value); err == nil {
			t.Errorf("Set(A.group, %q): expected an error", value)
		}
	}
	if *dev != before {
		t.Errorf("failed Set modified the device: %+v", dev)
	}
	if err := Set(dev, "A.group", "0/0/1,0/0/2,0/0/3,0/0/4,0/0/5,0/0/6,0/0/7,0/0/8"); err != nil {
		t.Errorf("Set with %d groups: %v", thunder.MaxGroups, err)
	}
	if _, err := Parse([]byte("[device]\nindividual = 7.8.1\n[A]\ngroup = 0/0/1, 0/0/1\n[B]\ngroup = 0/0/2\n")); err == nil {
		t.Error("Parse with a duplicate group: expected an error")
	}
}

func TestSensor(t *testing.T) {
	const noSensor = "[device]\nindividual = 7.8.1\n[A]\ngroup = 0/0/1\n[B]\ngroup = 0/0/2\n"
	dev, err := Parse([]byte(noSensor))
	if err != nil {
		t.Fatal(err)
	}
	if dev.Sensor != (thunder.Sensor{}) {
		t.Errorf("Sensor without a section = %+v, want unbound", dev.Sensor)
	}
	for _, key := range []string{"sensor.temperature", "sensor.humidity"} {
		if v, _ := Get(dev, key); v != "-" {
			t.Errorf("%s = %q, want -", key, v)
		}
	}
	if got := string(Format(dev)); got != "[device]\nindividual = 7.8.1\n\n[A]\ngroup = 0/0/1\n\n[B]\ngroup = 0/0/2\n" {
		t.Errorf("Format without sensor = %q", got)
	}

	dev, err = Parse([]byte(noSensor + "[sensor]\nhumidity = 1/2/3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := (thunder.Sensor{Humidity: uint16(cemi.NewGroupAddr3(1, 2, 3))}); dev.Sensor != want {
		t.Errorf("Sensor = %+v, want %+v", dev.Sensor, want)
	}

	if err := Set(dev, "Sensor.Temperature", "0/1/1"); err != nil {
		t.Fatal(err)
	}
	if v, _ := Get(dev, "sensor.temperature"); v != "0/1/1" {
		t.Errorf("sensor.temperature = %q", v)
	}
	again, err := Parse(Format(dev))
	if err != nil || *again != *dev {
		t.Errorf("round trip: %+v, %v", again, err)
	}
	if err := Set(dev, "sensor.humidity", "-"); err != nil || dev.Sensor.Humidity != 0 {
		t.Errorf("unbind: %+v, %v", dev.Sensor, err)
	}
	again, err = Parse(Format(dev))
	if err != nil || *again != *dev {
		t.Errorf("round trip after unbind: %+v, %v", again, err)
	}

	before := *dev
	for _, value := range []string{"", "0/1/1, 0/1/2", "nope", "1.1.1", "0/0/0"} {
		if err := Set(dev, "sensor.temperature", value); err == nil {
			t.Errorf("Set(sensor.temperature, %q): expected an error", value)
		}
	}
	if *dev != before {
		t.Errorf("failed Set modified the device: %+v", dev)
	}
	if _, err := Parse([]byte(noSensor + "[sensor]\ntemperature = nope\n")); err == nil {
		t.Error("Parse with a bad sensor group: expected an error")
	}
}
