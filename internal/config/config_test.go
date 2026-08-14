package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidate(t *testing.T) {
	valid := func() Config {
		c := Default()
		c.Provider.GitLab.BaseURL = "https://gitlab.example.com"
		c.Provider.GitLab.TokenEnv = "GITLAB_TOKEN"
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

func TestResolveToken(t *testing.T) {
	t.Setenv("SCM_CLEANER_TEST_TOKEN", "secret-value")

	got, err := ResolveToken("SCM_CLEANER_TEST_TOKEN")
	if err != nil || got != "secret-value" {
		t.Errorf("ResolveToken = %q, %v", got, err)
	}

	if _, err := ResolveToken("SCM_CLEANER_TEST_TOKEN_MISSING"); err == nil {
		t.Error("expected error for unset environment variable")
	}
}
