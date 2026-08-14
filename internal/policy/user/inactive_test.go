package user

import (
	"context"
	"testing"
	"time"

	"github.com/domehahn/housekeeping/internal/domain"
)

func TestInactiveUserPolicy_MatchAllRequiresBoth(t *testing.T) {
	now := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	clock := domain.FixedClock{Instant: now}
	old := now.Add(-40 * 24 * time.Hour)
	recent := now.Add(-5 * 24 * time.Hour)

	policy := func() InactiveUserPolicy {
		return InactiveUserPolicy{
			LastLogin:    LastLoginPolicy{ThresholdDays: 30, Clock: clock, OnUnknown: UnknownSkip},
			LastActivity: LastActivityPolicy{ThresholdDays: 30, Clock: clock, OnUnknown: UnknownSkip},
			Mode:         MatchAll,
		}
	}

	t.Run("both old: matches", func(t *testing.T) {
		u := domain.User{LastLoginAt: domain.Known(&old), LastActivityAt: domain.Known(&old)}
		if !policy().Evaluate(context.Background(), u).Match {
			t.Error("expected match")
		}
	})

	t.Run("only login old: does not match under all", func(t *testing.T) {
		u := domain.User{LastLoginAt: domain.Known(&old), LastActivityAt: domain.Known(&recent)}
		if policy().Evaluate(context.Background(), u).Match {
			t.Error("expected no match")
		}
	})
}

func TestInactiveUserPolicy_MatchAnyRequiresEither(t *testing.T) {
	now := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	clock := domain.FixedClock{Instant: now}
	old := now.Add(-40 * 24 * time.Hour)
	recent := now.Add(-5 * 24 * time.Hour)

	p := InactiveUserPolicy{
		LastLogin:    LastLoginPolicy{ThresholdDays: 30, Clock: clock, OnUnknown: UnknownSkip},
		LastActivity: LastActivityPolicy{ThresholdDays: 30, Clock: clock, OnUnknown: UnknownSkip},
		Mode:         MatchAny,
	}

	u := domain.User{LastLoginAt: domain.Known(&old), LastActivityAt: domain.Known(&recent)}
	if !p.Evaluate(context.Background(), u).Match {
		t.Error("expected match: any requires only one criterion")
	}
}

func TestParseMatchMode(t *testing.T) {
	tests := map[string]struct {
		want    MatchMode
		wantErr bool
	}{
		"":      {MatchAll, false},
		"all":   {MatchAll, false},
		"any":   {MatchAny, false},
		"bogus": {"", true},
	}
	for in, tc := range tests {
		got, err := ParseMatchMode(in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseMatchMode(%q) err = %v, wantErr %v", in, err, tc.wantErr)
		}
		if err == nil && got != tc.want {
			t.Errorf("ParseMatchMode(%q) = %v, want %v", in, got, tc.want)
		}
	}
}
