package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/domehahn/housekeeping/internal/app"
	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/provider"
)

func writeTestPlan(t *testing.T, actions []domain.PlannedAction) string {
	t.Helper()
	plan := domain.Plan{
		Version:   domain.PlanVersion,
		Provider:  "gitlab",
		Instance:  "https://gitlab.example.com",
		Scope:     domain.PlanScope{Type: domain.ScopeTypeGroup, ID: "1", Path: "company"},
		CreatedAt: time.Now(),
		Actions:   actions,
	}
	path := t.TempDir() + "/plan.json"
	if err := app.SavePlan(path, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	return path
}

func TestExecute_DryRunIsDefault(t *testing.T) {
	c := newFakeClient()
	c.info = fakeInfo()
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a", LastActivityAt: domain.Known(nil)}}
	e := testEnv(c)

	planPath := writeTestPlan(t, []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeProject, ResourceID: "1", ResourceName: "company/a", Action: domain.ActionDeleteProject, EvaluatedAt: time.Now()},
	})

	stdout, _, err := runCmd(newExecuteCmd(e), []string{planPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.deletedProjects["1"] {
		t.Error("execute without --apply must never delete anything")
	}
	if !strings.Contains(stdout, "dry_run") {
		t.Errorf("expected dry_run result in output, got:\n%s", stdout)
	}
}

func TestExecute_ApplyNonInteractiveRequiresConfirmScope(t *testing.T) {
	c := newFakeClient()
	c.info = fakeInfo()
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a", LastActivityAt: domain.Known(nil)}}
	e := testEnv(c)

	planPath := writeTestPlan(t, []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeProject, ResourceID: "1", ResourceName: "company/a", Action: domain.ActionDeleteProject, EvaluatedAt: time.Now()},
	})

	_, _, err := runCmd(newExecuteCmd(e), []string{planPath, "--apply", "--non-interactive"})
	if err == nil {
		t.Fatal("expected an error: --apply --non-interactive without --confirm-scope must be rejected")
	}
	if c.deletedProjects["1"] {
		t.Error("must not delete anything when confirmation is missing")
	}
}

func TestExecute_ApplyNonInteractiveWithConfirmScopeDeletes(t *testing.T) {
	c := newFakeClient()
	c.info = fakeInfo()
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a", LastActivityAt: domain.Known(nil)}}
	e := testEnv(c)

	planPath := writeTestPlan(t, []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeProject, ResourceID: "1", ResourceName: "company/a", Action: domain.ActionDeleteProject, EvaluatedAt: time.Now()},
	})

	stdout, _, err := runCmd(newExecuteCmd(e), []string{planPath, "--apply", "--non-interactive", "--confirm-scope", "company"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.deletedProjects["1"] {
		t.Error("expected the project to actually be deleted")
	}
	if !strings.Contains(stdout, "success") {
		t.Errorf("expected a success result in output, got:\n%s", stdout)
	}
}

func TestExecute_WrongInstanceRejected(t *testing.T) {
	c := newFakeClient()
	c.info = provider.Info{Provider: "gitlab", Instance: "https://gitlab-other.example.com"}
	e := testEnv(c)

	planPath := writeTestPlan(t, []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeProject, ResourceID: "1", ResourceName: "company/a", Action: domain.ActionDeleteProject, EvaluatedAt: time.Now()},
	})

	_, _, err := runCmd(newExecuteCmd(e), []string{planPath})
	if err == nil {
		t.Fatal("expected an error: plan's instance does not match the current provider's instance")
	}
	if got := exitCodeOf(err); got != ExitPlanValidationFailed {
		t.Errorf("exit code = %d, want %d", got, ExitPlanValidationFailed)
	}
}

func TestExecute_SafetyGuardBlocksOversizedPlan(t *testing.T) {
	c := newFakeClient()
	c.info = fakeInfo()
	var actions []domain.PlannedAction
	for i := 0; i < 20; i++ {
		actions = append(actions, domain.PlannedAction{
			ResourceType: domain.ResourceTypeProject, ResourceID: string(rune('a' + i)),
			ResourceName: "company/x", Action: domain.ActionDeleteProject, EvaluatedAt: time.Now(),
		})
	}
	e := testEnv(c)
	e.cfg.Safety.MaxActions.Projects = 5

	planPath := writeTestPlan(t, actions)
	_, _, err := runCmd(newExecuteCmd(e), []string{planPath})
	if err == nil {
		t.Fatal("expected the safety guard to reject an oversized plan even in dry-run mode")
	}
	if got := exitCodeOf(err); got != ExitSafetyGuardTriggered {
		t.Errorf("exit code = %d, want %d", got, ExitSafetyGuardTriggered)
	}
}

func TestExecute_RunnerActionRequiresOutOfScopeConfirmation(t *testing.T) {
	c := newFakeClient()
	c.info = fakeInfo()
	c.runnerTags["500"] = []string{}
	e := testEnv(c)

	planPath := writeTestPlan(t, []domain.PlannedAction{
		{
			ResourceType: domain.ResourceTypeRunner, ResourceID: "500", ResourceName: "shared",
			Action: domain.ActionAddRunnerTag, TagValue: "k8s-runner", EvaluatedAt: time.Now(),
			OutOfScopeProjectCount: 2, OutOfScopeProjectPaths: []string{"other/x", "other/y"},
		},
	})

	_, _, err := runCmd(newExecuteCmd(e), []string{planPath, "--apply", "--non-interactive", "--confirm-scope", "company"})
	if err == nil {
		t.Fatal("expected an error: out-of-scope impact must be confirmed before --apply")
	}
	if len(c.runnerTags["500"]) != 0 {
		t.Error("must not update runner tags without the out-of-scope confirmation")
	}

	_, _, err = runCmd(newExecuteCmd(testEnv(c)), []string{
		planPath, "--apply", "--non-interactive", "--confirm-scope", "company", "--confirm-out-of-scope-impact", "2",
	})
	if err != nil {
		t.Fatalf("expected the matching confirmation to allow execution, got: %v", err)
	}
	if len(c.runnerTags["500"]) != 1 || c.runnerTags["500"][0] != "k8s-runner" {
		t.Errorf("expected runner 500 to receive the tag, got %v", c.runnerTags["500"])
	}
}

func TestExecute_PartialFailureExitCode(t *testing.T) {
	c := newFakeClient()
	c.info = fakeInfo()
	c.projects = []domain.Project{
		{ID: "1", FullPath: "company/a", LastActivityAt: domain.Known(nil)},
		{ID: "2", FullPath: "company/b", LastActivityAt: domain.Known(nil)},
	}
	c.failDeleteProject = "1" // project 1 fails, project 2 succeeds
	e := testEnv(c)

	planPath := writeTestPlan(t, []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeProject, ResourceID: "1", ResourceName: "company/a", Action: domain.ActionDeleteProject, EvaluatedAt: time.Now()},
		{ResourceType: domain.ResourceTypeProject, ResourceID: "2", ResourceName: "company/b", Action: domain.ActionDeleteProject, EvaluatedAt: time.Now()},
	})

	stdout, _, err := runCmd(newExecuteCmd(e), []string{planPath, "--apply", "--non-interactive", "--confirm-scope", "company"})
	if got := exitCodeOf(err); got != ExitPartialExecution {
		t.Errorf("exit code = %d, want %d", got, ExitPartialExecution)
	}
	if !c.deletedProjects["2"] {
		t.Error("expected project 2 to still be deleted despite project 1 failing")
	}
	if !strings.Contains(stdout, "failed") || !strings.Contains(stdout, "success") {
		t.Errorf("expected both a failure and a success in output, got:\n%s", stdout)
	}
}
