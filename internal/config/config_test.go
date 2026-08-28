package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/domehahn/housekeeping/internal/secrets"
)

func TestValidate(t *testing.T) {
	valid := func() Config {
		c := Default()
		c.Provider.GitLab.BaseURL = "https://gitlab.example.com"
		c.Provider.GitLab.Token = TokenConfig{Source: secrets.SourceEnv, Env: "GITLAB_TOKEN"}
		return c
	}

	t.Run("valid default config passes", func(t *testing.T) {
		if err := valid().Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing base_url fails", func(t *testing.T) {
		c := valid()
		c.Provider.GitLab.BaseURL = ""
		if err := c.Validate(); err == nil {
			t.Error("expected error")
		}
	})

	t.Run("unknown provider type fails", func(t *testing.T) {
		c := valid()
		c.Provider.Type = "bitbucket"
		if err := c.Validate(); err == nil {
			t.Error("expected error for provider type not yet implemented")
		}
	})

	t.Run("invalid regex fails", func(t *testing.T) {
		c := valid()
		c.Projects.Include = []string{"("}
		if err := c.Validate(); err == nil {
			t.Error("expected error for invalid regex")
		}
	})

	t.Run("invalid match mode fails", func(t *testing.T) {
		c := valid()
		c.Users.Inactive.Match = "sometimes"
		if err := c.Validate(); err == nil {
			t.Error("expected error for invalid match mode")
		}
	})

	t.Run("invalid unknown_activity fails", func(t *testing.T) {
		c := valid()
		c.Users.UnknownActivity = "delete-them-all"
		if err := c.Validate(); err == nil {
			t.Error("expected error for invalid unknown_activity")
		}
	})

	t.Run("negative safety limits fail", func(t *testing.T) {
		c := valid()
		c.Safety.MaxActions.Projects = -1
		if err := c.Validate(); err == nil {
			t.Error("expected error for negative max_actions")
		}
	})

	t.Run("percentage out of range fails", func(t *testing.T) {
		c := valid()
		c.Safety.MaxPercentage.Projects = 150
		if err := c.Validate(); err == nil {
			t.Error("expected error for percentage > 100")
		}
	})
}

func TestGitLabSecretReference(t *testing.T) {
	tests := []struct {
		name      string
		config    GitLabConfig
		want      secrets.Reference
		wantError string
	}{
		{
			name: "structured environment", config: GitLabConfig{Token: TokenConfig{Source: secrets.SourceEnv, Env: "GITLAB_TOKEN"}},
			want: secrets.Reference{Source: secrets.SourceEnv, Env: "GITLAB_TOKEN"},
		},
		{
			name: "structured keychain", config: GitLabConfig{Token: TokenConfig{Source: secrets.SourceKeychain, Service: "scm-cleaner", Account: "alice"}},
			want: secrets.Reference{Source: secrets.SourceKeychain, Service: "scm-cleaner", Account: "alice"},
		},
		{
			name: "keychain account optional", config: GitLabConfig{Token: TokenConfig{Source: secrets.SourceKeychain, Service: "scm-cleaner"}},
			want: secrets.Reference{Source: secrets.SourceKeychain, Service: "scm-cleaner"},
		},
		{
			name: "legacy environment", config: GitLabConfig{TokenEnv: "GITLAB_TOKEN"},
			want: secrets.Reference{Source: secrets.SourceEnv, Env: "GITLAB_TOKEN"},
		},
		{
			name: "both syntaxes", config: GitLabConfig{TokenEnv: "OLD", Token: TokenConfig{Source: secrets.SourceEnv, Env: "NEW"}},
			wantError: "mutually exclusive",
		},
		{name: "unknown source", config: GitLabConfig{Token: TokenConfig{Source: "vault"}}, wantError: "unknown"},
		{name: "missing source", config: GitLabConfig{}, wantError: "source is required"},
		{name: "env missing name", config: GitLabConfig{Token: TokenConfig{Source: secrets.SourceEnv}}, wantError: "token.env is required"},
		{name: "env with keychain fields", config: GitLabConfig{Token: TokenConfig{Source: secrets.SourceEnv, Env: "TOKEN", Service: "bad"}}, wantError: "invalid when source is env"},
		{name: "keychain missing service", config: GitLabConfig{Token: TokenConfig{Source: secrets.SourceKeychain}}, wantError: "service is required"},
		{name: "keychain with env", config: GitLabConfig{Token: TokenConfig{Source: secrets.SourceKeychain, Service: "svc", Env: "bad"}}, wantError: "invalid when source is keychain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.config.SecretReference()
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("SecretReference() error = %v, want containing %q", err, tt.wantError)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("SecretReference() = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestLoad_RejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
version: 1
provider:
  type: gitlab
  gitlab:
    base_url: https://gitlab.example.com
    token_env: GITLAB_TOKEN
totally_unknown_field: true
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("expected error for unknown field, so typos are never silently ignored")
	}
}

func TestLoad_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
version: 1
provider:
  type: gitlab
  gitlab:
    base_url: https://gitlab.example.com
    token_env: GITLAB_TOKEN
scope:
  group: engineering
  recursive: true
users:
  inactive:
    enabled: true
    last_login_days: 30
    last_activity_days: 30
    match: all
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Scope.Group != "engineering" || !cfg.Scope.Recursive {
		t.Errorf("scope not parsed correctly: %+v", cfg.Scope)
	}
	if !cfg.Users.Inactive.Enabled || cfg.Users.Inactive.LastLoginDays != 30 {
		t.Errorf("users.inactive not parsed correctly: %+v", cfg.Users.Inactive)
	}
}

func TestLoad_StructuredSecretReferences(t *testing.T) {
	tests := map[string]string{
		"environment": `token:
      source: env
      env: GITLAB_TOKEN`,
		"keychain": `token:
      source: keychain
      service: scm-cleaner
      account: gitlab-bot`,
	}
	for name, tokenBlock := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			content := "version: 1\nprovider:\n  type: gitlab\n  gitlab:\n    base_url: https://gitlab.example.com\n    " + tokenBlock + "\n"
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err != nil {
				t.Fatalf("Load() error = %v", err)
			}
		})
	}
}

func TestLoad_RejectsLiteralTokenAndInvalidFields(t *testing.T) {
	tests := map[string]string{
		"literal token": `provider:
  type: gitlab
  gitlab:
    base_url: https://gitlab.example.com
    token: literal-secret`,
		"unknown token field": `provider:
  type: gitlab
  gitlab:
    base_url: https://gitlab.example.com
    token:
      source: env
      env: GITLAB_TOKEN
      value: literal-secret`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("expected strict YAML parsing to reject literal/unknown secret field")
			}
		})
	}
}
