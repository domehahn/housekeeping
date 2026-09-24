package cli

import (
	"fmt"
	"path/filepath"
	"slices"
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

func TestExecute_AutoMergeIfNoApprovalRequired_MergesAndConfirms(t *testing.T) {
	c := newFakeClient()
	c.info = fakeInfo()
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a"}}
	c.ciFiles["1"] = []byte("build-job:\n  script: [\"x\"]\n")
	c.proposeURL = "https://gitlab.example.com/company/a/-/merge_requests/42"
	c.autoMergeResult = true
	e := testEnv(c)

	planPath := writeTestPlan(t, []domain.PlannedAction{
		{ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", ResourceName: "company/a",
			Action: domain.ActionAddPipelineTag, TagValue: "k8s-runner", EvaluatedAt: time.Now()},
	})

	stdout, _, err := runCmd(newExecuteCmd(e), []string{
		planPath, "--apply", "--non-interactive", "--confirm-scope", "company", "--auto-merge-if-no-approval-required",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(c.autoMergeCalls, []string{c.proposeURL}) {
		t.Errorf("expected MergeIfNoApprovalRequired to be called, got %v", c.autoMergeCalls)
	}
	if !strings.Contains(stdout, "merge requested") {
		t.Errorf("expected confirmation that a merge was requested, got:\n%s", stdout)
	}
}

func TestExecute_AutoMergeIfNoApprovalRequired_ReportsNeedsApproval(t *testing.T) {
	c := newFakeClient()
	c.info = fakeInfo()
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a"}}
	c.ciFiles["1"] = []byte("build-job:\n  script: [\"x\"]\n")
	c.proposeURL = "https://gitlab.example.com/company/a/-/merge_requests/42"
	c.autoMergeApproval = true
	e := testEnv(c)

	planPath := writeTestPlan(t, []domain.PlannedAction{
		{ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", ResourceName: "company/a",
			Action: domain.ActionAddPipelineTag, TagValue: "k8s-runner", EvaluatedAt: time.Now()},
	})

	stdout, _, err := runCmd(newExecuteCmd(e), []string{
		planPath, "--apply", "--non-interactive", "--confirm-scope", "company", "--auto-merge-if-no-approval-required",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "require approval") || !strings.Contains(stdout, c.proposeURL) {
		t.Errorf("expected a needs-approval report naming the MR URL, got:\n%s", stdout)
	}
}

func TestExecute_AutoMergeIfNoApprovalRequired_OffByDefault(t *testing.T) {
	c := newFakeClient()
	c.info = fakeInfo()
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a"}}
	c.ciFiles["1"] = []byte("build-job:\n  script: [\"x\"]\n")
	c.proposeURL = "https://gitlab.example.com/company/a/-/merge_requests/42"
	e := testEnv(c)

	planPath := writeTestPlan(t, []domain.PlannedAction{
		{ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", ResourceName: "company/a",
			Action: domain.ActionAddPipelineTag, TagValue: "k8s-runner", EvaluatedAt: time.Now()},
	})

	_, _, err := runCmd(newExecuteCmd(e), []string{
		planPath, "--apply", "--non-interactive", "--confirm-scope", "company",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.autoMergeCalls) != 0 {
		t.Errorf("expected MergeIfNoApprovalRequired to never be called without the flag, got %v", c.autoMergeCalls)
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
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a"}}
	c.runners = []domain.Runner{{ID: "500", Description: "shared", ImpactKnown: true, InScopeProjectPaths: []string{"company/a"}, OutOfScopeProjectPaths: []string{"other/x", "other/y"}}}
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

func TestExecute_RunnerTagOutOfScopeConfirmations(t *testing.T) {
	tests := []struct {
		name                 string
		action               domain.ActionType
		resourceType         domain.ResourceType
		outOfScopeCount      int
		outOfScopePaths      []string
		providedConfirmation int
		expectError          bool
		errContains          string
		stdErrContains       string
	}{
		{
			name:                 "add-runner-tag with impact requires exact confirmation (missing)",
			action:               domain.ActionAddRunnerTag,
			resourceType:         domain.ResourceTypeRunner,
			outOfScopeCount:      2,
			outOfScopePaths:      []string{"other/x", "other/y"},
			providedConfirmation: 0,
			expectError:          true,
			errContains:          "pass --confirm-out-of-scope-impact=2",
			stdErrContains:       "other/x",
		},
		{
			name:                 "add-runner-tag with impact rejects incorrect confirmation",
			action:               domain.ActionAddRunnerTag,
			resourceType:         domain.ResourceTypeRunner,
			outOfScopeCount:      2,
			outOfScopePaths:      []string{"other/x", "other/y"},
			providedConfirmation: 1,
			expectError:          true,
			errContains:          "pass --confirm-out-of-scope-impact=2",
			stdErrContains:       "other/y",
		},
		{
			name:                 "add-runner-tag with impact accepts exact confirmation",
			action:               domain.ActionAddRunnerTag,
			resourceType:         domain.ResourceTypeRunner,
			outOfScopeCount:      2,
			outOfScopePaths:      []string{"other/x", "other/y"},
			providedConfirmation: 2,
			expectError:          false,
		},
		{
			name:                 "replace-runner-tag with impact requires exact confirmation (missing)",
			action:               domain.ActionReplaceRunnerTag,
			resourceType:         domain.ResourceTypeRunner,
			outOfScopeCount:      3,
			outOfScopePaths:      []string{"other/a", "other/b", "other/c"},
			providedConfirmation: 0,
			expectError:          true,
			errContains:          "pass --confirm-out-of-scope-impact=3",
			stdErrContains:       "other/a",
		},
		{
			name:                 "replace-runner-tag with impact accepts exact confirmation",
			action:               domain.ActionReplaceRunnerTag,
			resourceType:         domain.ResourceTypeRunner,
			outOfScopeCount:      3,
			outOfScopePaths:      []string{"other/a", "other/b", "other/c"},
			providedConfirmation: 3,
			expectError:          false,
		},
		{
			name:                 "zero-impact runner mutation does not require confirmation",
			action:               domain.ActionAddRunnerTag,
			resourceType:         domain.ResourceTypeRunner,
			outOfScopeCount:      0,
			outOfScopePaths:      nil,
			providedConfirmation: 0,
			expectError:          false,
		},
		{
			name:                 "report-only runner action with out-of-scope count does not trigger confirmation",
			action:               domain.ActionReport,
			resourceType:         domain.ResourceTypeRunner,
			outOfScopeCount:      5,
			outOfScopePaths:      []string{"other/x"},
			providedConfirmation: 0,
			expectError:          false,
		},
		{
			name:                 "future/unknown destructive runner action with impact fails closed",
			action:               domain.ActionType("custom-runner-delete"),
			resourceType:         domain.ResourceTypeRunner,
			outOfScopeCount:      4,
			outOfScopePaths:      []string{"other/z"},
			providedConfirmation: 0,
			expectError:          true,
			errContains:          "is invalid for resource type \"runner\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newFakeClient()
			c.info = fakeInfo()
			c.projects = []domain.Project{{ID: "1", FullPath: "company/a"}}
			c.runners = []domain.Runner{{ID: "500", Description: "shared", ImpactKnown: true, InScopeProjectPaths: []string{"company/a"}, OutOfScopeProjectPaths: tt.outOfScopePaths}}
			c.runnerTags["500"] = []string{"old-tag"}
			e := testEnv(c)

			action := domain.PlannedAction{
				ResourceType:           tt.resourceType,
				ResourceID:             "500",
				ResourceName:           "shared",
				Action:                 tt.action,
				TagValue:               "new-tag",
				TagRenames:             []domain.TagRename{{Old: "old-tag", New: "new-tag"}},
				EvaluatedAt:            time.Now(),
				OutOfScopeProjectCount: tt.outOfScopeCount,
				OutOfScopeProjectPaths: tt.outOfScopePaths,
			}
			planPath := writeTestPlan(t, []domain.PlannedAction{action})

			args := []string{planPath, "--apply", "--non-interactive", "--confirm-scope", "company"}
			if tt.providedConfirmation > 0 {
				args = append(args, "--confirm-out-of-scope-impact", fmt.Sprintf("%d", tt.providedConfirmation))
			}

			_, stderr, err := runCmd(newExecuteCmd(e), args)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContains)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.errContains)
				}
				if tt.stdErrContains != "" && !strings.Contains(stderr, tt.stdErrContains) {
					t.Errorf("stderr = %q, want substring %q", stderr, tt.stdErrContains)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
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

func TestExecute_StalePlanEnforcement(t *testing.T) {
	c := newFakeClient()
	c.info = fakeInfo()
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a", LastActivityAt: domain.Known(nil)}}
	e := testEnv(c)

	actions := []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeProject, ResourceID: "1", ResourceName: "company/a", Action: domain.ActionDeleteProject, EvaluatedAt: time.Now().Add(-48 * time.Hour)},
	}

	plan := domain.Plan{
		Version:   domain.PlanVersion,
		Provider:  "gitlab",
		Instance:  "https://gitlab.example.com",
		Scope:     domain.PlanScope{Type: domain.ScopeTypeGroup, ID: "1", Path: "company"},
		CreatedAt: time.Now().Add(-48 * time.Hour),
		Actions:   actions,
		Discovery: map[domain.ResourceType]int{domain.ResourceTypeProject: 100},
	}
	planPath := filepath.Join(t.TempDir(), "stale-plan.json")
	if err := app.SavePlan(planPath, plan); err != nil {
		t.Fatal(err)
	}

	savedPlan, err := app.LoadPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Without allow-stale-plan flag -> ExitStalePlan
	_, _, err = runCmd(newExecuteCmd(e), []string{planPath})
	if err == nil {
		t.Fatal("expected stale plan to be rejected")
	}
	if got := exitCodeOf(err); got != ExitStalePlan {
		t.Errorf("exit code = %d, want %d", got, ExitStalePlan)
	}

	// 2. With incorrect allow-stale-plan flag -> ExitStalePlan
	_, _, err = runCmd(newExecuteCmd(e), []string{planPath, "--allow-stale-plan", "invalid-hash"})
	if err == nil {
		t.Fatal("expected stale plan with wrong hash to be rejected")
	}

	// 3. With exact matching allow-stale-plan flag -> allowed
	_, _, err = runCmd(newExecuteCmd(e), []string{planPath, "--allow-stale-plan", savedPlan.Hash})
	if err != nil {
		t.Fatalf("expected exact --allow-stale-plan matching hash to proceed, got: %v", err)
	}
}

func TestExecute_ConfigDriftEnforcement(t *testing.T) {
	c := newFakeClient()
	c.info = fakeInfo()
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a", LastActivityAt: domain.Known(nil)}}
	e := testEnv(c)

	actions := []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeProject, ResourceID: "1", ResourceName: "company/a", Action: domain.ActionDeleteProject, EvaluatedAt: time.Now()},
	}

	plan := domain.Plan{
		Version:   domain.PlanVersion,
		Provider:  "gitlab",
		Instance:  "https://gitlab.example.com",
		Scope:     domain.PlanScope{Type: domain.ScopeTypeGroup, ID: "1", Path: "company"},
		CreatedAt: time.Now(),
		Actions:   actions,
		Discovery: map[domain.ResourceType]int{domain.ResourceTypeProject: 100},
		Provenance: &domain.PlanProvenance{
			ConfigDigest: "sha256:old-digest-that-does-not-match",
		},
	}
	planPath := filepath.Join(t.TempDir(), "drift-plan.json")
	if err := app.SavePlan(planPath, plan); err != nil {
		t.Fatal(err)
	}

	_, _, err := runCmd(newExecuteCmd(e), []string{planPath})
	if err == nil {
		t.Fatal("expected plan with mismatched config digest to be rejected")
	}
	if got := exitCodeOf(err); got != ExitConfigDrift {
		t.Errorf("exit code = %d, want %d", got, ExitConfigDrift)
	}
}

func TestExecute_UserBlockConfirmation(t *testing.T) {
	c := newFakeClient()
	c.info = fakeInfo()
	c.members = []domain.User{{ID: "10", Username: "dormant_user", LastLoginAt: domain.Known(nil), LastActivityAt: domain.Known(nil)}}
	e := testEnv(c)

	planPath := writeTestPlan(t, []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeUser, ResourceID: "10", ResourceName: "dormant_user", Action: domain.ActionBlockUser, EvaluatedAt: time.Now()},
	})

	// 1. Non-interactive without --confirm-instance-wide-user-block flag -> fails
	_, _, err := runCmd(newExecuteCmd(e), []string{planPath, "--apply", "--non-interactive", "--confirm-scope", "company"})
	if err == nil {
		t.Fatal("expected user block in non-interactive mode without flag to fail")
	}
	if !strings.Contains(err.Error(), "--confirm-instance-wide-user-block") {
		t.Errorf("expected error message mentioning --confirm-instance-wide-user-block, got %v", err)
	}

	// 2. Non-interactive with --confirm-instance-wide-user-block flag -> succeeds
	_, _, err = runCmd(newExecuteCmd(e), []string{planPath, "--apply", "--non-interactive", "--confirm-scope", "company", "--confirm-instance-wide-user-block"})
	if err != nil {
		t.Fatalf("expected user block with --confirm-instance-wide-user-block to succeed, got %v", err)
	}
	if !c.blockedUsers["10"] {
		t.Error("expected user 10 to be blocked")
	}
}
