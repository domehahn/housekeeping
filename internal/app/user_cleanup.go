package app

import (
	"context"
	"fmt"

	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/provider"
)

// UserDiscoverer is the subset of the provider needed to enumerate users
// within a scope.
type UserDiscoverer interface {
	provider.ScopeResolver
	provider.GroupMemberReader
}

// DiscoverUsers lists every member within an already-resolved scope.
func DiscoverUsers(ctx context.Context, reader provider.GroupMemberReader, scope domain.Scope) ([]domain.User, error) {
	users, err := reader.ListGroupMembers(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("list group members in scope %q: %w", scope.Path, err)
	}
	return users, nil
}

// EvaluateUsers runs the given policy plus protection rule against each
// discovered user, and separately flags users whose underlying activity
// data is unknown so callers can report on data-quality gaps regardless of
// how the unknown_activity policy setting resolved the match itself.
func EvaluateUsers(
	ctx context.Context,
	users []domain.User,
	policy domain.UserPolicy,
	protection domain.UserProtectionRule,
) UserEvaluationSummary {
	results := make([]UserEvaluation, 0, len(users))
	for _, u := range users {
		eval := policy.Evaluate(ctx, u)

		protected, reason := false, ""
		if protection != nil {
			protected, reason = protection.IsProtected(u)
		}

		results = append(results, UserEvaluation{
			User:             u,
			Evaluation:       eval,
			Protected:        protected,
			ProtectionReason: reason,
			UnknownActivity:  !u.LastLoginAt.IsKnown() || !u.LastActivityAt.IsKnown(),
		})
	}
	return UserEvaluationSummary{Results: results}
}
