package user

import (
	"context"
	"fmt"

	"github.com/domehahn/housekeeping/internal/domain"
)

// MatchMode controls how LastLoginPolicy and LastActivityPolicy are
// combined by InactiveUserPolicy.
type MatchMode string

const (
	MatchAll MatchMode = "all" // both must match
	MatchAny MatchMode = "any" // either matches
)

// ParseMatchMode validates a config string, defaulting to MatchAll.
func ParseMatchMode(s string) (MatchMode, error) {
	switch MatchMode(s) {
	case "", MatchAll:
		return MatchAll, nil
	case MatchAny:
		return MatchAny, nil
	default:
		return "", fmt.Errorf("policy: match must be one of all|any, got %q", s)
	}
}

// InactiveUserPolicy combines last-login and last-activity into a single
// composite policy per the configured MatchMode. It is the policy
// referenced by the `users.inactive` configuration block and by
// `users evaluate --inactive-for`.
//
// Both underlying timestamps are instance-wide, not scoped to whichever
// group is currently being evaluated (GitLab does not expose a
// per-group activity signal). This means a user who is inactive in the
// group being cleaned up, but who has recently signed in or been active
// anywhere else on the same instance (e.g. a different top-level group),
// will not match here - their global activity protects them from being
// removed from an unrelated group, automatically.
type InactiveUserPolicy struct {
	LastLogin    LastLoginPolicy
	LastActivity LastActivityPolicy
	Mode         MatchMode
}

func (p InactiveUserPolicy) Name() string { return "inactive-user" }

func (p InactiveUserPolicy) Evaluate(ctx context.Context, u domain.User) domain.Evaluation {
	loginEval := p.LastLogin.Evaluate(ctx, u)
	activityEval := p.LastActivity.Evaluate(ctx, u)

	switch p.Mode {
	case MatchAny:
		return domain.Or(loginEval, activityEval)
	default:
		return domain.And(loginEval, activityEval)
	}
}
