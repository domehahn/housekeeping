package cli

import (
	"testing"

	"github.com/domehahn/housekeeping/internal/secrets"
)

func TestTokenEnvFlagNormalizesToStructuredEnvironmentReference(t *testing.T) {
	e := &env{}
	e.flags.gitlabURL = "https://gitlab.example.com"
	e.flags.tokenEnv = "OVERRIDE_TOKEN"
	if err := e.loadConfig(); err != nil {
		t.Fatal(err)
	}
	ref, err := e.cfg.Provider.GitLab.SecretReference()
	if err != nil {
		t.Fatal(err)
	}
	if ref != (secrets.Reference{Source: secrets.SourceEnv, Env: "OVERRIDE_TOKEN"}) {
		t.Fatalf("secret reference = %+v", ref)
	}
	if e.cfg.Provider.GitLab.TokenEnv != "" {
		t.Fatalf("legacy token_env was not normalized: %q", e.cfg.Provider.GitLab.TokenEnv)
	}
}
