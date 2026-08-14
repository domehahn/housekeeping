// Package project contains provider-independent policies that decide
// whether a domain.Project matches a cleanup criterion.
package project

import (
	"context"
	"fmt"

	"github.com/domehahn/housekeeping/internal/domain"
)

// InactivePolicy matches projects whose last recorded activity is older
// than ThresholdDays. A nil-but-known LastActivityAt (project created but
// never touched) matches unconditionally. An unknown LastActivityAt never
// matches - see docs/adr/0003-unknown-activity-safe-default.md, which
// applies equally to projects.
type InactivePolicy struct {
	ThresholdDays int
	Clock         domain.Clock
}

func (p InactivePolicy) Name() string { return "inactive-project" }

func (p InactivePolicy) Evaluate(_ context.Context, proj domain.Project) domain.Evaluation {
	ts := proj.LastActivityAt
	if !ts.IsKnown() {
		return domain.Evaluation{
			Match:   false,
			Reasons: []string{"last activity: unknown - skipped (missing data is never treated as inactive)"},
		}
	}
	if ts.At == nil {
		return domain.Evaluation{
			Match:   true,
			Reasons: []string{"last activity: never recorded (known)"},
		}
	}
	days, _ := ts.DaysAgo(p.Clock.Now())
	if days > p.ThresholdDays {
		return domain.Evaluation{
			Match:   true,
			Reasons: []string{fmt.Sprintf("last activity: %d days ago > threshold %d days", days, p.ThresholdDays)},
		}
	}
	return domain.Evaluation{
		Match:   false,
		Reasons: []string{fmt.Sprintf("last activity: %d days ago <= threshold %d days", days, p.ThresholdDays)},
	}
}
