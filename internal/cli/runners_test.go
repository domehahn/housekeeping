package cli

import (
	"strings"
	"testing"

	"github.com/domehahn/housekeeping/internal/domain"
)

func TestRunnersEvaluate_RequiresTag(t *testing.T) {
	e := withGroup(testEnv(newFakeClient()), "company", false)
	_, _, err := runCmd(newRunnersCmd(e), []string{"evaluate"})
	if err == nil {
		t.Fatal("expected an error when --tag is missing")
	}
	if got := exitCodeOf(err); got != ExitInvalidConfiguration {
		t.Errorf("exit code = %d, want %d", got, ExitInvalidConfiguration)
	}
}

func TestRunnersEvaluate_ReportsOutOfScopeImpact(t *testing.T) {
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a"}}
	c.runners = []domain.Runner{
		{ID: "500", Description: "shared", Shared: true, ImpactKnown: true, OutOfScopeProjectPaths: []string{"other/x"}},
	}
	e := withGroup(testEnv(c), "company", false)

	stdout, _, err := runCmd(newRunnersCmd(e), []string{"evaluate", "--tag", "k8s-runner"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "total out-of-scope impact 1") {
		t.Errorf("expected the out-of-scope impact to be reported, got:\n%s", stdout)
	}
}

func TestRunnersList_ShowsRunners(t *testing.T) {
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a"}}
	c.runners = []domain.Runner{{ID: "500", Description: "shared-runner", TagList: []string{"docker"}}}
	e := withGroup(testEnv(c), "company", false)

	stdout, _, err := runCmd(newRunnersCmd(e), []string{"list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "shared-runner") {
		t.Errorf("expected the runner to be listed, got:\n%s", stdout)
	}
}

func TestRunnersPlan_WritesPlanFile(t *testing.T) {
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a"}}
	c.runners = []domain.Runner{{ID: "500", Description: "shared-runner", ImpactKnown: true}}
	c.info = fakeInfo()
	e := withGroup(testEnv(c), "company", false)

	planPath := t.TempDir() + "/plan.json"
	stdout, _, err := runCmd(newRunnersCmd(e), []string{"plan", "--tag", "k8s-runner", "--output-plan", planPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "Plan written to") {
		t.Errorf("expected confirmation that the plan was written, got:\n%s", stdout)
	}
}
