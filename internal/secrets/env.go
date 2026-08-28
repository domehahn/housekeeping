package secrets

import (
	"context"
	"fmt"
	"os"
)

// EnvLookup is the injectable subset of the process environment used by the
// environment resolver.
type EnvLookup func(string) (string, bool)

// EnvResolver resolves credentials from process environment variables.
type EnvResolver struct {
	Lookup EnvLookup
}

// Resolve reads the configured environment variable. An unset variable and
// an explicitly empty variable are reported separately.
func (r EnvResolver) Resolve(ctx context.Context, ref Reference) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("environment lookup canceled: %w", err)
	}
	if ref.Source != SourceEnv {
		return "", fmt.Errorf("environment resolver received source %q", ref.Source)
	}
	if ref.Env == "" {
		return "", fmt.Errorf("environment variable name is required")
	}
	lookup := r.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, ok := lookup(ref.Env)
	if !ok {
		return "", fmt.Errorf("environment variable %q is not set", ref.Env)
	}
	if value == "" {
		return "", fmt.Errorf("environment variable %q is empty", ref.Env)
	}
	return value, nil
}
