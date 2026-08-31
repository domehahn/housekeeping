package app

import (
	"context"
	"strings"
	"testing"

	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/policy/project"
)

type fakePipelineConfigReader struct {
	files     map[string][]byte // projectID -> content; absent = no CI file
	errs      map[string]error
	proposals map[string][]domain.PipelineProposal // projectID -> open scm-cleaner proposals, for rename tests
}

func (f *fakePipelineConfigReader) GetPipelineConfig(_ context.Context, projectID string) ([]byte, bool, error) {
	if err, ok := f.errs[projectID]; ok {
		return nil, false, err
	}
	content, ok := f.files[projectID]
	return content, ok, nil
}

func (f *fakePipelineConfigReader) ProposePipelineTagChange(context.Context, string, []byte, []string) (string, error) {
	return "", nil
}

func (f *fakePipelineConfigReader) ProposePipelineTagRename(context.Context, string, []byte, []domain.TagRename) (string, []string, error) {
	return "", nil, nil
}

func (f *fakePipelineConfigReader) ClosePipelineTagProposals(context.Context, string, []string) ([]string, error) {
	return nil, nil
}

func (f *fakePipelineConfigReader) ListPipelineTagProposals(_ context.Context, projectID string, _ []string) ([]domain.PipelineProposal, error) {
	return f.proposals[projectID], nil
}

func TestEvaluatePipelineTags_ClassifiesEveryStatus(t *testing.T) {
	reader := &fakePipelineConfigReader{
		files: map[string][]byte{
			"1": []byte("build-job:\n  script: [\"x\"]\n"),                                        // missing
			"2": []byte("default:\n  tags:\n    - k8s-runner\n\nbuild-job:\n  script: [\"x\"]\n"), // present
			"4": []byte("not: valid: [yaml"),                                                      // parse error
		},
		errs: map[string]error{"5": errFakeFetch{}},
	}
	projects := []domain.Project{
		{ID: "1", FullPath: "g/a"},
		{ID: "2", FullPath: "g/b"},
		{ID: "3", FullPath: "g/c"}, // no entry in files -> no CI file
		{ID: "4", FullPath: "g/d"},
		{ID: "5", FullPath: "g/e"},
	}

	summary := EvaluatePipelineTags(context.Background(), reader, projects, []string{"k8s-runner"}, nil, 2)

	statuses := map[string]PipelineTagStatus{}
	for _, r := range summary.Results {
		statuses[r.Project.ID] = r.Status
	}
	want := map[string]PipelineTagStatus{
		"1": PipelineTagMissing,
		"2": PipelineTagPresent,
		"3": PipelineTagNoCIFile,
		"4": PipelineTagParseError,
		"5": PipelineTagFetchError,
	}
	for id, wantStatus := range want {
		if statuses[id] != wantStatus {
			t.Errorf("project %s: got status %v, want %v", id, statuses[id], wantStatus)
		}
	}

	matched := summary.Matched()
	if len(matched) != 1 || matched[0].Project.ID != "1" {
		t.Errorf("expected only project 1 to match, got %+v", matched)
	}
}

func TestEvaluatePipelineTags_ProtectionExcludesFromMatch(t *testing.T) {
	reader := &fakePipelineConfigReader{
		files: map[string][]byte{"1": []byte("build-job:\n  script: [\"x\"]\n")},
	}
	protection, err := project.NewProtection([]string{"g/a"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	summary := EvaluatePipelineTags(context.Background(), reader, []domain.Project{{ID: "1", FullPath: "g/a"}}, []string{"k8s-runner"}, protection, 1)

	if len(summary.Matched()) != 0 {
		t.Error("expected the protected project not to match, even though the tag is missing")
	}
	if len(summary.Protected()) != 1 {
		t.Error("expected the project to be reported as protected")
	}
}

func TestEvaluatePipelineTags_ReportsIncludesWarning(t *testing.T) {
	reader := &fakePipelineConfigReader{
		files: map[string][]byte{"1": []byte("include:\n  - local: other.yml\nbuild-job:\n  script: [\"x\"]\n")},
	}
	summary := EvaluatePipelineTags(context.Background(), reader, []domain.Project{{ID: "1", FullPath: "g/a"}}, []string{"k8s-runner"}, nil, 1)

	if !summary.Results[0].HasIncludes {
		t.Error("expected HasIncludes to be true")
	}
	hasWarning := false
	for _, r := range summary.Results[0].Reasons {
		if strings.HasPrefix(r, "warning") {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Errorf("expected a warning reason about include:, got %v", summary.Results[0].Reasons)
	}
}

type errFakeFetch struct{}

func (errFakeFetch) Error() string { return "boom" }

func TestEvaluatePipelineTagRename_MatchesFileNeedingReplace(t *testing.T) {
	reader := &fakePipelineConfigReader{
		files: map[string][]byte{"1": []byte("default:\n  tags:\n    - AKS\n")},
	}
	projects := []domain.Project{{ID: "1", FullPath: "g/a"}}
	summary := EvaluatePipelineTagRename(context.Background(), reader, projects, []domain.TagRename{{Old: "AKS", New: "aks"}}, nil, 1)

	if len(summary.Matched()) != 1 {
		t.Fatalf("expected the project with the old tag in its file to be matched, got %+v", summary.Results)
	}
}

func TestEvaluatePipelineTagRename_MatchesFileNeverGotCorrectedTag(t *testing.T) {
	// The old add-tag Merge Request for this project was never merged:
	// the file has neither the old nor the new tag at all.
	reader := &fakePipelineConfigReader{
		files: map[string][]byte{"1": []byte("build-job:\n  script: [\"x\"]\n")},
	}
	projects := []domain.Project{{ID: "1", FullPath: "g/a"}}
	summary := EvaluatePipelineTagRename(context.Background(), reader, projects, []domain.TagRename{{Old: "AKS", New: "aks"}}, nil, 1)

	if len(summary.Matched()) != 1 {
		t.Fatalf("expected the project missing the corrected tag entirely to be matched, got %+v", summary.Results)
	}
}

func TestEvaluatePipelineTagRename_MatchesFileCorrectButStaleOpenProposal(t *testing.T) {
	// The file already has the corrected tag (or never needed it merged),
	// but a stale, still-open Merge Request from an earlier, wrong-tag
	// run still proposes the old tag - this is the exact case reported:
	// evaluate/plan must still flag it so execute closes it.
	reader := &fakePipelineConfigReader{
		files: map[string][]byte{"1": []byte("default:\n  tags:\n    - aks\n")},
		proposals: map[string][]domain.PipelineProposal{
			"1": {{ProjectID: "1", IID: 7, State: "opened", URL: "https://example/mr/7"}},
		},
	}
	projects := []domain.Project{{ID: "1", FullPath: "g/a"}}
	summary := EvaluatePipelineTagRename(context.Background(), reader, projects, []domain.TagRename{{Old: "AKS", New: "aks"}}, nil, 1)

	if len(summary.Matched()) != 1 {
		t.Fatalf("expected the project with only a stale open proposal to be matched, got %+v", summary.Results)
	}
	if !strings.Contains(summary.Matched()[0].Reasons[0], "mr/7") {
		t.Errorf("expected the stale proposal's URL in the reasons, got %v", summary.Matched()[0].Reasons)
	}
}

func TestEvaluatePipelineTagRename_NotMatchedWhenFullyCorrectAndNoStaleProposal(t *testing.T) {
	reader := &fakePipelineConfigReader{
		files: map[string][]byte{"1": []byte("default:\n  tags:\n    - aks\n")},
	}
	projects := []domain.Project{{ID: "1", FullPath: "g/a"}}
	summary := EvaluatePipelineTagRename(context.Background(), reader, projects, []domain.TagRename{{Old: "AKS", New: "aks"}}, nil, 1)

	if len(summary.Matched()) != 0 {
		t.Fatalf("expected no match when the file is correct and no stale proposal exists, got %+v", summary.Results)
	}
}

func TestEvaluatePipelineTagRename_IgnoresMergedOrClosedProposal(t *testing.T) {
	reader := &fakePipelineConfigReader{
		files: map[string][]byte{"1": []byte("default:\n  tags:\n    - aks\n")},
		proposals: map[string][]domain.PipelineProposal{
			"1": {{ProjectID: "1", IID: 7, State: "merged", URL: "https://example/mr/7"}},
		},
	}
	projects := []domain.Project{{ID: "1", FullPath: "g/a"}}
	summary := EvaluatePipelineTagRename(context.Background(), reader, projects, []domain.TagRename{{Old: "AKS", New: "aks"}}, nil, 1)

	if len(summary.Matched()) != 0 {
		t.Fatalf("expected a merged proposal to not count as stale, got %+v", summary.Results)
	}
}
