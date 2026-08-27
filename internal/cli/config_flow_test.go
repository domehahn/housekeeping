package cli

import (
	"strings"
	"testing"
)

func TestConfigValidate_RejectsInvalidConfig(t *testing.T) {
	e := testEnv(nil) // config.Default() has no base_url/token_env
	_, _, err := runCmd(newConfigCmd(e), []string{"validate"})
	if err == nil {
		t.Fatal("expected an error for a config with no provider.gitlab.base_url/token_env")
	}
	if got := exitCodeOf(err); got != ExitInvalidConfiguration {
		t.Errorf("exit code = %d, want %d", got, ExitInvalidConfiguration)
	}
}

func TestConfigValidate_RejectsMissingTokenEnvVariable(t *testing.T) {
	e := testEnv(nil)
	e.cfg.Provider.GitLab.BaseURL = "https://gitlab.example.com"
	e.cfg.Provider.GitLab.TokenEnv = "SCM_CLEANER_TEST_DEFINITELY_UNSET_VAR"

	_, _, err := runCmd(newConfigCmd(e), []string{"validate"})
	if err == nil {
		t.Fatal("expected an error when the named token_env variable is not actually set")
	}
}

func TestConfigValidate_HappyPath(t *testing.T) {
	t.Setenv("SCM_CLEANER_TEST_TOKEN", "x")
	e := testEnv(nil)
	e.cfg.Provider.GitLab.BaseURL = "https://gitlab.example.com"
	e.cfg.Provider.GitLab.TokenEnv = "SCM_CLEANER_TEST_TOKEN"

	stdout, _, err := runCmd(newConfigCmd(e), []string{"validate"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "valid") {
		t.Errorf("expected confirmation output, got:\n%s", stdout)
	}
}
