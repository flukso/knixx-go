package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"knixx/light"
	"strings"

	"github.com/vapourismo/knx-go/knx/cemi"
	"github.com/zieckey/goini"
)

const (
	deviceSection = "device"
	individualKey = "individual"
	groupKey      = "group"
)

var switchNames = [light.NumSwitches]string{"A", "B"}

// Default is the configuration written to a freshly formatted device.
//
//go:embed default.ini
var Default []byte

// Parse reads the device's individual address and the group address of each
// switch from ini file contents.
func Parse(data []byte) (*light.Device, error) {
	ini := goini.New()
	ini.SetParseSection(true)
	ini.SetSkipCommits(true)
	if err := ini.Parse(data, goini.DefaultLineSeparator, goini.DefaultKeyValueSeparator); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	var dev light.Device

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
		dev.Switches[i] = light.Switch{Name: name}
		if err := parseGroups(v, &dev.Switches[i]); err != nil {
			return nil, fmt.Errorf("config: [%s] %s: %w", name, groupKey, err)
		}
	}

	return &dev, nil
}

// parseGroups replaces the group addresses of sw with the comma separated
// list in value. sw is left untouched when value is invalid.
func parseGroups(value string, sw *light.Switch) error {
	var groups [light.MaxGroups]uint16
	n := 0
	rest := value
	for {
		entry, tail, more := strings.Cut(rest, ",")
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return errors.New("empty group address")
		}
		if n == light.MaxGroups {
			return fmt.Errorf("more than %d group addresses", light.MaxGroups)
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

func formatGroups(sw *light.Switch) string {
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
var Keys = [...]string{"device.individual", "A.group", "B.group"}

// Store persists a configuration produced by Format.
type Store interface {
	Save(data []byte) error
}

// field resolves a "section.key" name to the individual address or the
// switch it refers to; exactly one of the results is non-nil.
func field(dev *light.Device, key string) (individual *uint16, sw *light.Switch, err error) {
	section, name, _ := strings.Cut(key, ".")
	if strings.EqualFold(section, deviceSection) && strings.EqualFold(name, individualKey) {
		return &dev.Individual, nil, nil
	}
	if sw := dev.Switch(section); sw != nil && strings.EqualFold(name, groupKey) {
		return nil, sw, nil
	}
	return nil, nil, fmt.Errorf("config: unknown key %q", key)
}

// Get returns the value of key in ini notation.
func Get(dev *light.Device, key string) (string, error) {
	individual, sw, err := field(dev, key)
	if err != nil {
		return "", err
	}
	if individual != nil {
		return cemi.IndividualAddr(*individual).String(), nil
	}
	return formatGroups(sw), nil
}

// Set parses value and assigns it to key. A switch's group key takes a
// comma separated list. dev is left untouched when value is invalid.
func Set(dev *light.Device, key, value string) error {
	individual, sw, err := field(dev, key)
	if err != nil {
		return err
	}
	if individual != nil {
		a, err := cemi.NewIndividualAddrString(value)
		if err != nil {
			return fmt.Errorf("config: %s: %w", key, err)
		}
		*individual = uint16(a)
		return nil
	}
	if err := parseGroups(value, sw); err != nil {
		return fmt.Errorf("config: %s: %w", key, err)
	}
	return nil
}

// Format renders dev as ini file contents that Parse reads back.
func Format(dev *light.Device) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "[%s]\n%s = %s\n", deviceSection, individualKey, cemi.IndividualAddr(dev.Individual))
	for _, sw := range dev.Switches {
		fmt.Fprintf(&b, "\n[%s]\n%s = %s\n", sw.Name, groupKey, formatGroups(&sw))
	}
	return b.Bytes()
}
