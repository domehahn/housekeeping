package app

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/domehahn/housekeeping/internal/domain"
)

func TestNormalizeTagsDeterministic(t *testing.T) {
	got, err := NormalizeTags([]string{" production ", "AKS", "AKS"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"AKS", "production"}) {
		t.Fatalf("NormalizeTags() = %v", got)
	}
}

func TestFilterPipelineProjectsUsesFullPathAndExcludeWins(t *testing.T) {
	projects := []domain.Project{
		{ID: "1", FullPath: "company/backend/api"},
		{ID: "2", FullPath: "company/backend/legacy"},
		{ID: "3", FullPath: "company/frontend/web"},
	}
	got, err := FilterPipelineProjects(projects, []string{`^company/backend/`}, []string{`legacy$`})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("FilterPipelineProjects() = %+v", got)
	}
}

func TestBatchPlanSortsAndSplitsDeterministically(t *testing.T) {
	plan := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceID: "2", ResourceName: "company/z"},
		{ResourceID: "1", ResourceName: "company/a"},
		{ResourceID: "3", ResourceName: "company/m"},
	}}
	batches, err := BatchPlan(plan, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 || len(batches[0].Actions) != 2 || batches[0].Actions[0].ResourceID != "1" || batches[1].Actions[0].ResourceID != "2" {
		t.Fatalf("BatchPlan() = %+v", batches)
	}
}

type fakeMergedAnalyzer struct {
	content  []byte
	includes []domain.PipelineInclude
	err      error
}

func (f fakeMergedAnalyzer) GetMergedPipelineConfig(context.Context, string) ([]byte, []domain.PipelineInclude, error) {
	return f.content, f.includes, f.err
}

func TestAnalyzeMergedPipelineTagsFindsIncludedJobOverride(t *testing.T) {
	analyzer := fakeMergedAnalyzer{
		content:  []byte("default:\n  tags: [AKS]\nincluded-build:\n  tags: [docker]\n"),
		includes: []domain.PipelineInclude{{Type: "local", Location: "build.yml"}},
	}
	results := AnalyzeMergedPipelineTags(context.Background(), analyzer, []domain.Project{{ID: "1"}}, []string{"AKS"}, 1)
	if len(results) != 1 || results[0].Status != PipelineTagMissing || len(results[0].Includes) != 1 {
		t.Fatalf("AnalyzeMergedPipelineTags() = %+v", results)
	}
}

type fakeProposalReporter struct {
	proposals []domain.PipelineProposal
	err       error
}

func (f fakeProposalReporter) ListPipelineTagProposals(context.Context, string, []string) ([]domain.PipelineProposal, error) {
	return f.proposals, f.err
}

func TestDiscoverPipelineProposalStatuses(t *testing.T) {
	projects := []domain.Project{{ID: "1", FullPath: "company/a"}}
	statuses := DiscoverPipelineProposalStatuses(context.Background(), fakeProposalReporter{
		proposals: []domain.PipelineProposal{{State: "merged", URL: "https://example/mr/1"}},
	}, projects, []string{"AKS"}, 1)
	if len(statuses) != 1 || statuses[0].Proposal == nil || statuses[0].Proposal.ProjectPath != "company/a" {
		t.Fatalf("statuses = %+v", statuses)
	}

	errorsResult := DiscoverPipelineProposalStatuses(context.Background(), fakeProposalReporter{err: errors.New("boom")}, projects, []string{"AKS"}, 1)
	if errorsResult[0].Error == "" {
		t.Fatal("expected reporting error")
	}
}
