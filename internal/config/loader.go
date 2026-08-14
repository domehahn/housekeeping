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

	data, err := os.ReadFile(path)
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

// ResolveToken looks up the access token from the environment variable
// named by the provider's token_env setting. It never returns a default or
// falls back to a literal token in the config file - tokens must only ever
// come from the environment.
func ResolveToken(tokenEnv string) (string, error) {
	if tokenEnv == "" {
		return "", fmt.Errorf("config: no token_env configured")
	}
	val, ok := os.LookupEnv(tokenEnv)
	if !ok || val == "" {
		return "", fmt.Errorf("config: environment variable %s is not set", tokenEnv)
	}
	return val, nil
}
