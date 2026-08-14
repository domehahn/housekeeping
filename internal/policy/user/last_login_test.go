package user

import (
	"context"
	"testing"
	"time"

	"github.com/domehahn/housekeeping/internal/domain"
)

func TestLastLoginPolicy_BoundarySemantics(t *testing.T) {
	now := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	clock := domain.FixedClock{Instant: now}

	tests := []struct {
		name      string
		daysAgo   int
		threshold int
		wantMatch bool
	}{
		{"31 days ago, threshold 30: matches", 31, 30, true},
		{"29 days ago, threshold 30: does not match", 29, 30, false},
		{"exactly 30 days ago, threshold 30: does not match (boundary is exclusive)", 30, 30, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts := now.Add(-time.Duration(tc.daysAgo) * 24 * time.Hour)
			u := domain.User{LastLoginAt: domain.Known(&ts)}
			p := LastLoginPolicy{ThresholdDays: tc.threshold, Clock: clock, OnUnknown: UnknownSkip}

			eval := p.Evaluate(context.Background(), u)
			if eval.Match != tc.wantMatch {
				t.Errorf("Match = %v, want %v (reasons: %v)", eval.Match, tc.wantMatch, eval.Reasons)
			}
			if len(eval.Reasons) == 0 {
				t.Error("expected at least one reason")
			}
		})
	}
}

func TestLastLoginPolicy_NeverLoggedInMatches(t *testing.T) {
	clock := domain.FixedClock{Instant: time.Now()}
	u := domain.User{LastLoginAt: domain.Known(nil)}
	p := LastLoginPolicy{ThresholdDays: 30, Clock: clock, OnUnknown: UnknownSkip}

	eval := p.Evaluate(context.Background(), u)
	if !eval.Match {
		t.Error("a user known to have never logged in must match an inactivity threshold")
	}
}

func TestLastLoginPolicy_UnknownActivityDefaultsToSkip(t *testing.T) {
	clock := domain.FixedClock{Instant: time.Now()}
	u := domain.User{LastLoginAt: domain.Unknown()}
	p := LastLoginPolicy{ThresholdDays: 30, Clock: clock, OnUnknown: UnknownSkip}

	eval := p.Evaluate(context.Background(), u)
	if eval.Match {
		t.Error("unknown activity must never be treated as a match under the default (skip) behavior")
	}
}

func TestLastLoginPolicy_UnknownActivityCanBeConfiguredToMatch(t *testing.T) {
	clock := domain.FixedClock{Instant: time.Now()}
	u := domain.User{LastLoginAt: domain.Unknown()}
	p := LastLoginPolicy{ThresholdDays: 30, Clock: clock, OnUnknown: UnknownMatch}

	eval := p.Evaluate(context.Background(), u)
	if !eval.Match {
		t.Error("unknown_activity=match must treat unknown data as a match when explicitly configured")
	}
}

func TestLastLoginPolicy_UnknownActivityWarnDoesNotMatch(t *testing.T) {
	clock := domain.FixedClock{Instant: time.Now()}
	u := domain.User{LastLoginAt: domain.Unknown()}
	p := LastLoginPolicy{ThresholdDays: 30, Clock: clock, OnUnknown: UnknownWarn}

	eval := p.Evaluate(context.Background(), u)
	if eval.Match {
		t.Error("unknown_activity=warn must still not match - it only changes reporting, not the decision")
	}
}
