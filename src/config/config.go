package config

import (
	"bytes"
	_ "embed"
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
		group, err := cemi.NewGroupAddrString(v)
		if err != nil {
			return nil, fmt.Errorf("config: [%s] %s: %w", name, groupKey, err)
		}
		dev.Switches[i] = light.Switch{Name: name, Group: uint16(group)}
	}

	return &dev, nil
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

// field resolves a "section.key" name to the address it refers to.
func field(dev *light.Device, key string) (addr *uint16, individual bool, err error) {
	section, name, _ := strings.Cut(key, ".")
	if strings.EqualFold(section, deviceSection) && strings.EqualFold(name, individualKey) {
		return &dev.Individual, true, nil
	}
	if sw := dev.Switch(section); sw != nil && strings.EqualFold(name, groupKey) {
		return &sw.Group, false, nil
	}
	return nil, false, fmt.Errorf("config: unknown key %q", key)
}

// Get returns the value of key in ini notation.
func Get(dev *light.Device, key string) (string, error) {
	addr, individual, err := field(dev, key)
	if err != nil {
		return "", err
	}
	if individual {
		return cemi.IndividualAddr(*addr).String(), nil
	}
	return cemi.GroupAddr(*addr).String(), nil
}

// Set parses value and assigns it to key. dev is left untouched when value
// is invalid.
func Set(dev *light.Device, key, value string) error {
	addr, individual, err := field(dev, key)
	if err != nil {
		return err
	}
	if individual {
		a, err := cemi.NewIndividualAddrString(value)
		if err != nil {
			return fmt.Errorf("config: %s: %w", key, err)
		}
		*addr = uint16(a)
	} else {
		a, err := cemi.NewGroupAddrString(value)
		if err != nil {
			return fmt.Errorf("config: %s: %w", key, err)
		}
		*addr = uint16(a)
	}
	return nil
}

// Format renders dev as ini file contents that Parse reads back.
func Format(dev *light.Device) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "[%s]\n%s = %s\n", deviceSection, individualKey, cemi.IndividualAddr(dev.Individual))
	for _, sw := range dev.Switches {
		fmt.Fprintf(&b, "\n[%s]\n%s = %s\n", sw.Name, groupKey, cemi.GroupAddr(sw.Group))
	}
	return b.Bytes()
}
