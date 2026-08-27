package cli

import (
	"strings"
	"testing"

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
