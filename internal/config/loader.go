package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Load reads and validates a YAML configuration file, merging it on top of
// Default(). Unknown fields are rejected so typos in the config file fail
// loudly instead of being silently ignored.
func Load(path string) (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path) //nolint:gosec // path is the operator's own --config CLI argument, not attacker-controlled input
	if err != nil {
		return Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
