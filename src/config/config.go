package config

import (
	_ "embed"
	"fmt"
	"knixx/light"

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
