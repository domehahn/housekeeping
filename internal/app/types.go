// Package app contains the use cases (application services) that
// orchestrate domain policies against provider ports. It knows about
// neither GitLab nor any other concrete provider - only the small
// interfaces declared in internal/provider.
package app

import "github.com/domehahn/housekeeping/internal/domain"

// ProjectEvaluation pairs a discovered project with the outcome of running
// policies and protection rules against it.
type ProjectEvaluation struct {
	Project          domain.Project
	Evaluation       domain.Evaluation
	Protected        bool
	ProtectionReason string
}

// Matched reports whether this project should be included in a plan: it
// satisfied the policy and was not protected.
func (e ProjectEvaluation) Matched() bool { return e.Evaluation.Match && !e.Protected }

// ProjectEvaluationSummary is the full result of evaluating every project
// discovered in a scope.
type ProjectEvaluationSummary struct {
	Scope   domain.Scope
	Results []ProjectEvaluation
}

func (s ProjectEvaluationSummary) Discovered() int { return len(s.Results) }

func (s ProjectEvaluationSummary) Matched() []ProjectEvaluation {
	var out []ProjectEvaluation
	for _, r := range s.Results {
		if r.Matched() {
			out = append(out, r)
		}
	}
	return out
}

func (s ProjectEvaluationSummary) Protected() []ProjectEvaluation {
	var out []ProjectEvaluation
	for _, r := range s.Results {
		if r.Protected {
			out = append(out, r)
		}
	}
	return out
}

// UserEvaluation pairs a discovered user with the outcome of running
// policies and protection rules against it.
type UserEvaluation struct {
	User             domain.User
	Evaluation       domain.Evaluation
	Protected        bool
	ProtectionReason string
	// UnknownActivity is true when either LastLoginAt or LastActivityAt
	// could not be determined by the provider. Kept separate from Matched
	// so callers can report "N users with unknown activity" regardless of
	// how the unknown_activity policy setting resolved the match.
	UnknownActivity bool
}

func (e UserEvaluation) Matched() bool { return e.Evaluation.Match && !e.Protected }

// UserEvaluationSummary is the full result of evaluating every user
// discovered in a scope.
type UserEvaluationSummary struct {
	Scope   domain.Scope
	Results []UserEvaluation
}

func (s UserEvaluationSummary) Discovered() int { return len(s.Results) }

func (s UserEvaluationSummary) Matched() []UserEvaluation {
	var out []UserEvaluation
	for _, r := range s.Results {
		if r.Matched() {
			out = append(out, r)
		}
	}
	return out
}

func (s UserEvaluationSummary) Protected() []UserEvaluation {
	var out []UserEvaluation
	for _, r := range s.Results {
		if r.Protected {
			out = append(out, r)
		}
	}
	return out
}

func (s UserEvaluationSummary) Unknown() []UserEvaluation {
	var out []UserEvaluation
	for _, r := range s.Results {
		if r.UnknownActivity {
			out = append(out, r)
		}
	}
	return out
}
