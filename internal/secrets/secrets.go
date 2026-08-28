// Package secrets resolves references to credentials without exposing secret
// storage details to providers or application code.
package secrets

import (
	"context"
	"fmt"
)

// Source identifies exactly one configured secret backend.
type Source string

const (
	SourceEnv      Source = "env"
	SourceKeychain Source = "keychain"
)

// Reference identifies a secret. It deliberately contains no literal secret
// value and is safe to keep in configuration-derived runtime state.
type Reference struct {
	Source  Source
	Env     string
	Service string
	Account string
}

// Validate rejects ambiguous or incomplete references independently of any
// configuration format.
func (r Reference) Validate() error {
	switch r.Source {
	case SourceEnv:
		if r.Env == "" {
			return fmt.Errorf("secrets: environment variable name is required")
		}
		if r.Service != "" || r.Account != "" {
			return fmt.Errorf("secrets: keychain fields are invalid for source env")
		}
	case SourceKeychain:
		if r.Service == "" {
			return fmt.Errorf("secrets: keychain service is required")
		}
		if r.Env != "" {
			return fmt.Errorf("secrets: environment field is invalid for source keychain")
		}
	default:
		return fmt.Errorf("secrets: unsupported source %q", r.Source)
	}
	return nil
}

// Resolver resolves a reference to its secret value.
type Resolver interface {
	Resolve(context.Context, Reference) (string, error)
}

// Registry dispatches a reference to exactly one configured backend. It does
// not fall back to another source when resolution fails.
type Registry struct {
	backends map[Source]Resolver
}

// NewRegistry constructs a resolver registry from the supplied backends.
func NewRegistry(backends map[Source]Resolver) (*Registry, error) {
	if len(backends) == 0 {
		return nil, fmt.Errorf("secrets: at least one backend is required")
	}
	copyOfBackends := make(map[Source]Resolver, len(backends))
	for source, backend := range backends {
		if source == "" || backend == nil {
			return nil, fmt.Errorf("secrets: backend source and resolver must be set")
		}
		copyOfBackends[source] = backend
	}
	return &Registry{backends: copyOfBackends}, nil
}

// Resolve dispatches to the backend selected by ref.Source.
func (r *Registry) Resolve(ctx context.Context, ref Reference) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("secrets: resolve canceled: %w", err)
	}
	if err := ref.Validate(); err != nil {
		return "", err
	}
	backend, ok := r.backends[ref.Source]
	if !ok {
		return "", fmt.Errorf("secrets: unsupported source %q", ref.Source)
	}
	value, err := backend.Resolve(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("secrets: resolve from %s: %w", ref.Source, err)
	}
	return value, nil
}
