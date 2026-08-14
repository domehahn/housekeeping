package domain

import "context"

// Reason is a single, human-readable justification contributing to a
// policy Evaluation. Kept as a plain string (rather than a structured
// type) because reasons exist purely for transparency/audit output, not
// for further programmatic branching.
type Reason = string

// Evaluation is the result of running a single policy against a single
// resource. It never contains a decision about *what to do* - only whether
// the resource matched the policy's criteria and why.
type Evaluation struct {
	Match   bool
	Reasons []Reason
}

// Merge combines this evaluation with another, keeping all reasons and
// requiring both to match for the combined result to match. Used to fold
// unknown-data and protection checks in with policy results.
func (e Evaluation) and(other Evaluation) Evaluation {
	return Evaluation{
		Match:   e.Match && other.Match,
		Reasons: append(append([]Reason{}, e.Reasons...), other.Reasons...),
	}
}

func (e Evaluation) or(other Evaluation) Evaluation {
	return Evaluation{
		Match:   e.Match || other.Match,
		Reasons: append(append([]Reason{}, e.Reasons...), other.Reasons...),
	}
}

// And combines evaluations requiring all to match ("match: all" semantics).
func And(evals ...Evaluation) Evaluation {
	if len(evals) == 0 {
		return Evaluation{Match: false}
	}
	result := evals[0]
	for _, e := range evals[1:] {
		result = result.and(e)
	}
	return result
}

// Or combines evaluations requiring any to match ("match: any" semantics).
func Or(evals ...Evaluation) Evaluation {
	if len(evals) == 0 {
		return Evaluation{Match: false}
	}
	result := evals[0]
	for _, e := range evals[1:] {
		result = result.or(e)
	}
	return result
}

// ProjectPolicy decides whether a Project matches a cleanup criterion.
// Implementations must be pure functions of their inputs (plus the
// injected Clock) - no network access, no hidden state.
type ProjectPolicy interface {
	// Name identifies the policy for reasons/audit output.
	Name() string
	Evaluate(ctx context.Context, project Project) Evaluation
}

// UserPolicy decides whether a User matches a cleanup criterion.
type UserPolicy interface {
	Name() string
	Evaluate(ctx context.Context, user User) Evaluation
}

// ProtectionRule decides whether a resource must never be matched by any
// policy, regardless of other criteria. Protection always wins.
type ProjectProtectionRule interface {
	IsProtected(project Project) (bool, Reason)
}

type UserProtectionRule interface {
	IsProtected(user User) (bool, Reason)
}
