package app

import (
	"context"
	"strings"
	"testing"

	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/policy/project"
)

type fakePipelineConfigReader struct {
	files map[string][]byte // projectID -> content; absent = no CI file
	errs  map[string]error
}

func (f *fakePipelineConfigReader) GetPipelineConfig(_ context.Context, projectID string) ([]byte, bool, error) {
	if err, ok := f.errs[projectID]; ok {
		return nil, false, err
	}
	content, ok := f.files[projectID]
	return content, ok, nil
}

func (f *fakePipelineConfigReader) ProposePipelineTagChange(context.Context, string, []byte, string) (string, error) {
	return "", nil
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

	summary := EvaluatePipelineTags(context.Background(), reader, projects, "k8s-runner", nil, 2)

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

	summary := EvaluatePipelineTags(context.Background(), reader, []domain.Project{{ID: "1", FullPath: "g/a"}}, "k8s-runner", protection, 1)

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
	summary := EvaluatePipelineTags(context.Background(), reader, []domain.Project{{ID: "1", FullPath: "g/a"}}, "k8s-runner", nil, 1)

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
