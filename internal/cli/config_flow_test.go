package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/domehahn/housekeeping/internal/config"
	"github.com/domehahn/housekeeping/internal/secrets"
)

type staticSecretResolver struct {
	ref   secrets.Reference
	value string
	err   error
}

func (r *staticSecretResolver) Resolve(_ context.Context, ref secrets.Reference) (string, error) {
	r.ref = ref
	return r.value, r.err
}

func TestConfigValidate_RejectsInvalidConfig(t *testing.T) {
	e := testEnv(nil) // config.Default() has no base URL or token reference.
	_, _, err := runCmd(newConfigCmd(e), []string{"validate"})
	if err == nil {
		t.Fatal("expected an error for a config with no GitLab base URL/token reference")
	}
	if got := exitCodeOf(err); got != ExitInvalidConfiguration {
		t.Errorf("exit code = %d, want %d", got, ExitInvalidConfiguration)
	}
}

func TestConfigValidate_RejectsMissingTokenEnvVariable(t *testing.T) {
	e := testEnv(nil)
	e.cfg.Provider.GitLab.BaseURL = "https://gitlab.example.com"
	e.cfg.Provider.GitLab.Token = config.TokenConfig{Source: secrets.SourceEnv, Env: "SCM_CLEANER_TEST_DEFINITELY_UNSET_VAR"}

	_, _, err := runCmd(newConfigCmd(e), []string{"validate"})
	if err == nil {
		t.Fatal("expected an error when the named token environment variable is not set")
	}
}

func TestConfigValidate_HappyPath(t *testing.T) {
	t.Setenv("SCM_CLEANER_TEST_TOKEN", "x")
	e := testEnv(nil)
	e.cfg.Provider.GitLab.BaseURL = "https://gitlab.example.com"
	e.cfg.Provider.GitLab.Token = config.TokenConfig{Source: secrets.SourceEnv, Env: "SCM_CLEANER_TEST_TOKEN"}

	stdout, _, err := runCmd(newConfigCmd(e), []string{"validate"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "valid") {
		t.Errorf("expected confirmation output, got:\n%s", stdout)
	}
}

func TestConfigValidate_KeychainReferenceUsesInjectedResolver(t *testing.T) {
	e := testEnv(nil)
	e.cfg.Provider.GitLab.BaseURL = "https://gitlab.example.com"
	e.cfg.Provider.GitLab.Token = config.TokenConfig{Source: secrets.SourceKeychain, Service: "scm-cleaner", Account: "alice"}
	resolver := &staticSecretResolver{value: "secret-value"}
	e.secretResolver = resolver

	stdout, _, err := runCmd(newConfigCmd(e), []string{"validate"})
	if err != nil || !strings.Contains(stdout, "valid") {
		t.Fatalf("validate output = %q, error = %v", stdout, err)
	}
	if resolver.ref != (secrets.Reference{Source: secrets.SourceKeychain, Service: "scm-cleaner", Account: "alice"}) {
		t.Fatalf("resolver reference = %+v", resolver.ref)
	}
}
