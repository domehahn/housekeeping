package app

import (
	"context"
	"fmt"

	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/policy/user"
	"github.com/domehahn/housekeeping/internal/provider"
)

// BillableOverrideReader is the subset of the provider needed to apply the
// billable-seat override.
type BillableOverrideReader interface {
	provider.BillableMembersReader
	provider.UserMembershipReader
}

// BillableOverrideResult is the outcome of attempting to apply the
// billable-seat override to a UserEvaluationSummary.
type BillableOverrideResult struct {
	Summary UserEvaluationSummary
	// Warnings are non-fatal problems encountered while applying the
	// override for a specific user (e.g. their cross-instance memberships
	// could not be fetched). The override is simply skipped for that user
	// - a data gap never causes a user to be treated as eligible for
	// removal - but the gap is reported here so it is never silently
	// swallowed.
	Warnings []string
}

// ApplyBillableSeatOverride re-evaluates users who did not match the
// standard inactivity policy solely because of recent global activity: if
// they are billable in the target group but hold no privileged membership
// anywhere else on the instance, their protection is overridden. See
// policy/user.BillableSeatOverride for the full rationale and
// docs/adr/0004-billable-seat-override.md for the design tradeoffs.
//
// Fails safe as a whole: if the target group's billable-member list
// itself cannot be retrieved (e.g. the token lacks Owner rights on that
// top-level group), the override is not applied to anyone and the error is
// returned - callers must not fall back to treating that as "nobody is
// billable" or any other default that could widen who gets matched.
// Already-matched or already-protected users are never touched - the
// override only ever adds matches, and protection always wins regardless
// of billing.
func ApplyBillableSeatOverride(
	ctx context.Context,
	reader BillableOverrideReader,
	scope domain.Scope,
	summary UserEvaluationSummary,
	override user.BillableSeatOverride,
) (BillableOverrideResult, error) {
	result := BillableOverrideResult{Summary: summary}
	if !override.Enabled {
		return result, nil
	}

	billable, err := reader.ListBillableGroupMembers(ctx, scope.ID)
	if err != nil {
		return BillableOverrideResult{}, fmt.Errorf("list billable members of %q: %w", scope.Path, err)
	}

	for i := range result.Summary.Results {
		r := &result.Summary.Results[i]

		if r.Protected || r.Evaluation.Match {
			continue // protection always wins; already a match, nothing to add
		}
		if !billable[r.User.ID] {
			continue // not billable in the target group, override does not apply
		}

		memberships, err := reader.ListUserMemberships(ctx, r.User.ID)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"user %s: could not determine cross-instance memberships, override skipped: %v", r.User.Username, err))
			continue
		}

		overrideEval := override.Evaluate(scope.ID, scope.Path, true, memberships)
		if overrideEval.Match {
			r.Evaluation = domain.Evaluation{
				Match:   true,
				Reasons: append(append([]string{}, r.Evaluation.Reasons...), overrideEval.Reasons...),
			}
		}
	}

	return result, nil
}
