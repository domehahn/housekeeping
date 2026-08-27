package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/provider"
)

// withGroup is a small helper: --group/--recursive are persistent flags
// registered on the real root command, not on a command subtree tested in
// isolation, so tests configure scope the same way the root command's
// PersistentPreRunE would - by setting cfg.Scope directly.
func withGroup(e *env, group string, recursive bool) *env {
	e.cfg.Scope.Group = group
	e.cfg.Scope.Recursive = recursive
	return e
}

func TestProjectsList_RequiresGroup(t *testing.T) {
	e := testEnv(newFakeClient())
	_, _, err := runCmd(newProjectsCmd(e), []string{"list"})
	if err == nil {
		t.Fatal("expected an error when no --group/scope.group is configured")
	}
	if got := exitCodeOf(err); got != ExitInvalidConfiguration {
		t.Errorf("exit code = %d, want %d", got, ExitInvalidConfiguration)
	}
}

func TestProjectsList_HappyPath(t *testing.T) {
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.projects = []domain.Project{
		{ID: "1", FullPath: "company/a", LastActivityAt: domain.Known(nil)},
	}
	e := withGroup(testEnv(c), "company", false)

	stdout, _, err := runCmd(newProjectsCmd(e), []string{"list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "company/a") {
		t.Errorf("expected output to list the discovered project, got:\n%s", stdout)
	}
}

func TestProjectsEvaluate_NoPolicyConfigured(t *testing.T) {
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	e := withGroup(testEnv(c), "company", false)

	_, _, err := runCmd(newProjectsCmd(e), []string{"evaluate"})
	if err == nil {
		t.Fatal("expected an error when no project policy is enabled at all")
	}
	if got := exitCodeOf(err); got != ExitInvalidConfiguration {
		t.Errorf("exit code = %d, want %d", got, ExitInvalidConfiguration)
	}
}

func TestProjectsEvaluate_MatchesInactiveProject(t *testing.T) {
	old := time.Now().Add(-100 * 24 * time.Hour)
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.projects = []domain.Project{
		{ID: "1", FullPath: "company/stale", LastActivityAt: domain.Known(&old)},
	}
	e := withGroup(testEnv(c), "company", false)

	stdout, _, err := runCmd(newProjectsCmd(e), []string{"evaluate", "--inactive-for", "90d"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "company/stale") {
		t.Errorf("expected the stale project to be reported as matched, got:\n%s", stdout)
	}
}

func TestProjectsPlan_WritesPlanFile(t *testing.T) {
	old := time.Now().Add(-100 * 24 * time.Hour)
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.projects = []domain.Project{
		{ID: "1", FullPath: "company/stale", LastActivityAt: domain.Known(&old)},
	}
	c.info = fakeInfo()
	e := withGroup(testEnv(c), "company", false)

	planPath := t.TempDir() + "/plan.json"
	stdout, _, err := runCmd(newProjectsCmd(e), []string{
		"plan", "--inactive-for", "90d", "--action", "delete", "--output-plan", planPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "Plan written to") {
		t.Errorf("expected confirmation that the plan was written, got:\n%s", stdout)
	}
}

func TestProjectsPlan_SafetyGuardBlocksOversizedPlan(t *testing.T) {
	old := time.Now().Add(-100 * 24 * time.Hour)
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	for i := 0; i < 20; i++ {
		c.projects = append(c.projects, domain.Project{
			ID: string(rune('a' + i)), FullPath: "company/x", LastActivityAt: domain.Known(&old),
		})
	}
	c.info = fakeInfo()
	e := withGroup(testEnv(c), "company", false)
	e.cfg.Safety.MaxActions.Projects = 5

	_, _, err := runCmd(newProjectsCmd(e), []string{
		"plan", "--inactive-for", "90d", "--action", "delete",
	})
	if err == nil {
		t.Fatal("expected the safety guard to reject a plan exceeding max_actions.projects")
	}
	if got := exitCodeOf(err); got != ExitSafetyGuardTriggered {
		t.Errorf("exit code = %d, want %d", got, ExitSafetyGuardTriggered)
	}
}

// fakeInfo is a reusable provider.Info for tests that need client.Info()
// to succeed (e.g. any `plan` command, which stamps the plan's
// provider/instance).
func fakeInfo() provider.Info {
	return provider.Info{Provider: "gitlab", Instance: "https://gitlab.example.com", AuthenticatedAs: "bot"}
}

// exitCodeOf extracts the ExitError code from a command error, or -1 if
// err is nil or not an *ExitError.
func exitCodeOf(err error) int {
	if err == nil {
		return -1
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	return -1
}
