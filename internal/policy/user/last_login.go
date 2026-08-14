package user

import (
	"context"
	"fmt"

	"github.com/domehahn/housekeeping/internal/domain"
)

// LastLoginPolicy matches users whose last authentication is older than
// ThresholdDays. A user is considered matched when the age is strictly
// greater than the threshold: exactly ThresholdDays ago does not match.
// This keeps the semantics identical to LastActivityPolicy and avoids an
// off-by-one trap at the boundary.
type LastLoginPolicy struct {
	ThresholdDays int
	Clock         domain.Clock
	OnUnknown     UnknownBehavior
}

func (p LastLoginPolicy) Name() string { return "last-login" }

func (p LastLoginPolicy) Evaluate(_ context.Context, u domain.User) domain.Evaluation {
	return evaluateTimestamp("last login", u.LastLoginAt, p.ThresholdDays, p.Clock, p.OnUnknown)
}

// evaluateTimestamp implements the shared "older than N days" comparison
// used by both LastLoginPolicy and LastActivityPolicy.
func evaluateTimestamp(label string, ts domain.Timestamp, thresholdDays int, clock domain.Clock, onUnknown UnknownBehavior) domain.Evaluation {
	now := clock.Now()

	if !ts.IsKnown() {
		switch onUnknown {
		case UnknownMatch:
			return domain.Evaluation{
				Match:   true,
				Reasons: []string{fmt.Sprintf("%s: unknown (treated as match per unknown_activity=match)", label)},
			}
		case UnknownWarn:
			return domain.Evaluation{
				Match:   false,
				Reasons: []string{fmt.Sprintf("%s: unknown - WARNING: cannot evaluate, skipped (unknown_activity=warn)", label)},
			}
		default: // UnknownSkip
			return domain.Evaluation{
				Match:   false,
				Reasons: []string{fmt.Sprintf("%s: unknown - skipped (unknown_activity=skip)", label)},
			}
		}
	}

	if ts.At == nil {
		// Known to have never happened - definitely older than any threshold.
		return domain.Evaluation{
			Match:   true,
			Reasons: []string{fmt.Sprintf("%s: never recorded (known)", label)},
		}
	}

	days, _ := ts.DaysAgo(now)
	if days > thresholdDays {
		return domain.Evaluation{
			Match:   true,
			Reasons: []string{fmt.Sprintf("%s: %d days ago > threshold %d days", label, days, thresholdDays)},
		}
	}
	return domain.Evaluation{
		Match:   false,
		Reasons: []string{fmt.Sprintf("%s: %d days ago <= threshold %d days", label, days, thresholdDays)},
	}
}
