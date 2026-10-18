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
	// noSensor2 is noSensor as Format writes it.
	const noSensor2 = "[device]\nindividual = 7.8.1\n\n[A]\ngroup = 0/0/1\n\n[B]\ngroup = 0/0/2\n"
	dev, err := Parse([]byte(noSensor))
	if err != nil {
		t.Fatal(err)
	}
	if dev.Sensor != (thunder.Sensor{}) {
		t.Errorf("Sensor without a section = %+v, want unbound", dev.Sensor)
	}
	for _, key := range []string{"temperature.group", "humidity.group", "illuminance.group"} {
		if v, _ := Get(dev, key); v != "-" {
			t.Errorf("%s = %q, want -", key, v)
		}
	}
	if got := string(Format(dev)); got != "[device]\nindividual = 7.8.1\n\n[A]\ngroup = 0/0/1\n\n[B]\ngroup = 0/0/2\n" {
		t.Errorf("Format without sensor = %q", got)
	}

	if err := Set(dev, "illuminance.group", "0/1/3"); err != nil || dev.Sensor.Illuminance != uint16(cemi.NewGroupAddr3(0, 1, 3)) {
		t.Errorf("Set(illuminance.group): %+v, %v", dev.Sensor, err)
	}
	if got := string(Format(dev)); got != noSensor2+"\n[illuminance]\ngroup = 0/1/3\n" {
		t.Errorf("Format with illuminance only = %q", got)
	}
	if again, err := Parse(Format(dev)); err != nil || *again != *dev {
		t.Errorf("illuminance round trip: %+v, %v", again, err)
	}
	for _, value := range []string{"", "0/1/3, 0/1/4", "0/0/0", "lux"} {
		if err := Set(dev, "illuminance.group", value); err == nil {
			t.Errorf("Set(illuminance.group, %q): expected an error", value)
		}
	}
	if err := Set(dev, "illuminance.group", "-"); err != nil || dev.Sensor.Illuminance != 0 {
		t.Errorf("unbind illuminance: %+v, %v", dev.Sensor, err)
	}

	// The [sensor] section of earlier versions is ignored.
	dev, err = Parse([]byte(noSensor + "[sensor]\ntemperature = 0/1/1\nhumidity = 0/1/2\n"))
	if err != nil || dev.Sensor != (thunder.Sensor{}) {
		t.Errorf("legacy [sensor]: %+v, %v", dev.Sensor, err)
	}

	dev, err = Parse([]byte(noSensor + "[humidity]\ngroup = 1/2/3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := (thunder.Sensor{Humidity: uint16(cemi.NewGroupAddr3(1, 2, 3))}); dev.Sensor != want {
		t.Errorf("Sensor = %+v, want %+v", dev.Sensor, want)
	}
	if got := string(Format(dev)); got != noSensor2+"\n[humidity]\ngroup = 1/2/3\n" {
		t.Errorf("Format with humidity only = %q", got)
	}

	if err := Set(dev, "Temperature.Group", "0/1/1"); err != nil {
		t.Fatal(err)
	}
	if v, _ := Get(dev, "temperature.group"); v != "0/1/1" {
		t.Errorf("temperature.group = %q", v)
	}
	again, err := Parse(Format(dev))
	if err != nil || *again != *dev {
		t.Errorf("round trip: %+v, %v", again, err)
	}
	if err := Set(dev, "humidity.group", "-"); err != nil || dev.Sensor.Humidity != 0 {
		t.Errorf("unbind: %+v, %v", dev.Sensor, err)
	}
	again, err = Parse(Format(dev))
	if err != nil || *again != *dev {
		t.Errorf("round trip after unbind: %+v, %v", again, err)
	}

	before := *dev
	for _, value := range []string{"", "0/1/1, 0/1/2", "nope", "1.1.1", "0/0/0"} {
		if err := Set(dev, "temperature.group", value); err == nil {
			t.Errorf("Set(temperature.group, %q): expected an error", value)
		}
	}
	if *dev != before {
		t.Errorf("failed Set modified the device: %+v", dev)
	}
	if _, err := Parse([]byte(noSensor + "[temperature]\ngroup = nope\n")); err == nil {
		t.Error("Parse with a bad sensor group: expected an error")
	}
}

func TestOccupancy(t *testing.T) {
	const base = "[device]\nindividual = 7.8.1\n[A]\ngroup = 0/0/1\n[B]\ngroup = 0/0/2\n"
	dev, err := Parse([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	defaults := thunder.Occupancy{
		Timeout:     thunder.DefaultOccupancyTimeout,
		Threshold:   thunder.DefaultMotionThreshold,
		Frequency:   thunder.DefaultRangingFrequency,
		Integration: thunder.DefaultIntegrationTime,
		MinDistance: thunder.DefaultMinDistance,
		MaxDistance: thunder.DefaultMaxDistance,
	}
	if dev.Occupancy != defaults {
		t.Errorf("Occupancy without a section = %+v, want %+v", dev.Occupancy, defaults)
	}
	if got := string(Format(dev)); got != "[device]\nindividual = 7.8.1\n\n[A]\ngroup = 0/0/1\n\n[B]\ngroup = 0/0/2\n" {
		t.Errorf("Format with default occupancy = %q", got)
	}

	def, err := Parse(Default)
	if err != nil {
		t.Fatal(err)
	}
	want := thunder.Occupancy{Group: uint16(cemi.NewGroupAddr3(0, 2, 1)), Timeout: 60, Threshold: 44,
		Frequency: 1, Integration: 5, MinDistance: 400, MaxDistance: 1500}
	if def.Occupancy != want {
		t.Errorf("default Occupancy = %+v, want %+v", def.Occupancy, want)
	}

	for key, value := range map[string]string{
		"occupancy.group":        "1/2/3",
		"Occupancy.Timeout":      "65535",
		"occupancy.threshold":    "32767",
		"occupancy.frequency":    "60",
		"occupancy.integration":  "1000",
		"occupancy.min_distance": "4000",
		"occupancy.max_distance": "400",
	} {
		if err := Set(dev, key, value); err != nil {
			t.Fatalf("Set(%s, %s): %v", key, value, err)
		}
		if got, _ := Get(dev, key); got != value {
			t.Errorf("Get(%s) = %q, want %q", key, got, value)
		}
	}
	again, err := Parse(Format(dev))
	if err != nil || *again != *dev {
		t.Errorf("round trip: %+v, %v", again, err)
	}
	if err := Set(dev, "occupancy.group", "-"); err != nil || dev.Occupancy.Group != 0 {
		t.Errorf("unbind: %+v, %v", dev.Occupancy, err)
	}
	again, err = Parse(Format(dev))
	if err != nil || *again != *dev {
		t.Errorf("round trip after unbind: %+v, %v", again, err)
	}

	before := *dev
	for key, values := range map[string][]string{
		"occupancy.timeout":      {"0", "65536", "-1", "1.5", "", "sixty"},
		"occupancy.threshold":    {"0", "32768", "-44", "nope"},
		"occupancy.group":        {"0/0/0", "0/2/1, 0/2/2", "nope"},
		"occupancy.frequency":    {"0", "61", "-1", "ten"},
		"occupancy.integration":  {"1", "1001", "5ms"},
		"occupancy.min_distance": {"399", "4001", "far"},
		"occupancy.max_distance": {"0", "4001", "1.5m"},
	} {
		for _, value := range values {
			if err := Set(dev, key, value); err == nil {
				t.Errorf("Set(%s, %q): expected an error", key, value)
			}
		}
	}
	if *dev != before {
		t.Errorf("failed Set modified the device: %+v", dev)
	}
	for _, section := range []string{"timeout = 0\n", "threshold = 40000\n", "group = x\n",
		"frequency = 61\n", "integration = 1\n", "min_distance = 399\n", "max_distance = 4001\n"} {
		if _, err := Parse([]byte(base + "[occupancy]\n" + section)); err == nil {
			t.Errorf("Parse with %q: expected an error", section)
		}
	}
	for _, value := range []string{"1", " 60 "} {
		if err := Set(dev, "occupancy.timeout", value); err != nil {
			t.Errorf("Set(occupancy.timeout, %q): %v", value, err)
		}
	}

	// The window is not checked as a pair: an inverted one, as set above,
	// survives a round trip.
	if o := again.Occupancy; o.MinDistance != 4000 || o.MaxDistance != 400 {
		t.Errorf("inverted window after round trip: %+v", o)
	}

	// A section written before the ranging keys existed keeps their defaults.
	old, err := Parse([]byte(base + "[occupancy]\ngroup = 0/2/1\ntimeout = 60\nthreshold = 44\n"))
	if err != nil {
		t.Fatal(err)
	}
	if old.Occupancy != want {
		t.Errorf("Occupancy without ranging keys = %+v, want %+v", old.Occupancy, want)
	}
}
