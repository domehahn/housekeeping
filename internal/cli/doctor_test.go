package cli

import (
	"strings"
	"testing"

	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/provider"
)

func TestDoctor_InvalidConfigStopsAtFirstCheck(t *testing.T) {
	e := testEnv(newFakeClient()) // config.Default() has no base URL or token reference.
	stdout, _, err := runCmd(newDoctorCmd(e), nil)
	if err != nil {
		t.Fatalf("doctor itself must not error, it reports failures in its table: %v", err)
	}
	if !strings.Contains(stdout, "Configuration") || !strings.Contains(stdout, "FAILED") {
		t.Errorf("expected a FAILED Configuration check, got:\n%s", stdout)
	}
}

func TestDoctor_HappyPathWithGroup(t *testing.T) {
	c := newFakeClient()
	c.info = fakeInfo()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.capabilities = provider.Capabilities{DeleteProjects: provider.SupportRequiresOwner}
	e := testEnv(c)
	e.cfg.Provider.GitLab.BaseURL = "https://gitlab.example.com"
	e.cfg.Provider.GitLab.TokenEnv = "GITLAB_TOKEN"
	e.cfg.Scope.Group = "company"

	stdout, _, err := runCmd(newDoctorCmd(e), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"Configuration", "OK", "API connectivity", "FOUND (company)"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected doctor output to contain %q, got:\n%s", want, stdout)
		}
	}
}

func TestDoctor_SkipsGroupCheckWhenNoGroupConfigured(t *testing.T) {
	c := newFakeClient()
	c.info = fakeInfo()
	e := testEnv(c)
	e.cfg.Provider.GitLab.BaseURL = "https://gitlab.example.com"
	e.cfg.Provider.GitLab.TokenEnv = "GITLAB_TOKEN"

	stdout, _, err := runCmd(newDoctorCmd(e), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "SKIPPED") {
		t.Errorf("expected the group check to be skipped, got:\n%s", stdout)
	}
}
