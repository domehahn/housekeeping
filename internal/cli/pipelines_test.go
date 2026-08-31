package cli

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/domehahn/housekeeping/internal/app"
	"github.com/domehahn/housekeeping/internal/domain"
)

func TestPipelinesEvaluate_RequiresTag(t *testing.T) {
	e := withGroup(testEnv(newFakeClient()), "company", false)
	_, _, err := runCmd(newPipelinesCmd(e), []string{"evaluate"})
	if err == nil {
		t.Fatal("expected an error when --tag is missing")
	}
	if got := exitCodeOf(err); got != ExitInvalidConfiguration {
		t.Errorf("exit code = %d, want %d", got, ExitInvalidConfiguration)
	}
}

func TestPipelinesPlan_MultipleTagsFiltersAndBatches(t *testing.T) {
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.projects = []domain.Project{
		{ID: "3", FullPath: "company/backend/z", DefaultBranch: "main"},
		{ID: "1", FullPath: "company/backend/a", DefaultBranch: "main"},
		{ID: "2", FullPath: "company/frontend/web", DefaultBranch: "main"},
	}
	for _, project := range c.projects {
		c.ciFiles[project.ID] = []byte("build:\n  script: [x]\n")
	}
	c.info = fakeInfo()
	e := withGroup(testEnv(c), "company", false)
	basePath := filepath.Join(t.TempDir(), "pipeline-tags.json")

	stdout, _, err := runCmd(newPipelinesCmd(e), []string{
		"plan", "--tag", "production", "--tag", "AKS",
		"--include-project", `^company/backend/`, "--batch-size", "1", "--output-plan", basePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	for index, wantID := range []string{"1", "3"} {
		path := filepath.Join(filepath.Dir(basePath), fmt.Sprintf("pipeline-tags-%03d.json", index+1))
		plan, err := app.LoadPlan(path)
		if err != nil {
			t.Fatalf("load batch %s: %v\n%s", path, err, stdout)
		}
		if len(plan.Actions) != 1 || plan.Actions[0].ResourceID != wantID || !slices.Equal(plan.Actions[0].Tags(), []string{"AKS", "production"}) {
			t.Fatalf("batch %d = %+v", index, plan.Actions)
		}
	}
}

func TestPipelinesPlan_BatchCannotExceedMaxActions(t *testing.T) {
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	for i := 1; i <= 11; i++ {
		id := fmt.Sprintf("%d", i)
		c.projects = append(c.projects, domain.Project{ID: id, FullPath: fmt.Sprintf("company/p%02d", i)})
		c.ciFiles[id] = []byte("build:\n  script: [x]\n")
	}
	c.info = fakeInfo()
	e := withGroup(testEnv(c), "company", false)
	_, stderr, err := runCmd(newPipelinesCmd(e), []string{
		"plan", "--tag", "AKS", "--batch-size", "11", "--output-plan", filepath.Join(t.TempDir(), "plan.json"),
	})
	if err == nil || !strings.Contains(stderr, "SAFETY GUARD") {
		t.Fatalf("stderr/error = %q / %v", stderr, err)
	}
}

func TestPipelinesAnalyzeUsesMergedConfig(t *testing.T) {
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a"}}
	c.mergedCI["1"] = []byte("default:\n  tags: [AKS]\nincluded:\n  tags: [docker]\n")
	c.ciIncludes["1"] = []domain.PipelineInclude{{Type: "local", Location: "jobs.yml"}}
	e := withGroup(testEnv(c), "company", false)

	stdout, _, err := runCmd(newPipelinesCmd(e), []string{"analyze", "--tag", "AKS"})
	if err != nil || !strings.Contains(stdout, "missing") || !strings.Contains(stdout, "company/a") {
		t.Fatalf("output/error = %q / %v", stdout, err)
	}
}

func TestPipelinesProposalStatus(t *testing.T) {
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a"}}
	c.proposals["1"] = []domain.PipelineProposal{{State: "merged", URL: "https://gitlab.example/mr/1"}}
	e := withGroup(testEnv(c), "company", false)

	stdout, _, err := runCmd(newPipelinesCmd(e), []string{"proposals", "status", "--tag", "AKS"})
	if err != nil || !strings.Contains(stdout, "merged") || !strings.Contains(stdout, "https://gitlab.example/mr/1") {
		t.Fatalf("output/error = %q / %v", stdout, err)
	}
}

func TestPipelinesEvaluate_MatchesMissingTag(t *testing.T) {
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a", DefaultBranch: "main"}}
	c.ciFiles["1"] = []byte("build-job:\n  script: [\"echo hi\"]\n")
	e := withGroup(testEnv(c), "company", false)

	stdout, _, err := runCmd(newPipelinesCmd(e), []string{"evaluate", "--tag", "k8s-runner"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "company/a") {
		t.Errorf("expected the project to be reported as matched, got:\n%s", stdout)
	}
}

func TestPipelinesList_ReportsCIFilePresence(t *testing.T) {
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.projects = []domain.Project{
		{ID: "1", FullPath: "company/a", DefaultBranch: "main"},
		{ID: "2", FullPath: "company/b", DefaultBranch: "main"},
	}
	c.ciFiles["1"] = []byte("build-job:\n  script: [\"echo hi\"]\n")
	e := withGroup(testEnv(c), "company", false)

	stdout, _, err := runCmd(newPipelinesCmd(e), []string{"list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "company/a") || !strings.Contains(stdout, "company/b") {
		t.Errorf("expected both projects listed, got:\n%s", stdout)
	}
}

func TestPipelinesEvaluate_TagAndReplaceTagAreMutuallyExclusive(t *testing.T) {
	e := withGroup(testEnv(newFakeClient()), "company", false)
	_, _, err := runCmd(newPipelinesCmd(e), []string{"evaluate", "--tag", "AKS", "--replace-tag", "AKS:aks"})
	if err == nil {
		t.Fatal("expected an error when --tag and --replace-tag are both set")
	}
	if got := exitCodeOf(err); got != ExitInvalidConfiguration {
		t.Errorf("exit code = %d, want %d", got, ExitInvalidConfiguration)
	}
}

func TestPipelinesEvaluate_RejectsMalformedReplaceTag(t *testing.T) {
	e := withGroup(testEnv(newFakeClient()), "company", false)
	_, _, err := runCmd(newPipelinesCmd(e), []string{"evaluate", "--replace-tag", "AKS"})
	if err == nil {
		t.Fatal("expected an error for a --replace-tag value without a colon")
	}
	if got := exitCodeOf(err); got != ExitInvalidConfiguration {
		t.Errorf("exit code = %d, want %d", got, ExitInvalidConfiguration)
	}
}

func TestPipelinesEvaluate_MatchesOldTagForReplace(t *testing.T) {
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a", DefaultBranch: "main"}}
	c.ciFiles["1"] = []byte("default:\n  tags:\n    - AKS\n")
	e := withGroup(testEnv(c), "company", false)

	stdout, _, err := runCmd(newPipelinesCmd(e), []string{"evaluate", "--replace-tag", "AKS:aks"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "company/a") {
		t.Errorf("expected the project with the old tag to be reported as matched, got:\n%s", stdout)
	}
}

func TestPipelinesPlan_WritesReplaceTagPlanFile(t *testing.T) {
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a", DefaultBranch: "main"}}
	c.ciFiles["1"] = []byte("default:\n  tags:\n    - AKS\n")
	c.info = fakeInfo()
	e := withGroup(testEnv(c), "company", false)

	planPath := t.TempDir() + "/rename-plan.json"
	stdout, _, err := runCmd(newPipelinesCmd(e), []string{"plan", "--replace-tag", "AKS:aks", "--output-plan", planPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "Plan written to") {
		t.Errorf("expected confirmation that the plan was written, got:\n%s", stdout)
	}
	plan, err := app.LoadPlan(planPath)
	if err != nil {
		t.Fatalf("load plan: %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Action != domain.ActionReplacePipelineTag ||
		!slices.Equal(plan.Actions[0].TagRenames, []domain.TagRename{{Old: "AKS", New: "aks"}}) {
		t.Fatalf("unexpected plan actions: %+v", plan.Actions)
	}
}

func TestPipelinesPlan_WritesPlanFile(t *testing.T) {
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.projects = []domain.Project{{ID: "1", FullPath: "company/a", DefaultBranch: "main"}}
	c.ciFiles["1"] = []byte("build-job:\n  script: [\"echo hi\"]\n")
	c.info = fakeInfo()
	e := withGroup(testEnv(c), "company", false)

	planPath := t.TempDir() + "/plan.json"
	stdout, _, err := runCmd(newPipelinesCmd(e), []string{"plan", "--tag", "k8s-runner", "--output-plan", planPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "Plan written to") {
		t.Errorf("expected confirmation that the plan was written, got:\n%s", stdout)
	}
}
