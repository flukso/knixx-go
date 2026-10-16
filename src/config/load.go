//go:build !tinygo

package config

import (
	"fmt"
	"knixx/light"
	"os"
)

// Load parses the ini file at path.
func Load(path string) (*light.Device, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return Parse(data)
}
