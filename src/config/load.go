//go:build !tinygo

package config

import (
	"fmt"
	"knixx/thunder"
	"os"
)

// Load parses the ini file at path.
func Load(path string) (*thunder.Device, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return Parse(data)
}

// FileStore saves the configuration to the file at Path.
type FileStore struct {
	Path string
}

func (s FileStore) Save(data []byte) error {
	if err := os.WriteFile(s.Path, data, 0o644); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return nil
}
