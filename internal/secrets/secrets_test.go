package secrets

import (
	"context"
	"errors"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"
)

type resolverFunc func(context.Context, Reference) (string, error)

func (f resolverFunc) Resolve(ctx context.Context, ref Reference) (string, error) {
	return f(ctx, ref)
}

type fakeKeyring struct {
	service string
	account string
	value   string
	err     error
}

func (f *fakeKeyring) Get(service, account string) (string, error) {
	f.service, f.account = service, account
	return f.value, f.err
}

func TestEnvResolver(t *testing.T) {
	tests := []struct {
		name      string
		lookup    EnvLookup
		want      string
		wantError string
	}{
		{name: "set", lookup: func(string) (string, bool) { return "secret-value", true }, want: "secret-value"},
		{name: "missing", lookup: func(string) (string, bool) { return "", false }, wantError: "not set"},
		{name: "empty", lookup: func(string) (string, bool) { return "", true }, wantError: "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (EnvResolver{Lookup: tt.lookup}).Resolve(context.Background(), Reference{Source: SourceEnv, Env: "TOKEN"})
			if got != tt.want || (tt.wantError != "" && (err == nil || !strings.Contains(err.Error(), tt.wantError))) {
				t.Fatalf("Resolve() = %q, %v", got, err)
			}
			if err != nil && strings.Contains(err.Error(), "secret-value") {
				t.Fatal("error leaked the secret value")
			}
		})
	}
}

func TestKeychainResolver(t *testing.T) {
	t.Run("explicit account", func(t *testing.T) {
		store := &fakeKeyring{value: "secret-value"}
		got, err := (KeychainResolver{Keyring: store}).Resolve(context.Background(), Reference{
			Source: SourceKeychain, Service: "scm-cleaner", Account: "alice",
		})
		if err != nil || got != "secret-value" || store.service != "scm-cleaner" || store.account != "alice" {
			t.Fatalf("Resolve() = %q, %v; lookup = %s/%s", got, err, store.service, store.account)
		}
	})

	t.Run("defaults account", func(t *testing.T) {
		store := &fakeKeyring{value: "secret-value"}
		got, err := (KeychainResolver{
			Keyring:     store,
			CurrentUser: func() (string, error) { return "os-user", nil },
		}).Resolve(context.Background(), Reference{Source: SourceKeychain, Service: "scm-cleaner"})
		if err != nil || got != "secret-value" || store.account != "os-user" {
			t.Fatalf("Resolve() = %q, %v; account = %q", got, err, store.account)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := (KeychainResolver{Keyring: &fakeKeyring{err: keyring.ErrNotFound}}).Resolve(
			context.Background(), Reference{Source: SourceKeychain, Service: "scm-cleaner", Account: "alice"})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("Resolve() error = %v", err)
		}
	})

	t.Run("backend error does not expose prior value", func(t *testing.T) {
		_, err := (KeychainResolver{Keyring: &fakeKeyring{err: errors.New("backend exposed secret-value")}}).Resolve(
			context.Background(), Reference{Source: SourceKeychain, Service: "scm-cleaner", Account: "alice"})
		if err == nil || strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("Resolve() error = %v", err)
		}
	})
}

func TestResolversDoNotCacheValues(t *testing.T) {
	calls := 0
	resolver := EnvResolver{Lookup: func(string) (string, bool) {
		calls++
		if calls == 1 {
			return "first", true
		}
		return "second", true
	}}
	ref := Reference{Source: SourceEnv, Env: "TOKEN"}
	first, err := resolver.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolver.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if first != "first" || second != "second" || calls != 2 {
		t.Fatalf("values = %q/%q, lookups = %d", first, second, calls)
	}
}

func TestRegistryDispatchesExactlyOnceWithoutFallback(t *testing.T) {
	envCalls, keychainCalls := 0, 0
	registry, err := NewRegistry(map[Source]Resolver{
		SourceEnv: resolverFunc(func(context.Context, Reference) (string, error) {
			envCalls++
			return "", errors.New("env failed")
		}),
		SourceKeychain: resolverFunc(func(context.Context, Reference) (string, error) {
			keychainCalls++
			return "unexpected", nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Resolve(context.Background(), Reference{Source: SourceEnv, Env: "TOKEN"}); err == nil {
		t.Fatal("expected environment backend error")
	}
	if envCalls != 1 || keychainCalls != 0 {
		t.Fatalf("calls: env=%d keychain=%d", envCalls, keychainCalls)
	}
}

func TestRegistryRejectsInvalidReferencesBeforeDispatch(t *testing.T) {
	calls := 0
	registry, err := NewRegistry(map[Source]Resolver{
		SourceEnv: resolverFunc(func(context.Context, Reference) (string, error) {
			calls++
			return "unexpected", nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.Resolve(context.Background(), Reference{Source: SourceEnv, Env: "TOKEN", Service: "invalid"})
	if err == nil || calls != 0 {
		t.Fatalf("Resolve() error = %v, backend calls = %d", err, calls)
	}
}

func TestRegistryHonorsCanceledContextBeforeDispatch(t *testing.T) {
	calls := 0
	registry, err := NewRegistry(map[Source]Resolver{
		SourceEnv: resolverFunc(func(context.Context, Reference) (string, error) {
			calls++
			return "unexpected", nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = registry.Resolve(ctx, Reference{Source: SourceEnv, Env: "TOKEN"})
	if err == nil || calls != 0 {
		t.Fatalf("Resolve() error = %v, backend calls = %d", err, calls)
	}
}
