package app

import (
	"context"
	"testing"

	"github.com/domehahn/housekeeping/internal/domain"
)

type fakeRunnerScanner struct {
	runners []domain.Runner
	err     error
}

func (f *fakeRunnerScanner) ListRunnersForProjects(context.Context, domain.Scope, []string) ([]domain.Runner, error) {
	return f.runners, f.err
}

func (f *fakeRunnerScanner) GetRunnerForProjects(context.Context, string, domain.Scope, []string) (domain.Runner, error) {
	return domain.Runner{}, nil
}

func TestEvaluateRunnerTags_MatchesMissingTag(t *testing.T) {
	scanner := &fakeRunnerScanner{runners: []domain.Runner{
		{ID: "1", TagList: []string{"other-tag"}, ImpactKnown: true},
		{ID: "2", TagList: []string{"k8s-runner"}, ImpactKnown: true},
	}}

	summary, err := EvaluateRunnerTags(context.Background(), scanner, domain.Scope{}, []string{"p1"}, "k8s-runner")
	if err != nil {
		t.Fatalf("EvaluateRunnerTags: %v", err)
	}
	matched := summary.Matched()
	if len(matched) != 1 || matched[0].Runner.ID != "1" {
		t.Errorf("expected only runner 1 to match, got %+v", matched)
	}
}

func TestEvaluateRunnerTags_OutOfScopeImpactSummed(t *testing.T) {
	scanner := &fakeRunnerScanner{runners: []domain.Runner{
		{ID: "1", Shared: true, ImpactKnown: true, OutOfScopeProjectPaths: []string{"a/x", "a/y"}},
		{ID: "2", Shared: true, ImpactKnown: true, OutOfScopeProjectPaths: []string{"b/z"}},
	}}

	summary, err := EvaluateRunnerTags(context.Background(), scanner, domain.Scope{}, []string{"p1"}, "k8s-runner")
	if err != nil {
		t.Fatalf("EvaluateRunnerTags: %v", err)
	}
	if got := summary.TotalOutOfScopeImpact(); got != 3 {
		t.Errorf("expected total out-of-scope impact 3, got %d", got)
	}
}

func TestEvaluateRunnerTags_NoOutOfScopeImpactForDedicatedRunner(t *testing.T) {
	scanner := &fakeRunnerScanner{runners: []domain.Runner{
		{ID: "1", Shared: false, ImpactKnown: true, InScopeProjectPaths: []string{"a/x"}},
	}}

	summary, err := EvaluateRunnerTags(context.Background(), scanner, domain.Scope{}, []string{"p1"}, "k8s-runner")
	if err != nil {
		t.Fatalf("EvaluateRunnerTags: %v", err)
	}
	if got := summary.TotalOutOfScopeImpact(); got != 0 {
		t.Errorf("expected 0 out-of-scope impact for a dedicated runner, got %d", got)
	}
}

func TestEvaluateRunnerTags_PropagatesScannerError(t *testing.T) {
	scanner := &fakeRunnerScanner{err: errFakeFetch{}}
	_, err := EvaluateRunnerTags(context.Background(), scanner, domain.Scope{}, []string{"p1"}, "k8s-runner")
	if err == nil {
		t.Fatal("expected an error to propagate")
	}
}

func TestEvaluateRunnerTags_BlocksRunnerWithUnprovableImpact(t *testing.T) {
	scanner := &fakeRunnerScanner{runners: []domain.Runner{{
		ID: "1", RunnerType: "instance_type", ImpactReason: "instance-wide impact",
	}}}
	summary, err := EvaluateRunnerTags(context.Background(), scanner, domain.Scope{}, []string{"p1"}, "k8s-runner")
	if err != nil {
		t.Fatalf("EvaluateRunnerTags: %v", err)
	}
	if len(summary.Results) != 1 || !summary.Results[0].Blocked || len(summary.Matched()) != 0 {
		t.Fatalf("expected the unsafe runner to be visible but not actionable, got %+v", summary)
	}
}
