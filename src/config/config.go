package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"knixx/thunder"
	"strings"

	"github.com/vapourismo/knx-go/knx/cemi"
	"github.com/zieckey/goini"
)

const (
	deviceSection  = "device"
	individualKey  = "individual"
	groupKey       = "group"
	sensorSection  = "sensor"
	temperatureKey = "temperature"
	humidityKey    = "humidity"
	// unbound is the value of an optional group address that is not set.
	unbound = "-"
)

var switchNames = [thunder.NumSwitches]string{"A", "B"}

// Default is the configuration written to a freshly formatted device.
//
//go:embed default.ini
var Default []byte

// Parse reads the device's individual address, the group addresses of each
// switch and the optional sensor group addresses from ini file contents.
func Parse(data []byte) (*thunder.Device, error) {
	ini := goini.New()
	ini.SetParseSection(true)
	ini.SetSkipCommits(true)
	if err := ini.Parse(data, goini.DefaultLineSeparator, goini.DefaultKeyValueSeparator); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	var dev thunder.Device

	v, err := get(ini, deviceSection, individualKey)
	if err != nil {
		return nil, err
	}
	individual, err := cemi.NewIndividualAddrString(v)
	if err != nil {
		return nil, fmt.Errorf("config: [%s] %s: %w", deviceSection, individualKey, err)
	}
	dev.Individual = uint16(individual)

	for i, name := range switchNames {
		v, err := get(ini, name, groupKey)
		if err != nil {
			return nil, err
		}
		dev.Switches[i] = thunder.Switch{Name: name}
		if err := parseGroups(v, &dev.Switches[i]); err != nil {
			return nil, fmt.Errorf("config: [%s] %s: %w", name, groupKey, err)
		}
	}

	// The sensor section is optional so that files written before it existed
	// still load; a missing key leaves the value unbound.
	for _, k := range [...]struct {
		key   string
		group *uint16
	}{
		{temperatureKey, &dev.Sensor.Temperature},
		{humidityKey, &dev.Sensor.Humidity},
	} {
		v, ok := ini.SectionGet(sensorSection, k.key)
		if !ok {
			continue
		}
		if err := parseOptionalGroup(v, k.group); err != nil {
			return nil, fmt.Errorf("config: [%s] %s: %w", sensorSection, k.key, err)
		}
	}

	return &dev, nil
}

// parseGroups replaces the group addresses of sw with the comma separated
// list in value. sw is left untouched when value is invalid.
func parseGroups(value string, sw *thunder.Switch) error {
	var groups [thunder.MaxGroups]uint16
	n := 0
	rest := value
	for {
		entry, tail, more := strings.Cut(rest, ",")
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return errors.New("empty group address")
		}
		if n == thunder.MaxGroups {
			return fmt.Errorf("more than %d group addresses", thunder.MaxGroups)
		}
		a, err := cemi.NewGroupAddrString(entry)
		if err != nil {
			return err
		}
		for _, g := range groups[:n] {
			if g == uint16(a) {
				return fmt.Errorf("duplicate group address %s", entry)
			}
		}
		groups[n] = uint16(a)
		n++
		if !more {
			break
		}
		rest = tail
	}
	sw.Groups, sw.NGroups = groups, uint8(n)
	return nil
}

// parseOptionalGroup sets *group to the single group address in value, or
// to 0 when value is the unbound marker. *group is left untouched when value
// is invalid.
func parseOptionalGroup(value string, group *uint16) error {
	value = strings.TrimSpace(value)
	if value == unbound {
		*group = 0
		return nil
	}
	a, err := cemi.NewGroupAddrString(value)
	if err != nil {
		return err
	}
	if a == 0 {
		return errors.New("0/0/0 is the broadcast address")
	}
	*group = uint16(a)
	return nil
}

func formatOptionalGroup(group uint16) string {
	if group == 0 {
		return unbound
	}
	return cemi.GroupAddr(group).String()
}

func formatGroups(sw *thunder.Switch) string {
	var b strings.Builder
	for i, g := range sw.GroupList() {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(cemi.GroupAddr(g).String())
	}
	return b.String()
}

func get(ini *goini.INI, section, key string) (string, error) {
	v, ok := ini.SectionGet(section, key)
	if !ok {
		return "", fmt.Errorf("config: [%s] %s: missing", section, key)
	}
	return v, nil
}

// Keys lists the parameters accessible through Get and Set, in file order.
var Keys = [...]string{
	"device.individual", "A.group", "B.group",
	"sensor.temperature", "sensor.humidity",
}

// Store persists a configuration produced by Format.
type Store interface {
	Save(data []byte) error
}

// ref points at the value a key refers to; exactly one field is non-nil.
type ref struct {
	individual *uint16
	sw         *thunder.Switch
	// group is an optional single group address.
	group *uint16
}

// field resolves a "section.key" name to the value it refers to.
func field(dev *thunder.Device, key string) (ref, error) {
	section, name, _ := strings.Cut(key, ".")
	switch {
	case strings.EqualFold(section, deviceSection) && strings.EqualFold(name, individualKey):
		return ref{individual: &dev.Individual}, nil
	case strings.EqualFold(section, sensorSection) && strings.EqualFold(name, temperatureKey):
		return ref{group: &dev.Sensor.Temperature}, nil
	case strings.EqualFold(section, sensorSection) && strings.EqualFold(name, humidityKey):
		return ref{group: &dev.Sensor.Humidity}, nil
	}
	if sw := dev.Switch(section); sw != nil && strings.EqualFold(name, groupKey) {
		return ref{sw: sw}, nil
	}
	return ref{}, fmt.Errorf("config: unknown key %q", key)
}

// Get returns the value of key in ini notation.
func Get(dev *thunder.Device, key string) (string, error) {
	r, err := field(dev, key)
	if err != nil {
		return "", err
	}
	switch {
	case r.individual != nil:
		return cemi.IndividualAddr(*r.individual).String(), nil
	case r.group != nil:
		return formatOptionalGroup(*r.group), nil
	}
	return formatGroups(r.sw), nil
}

// Set parses value and assigns it to key. A switch's group key takes a
// comma separated list; a sensor key takes a single group address, or "-"
// to unbind it. dev is left untouched when value is invalid.
func Set(dev *thunder.Device, key, value string) error {
	r, err := field(dev, key)
	if err != nil {
		return err
	}
	switch {
	case r.individual != nil:
		var a cemi.IndividualAddr
		if a, err = cemi.NewIndividualAddrString(value); err == nil {
			*r.individual = uint16(a)
		}
	case r.group != nil:
		err = parseOptionalGroup(value, r.group)
	default:
		err = parseGroups(value, r.sw)
	}
	if err != nil {
		return fmt.Errorf("config: %s: %w", key, err)
	}
	return nil
}

// Format renders dev as ini file contents that Parse reads back.
func Format(dev *thunder.Device) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "[%s]\n%s = %s\n", deviceSection, individualKey, cemi.IndividualAddr(dev.Individual))
	for _, sw := range dev.Switches {
		fmt.Fprintf(&b, "\n[%s]\n%s = %s\n", sw.Name, groupKey, formatGroups(&sw))
	}
	if s := dev.Sensor; s.Temperature != 0 || s.Humidity != 0 {
		fmt.Fprintf(&b, "\n[%s]\n", sensorSection)
		if s.Temperature != 0 {
			fmt.Fprintf(&b, "%s = %s\n", temperatureKey, cemi.GroupAddr(s.Temperature))
		}
		if s.Humidity != 0 {
			fmt.Fprintf(&b, "%s = %s\n", humidityKey, cemi.GroupAddr(s.Humidity))
		}
	}
	return b.Bytes()
}
