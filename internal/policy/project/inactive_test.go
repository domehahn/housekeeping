package project

import (
	"context"
	"testing"
	"time"

	"github.com/domehahn/housekeeping/internal/domain"
)

func TestInactivePolicy_Boundary(t *testing.T) {
	now := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	clock := domain.FixedClock{Instant: now}
	p := InactivePolicy{ThresholdDays: 90, Clock: clock}

	tests := []struct {
		name      string
		daysAgo   int
		wantMatch bool
	}{
		{"91 days ago: matches", 91, true},
		{"90 days ago: does not match (boundary exclusive)", 90, false},
		{"89 days ago: does not match", 89, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts := now.Add(-time.Duration(tc.daysAgo) * 24 * time.Hour)
			proj := domain.Project{LastActivityAt: domain.Known(&ts)}
			if got := p.Evaluate(context.Background(), proj).Match; got != tc.wantMatch {
				t.Errorf("Match = %v, want %v", got, tc.wantMatch)
			}
		})
	}
}

func TestInactivePolicy_UnknownActivityNeverMatches(t *testing.T) {
	clock := domain.FixedClock{Instant: time.Now()}
	p := InactivePolicy{ThresholdDays: 90, Clock: clock}
	proj := domain.Project{LastActivityAt: domain.Unknown()}

	if p.Evaluate(context.Background(), proj).Match {
		t.Error("a project with unknown activity must never match an inactivity policy")
	}
}

func TestInactivePolicy_KnownNeverActiveMatches(t *testing.T) {
	clock := domain.FixedClock{Instant: time.Now()}
	p := InactivePolicy{ThresholdDays: 90, Clock: clock}
	proj := domain.Project{LastActivityAt: domain.Known(nil)}

	if !p.Evaluate(context.Background(), proj).Match {
		t.Error("a project known to have never had activity must match")
	}
}
