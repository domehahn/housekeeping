package user

import (
	"context"

	"github.com/domehahn/housekeeping/internal/domain"
)

// LastActivityPolicy matches users whose last relevant platform activity is
// older than ThresholdDays. See LastLoginPolicy for the shared boundary
// semantics and unknown-data handling.
type LastActivityPolicy struct {
	ThresholdDays int
	Clock         domain.Clock
	OnUnknown     UnknownBehavior
}

func (p LastActivityPolicy) Name() string { return "last-activity" }

func (p LastActivityPolicy) Evaluate(_ context.Context, u domain.User) domain.Evaluation {
	return evaluateTimestamp("last activity", u.LastActivityAt, p.ThresholdDays, p.Clock, p.OnUnknown)
}
