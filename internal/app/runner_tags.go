package app

import (
	"context"
	"fmt"

	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/provider"
)

// RunnerTagEvaluation pairs a discovered runner with the outcome of
// checking it for a desired CI tag.
type RunnerTagEvaluation struct {
	Runner  domain.Runner
	Missing bool // true if the runner does not already have the tag
	Reasons []string
}

// Matched reports whether this runner should be included in a plan: the
// tag is missing. There is no protection concept for runners (unlike
// projects/users) - the safety control here is the out-of-scope-impact
// confirmation guard, not a protection list.
func (e RunnerTagEvaluation) Matched() bool { return e.Missing }

// OutOfScopeProjectCount is a convenience accessor mirroring the field
// planner.go copies into the resulting PlannedAction.
func (e RunnerTagEvaluation) OutOfScopeProjectCount() int {
	return len(e.Runner.OutOfScopeProjectPaths)
}

// RunnerTagEvaluationSummary is the full result of evaluating every
// runner used by the discovered projects against a desired CI tag.
type RunnerTagEvaluationSummary struct {
	Tag     string
	Results []RunnerTagEvaluation
}

func (s RunnerTagEvaluationSummary) Discovered() int { return len(s.Results) }

func (s RunnerTagEvaluationSummary) Matched() []RunnerTagEvaluation {
	var out []RunnerTagEvaluation
	for _, r := range s.Results {
		if r.Matched() {
			out = append(out, r)
		}
	}
	return out
}

// TotalOutOfScopeImpact sums the out-of-scope project count across every
// matched runner - the number this evaluation's plan would eventually
// require confirming via --confirm-out-of-scope-impact.
func (s RunnerTagEvaluationSummary) TotalOutOfScopeImpact() int {
	total := 0
	for _, r := range s.Matched() {
		total += r.OutOfScopeProjectCount()
	}
	return total
}

// EvaluateRunnerTags lists every runner used by projectIDs and checks
// each for tag. Blast radius (Runner.OutOfScopeProjectPaths) is computed
// by the provider (see provider.RunnerScanner) from the runner's full
// project list, not just the ones passed in here.
func EvaluateRunnerTags(ctx context.Context, scanner provider.RunnerScanner, projectIDs []string, tag string) (RunnerTagEvaluationSummary, error) {
	runners, err := scanner.ListRunnersForProjects(ctx, projectIDs)
	if err != nil {
		return RunnerTagEvaluationSummary{}, fmt.Errorf("list runners for scope: %w", err)
	}

	results := make([]RunnerTagEvaluation, 0, len(runners))
	for _, r := range runners {
		eval := RunnerTagEvaluation{Runner: r}
		if r.HasTag(tag) {
			eval.Reasons = []string{fmt.Sprintf("runner already has tag %q", tag)}
		} else {
			eval.Missing = true
			eval.Reasons = []string{fmt.Sprintf("tag %q missing from runner's tag list", tag)}
			if r.Shared {
				eval.Reasons = append(eval.Reasons, "runner is shared")
			}
			if n := len(r.OutOfScopeProjectPaths); n > 0 {
				eval.Reasons = append(eval.Reasons, fmt.Sprintf("used by %d project(s) outside the evaluated scope", n))
			}
		}
		results = append(results, eval)
	}

	return RunnerTagEvaluationSummary{Tag: tag, Results: results}, nil
}
