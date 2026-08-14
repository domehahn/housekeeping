package app

import (
	"context"
	"testing"
	"time"

	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/policy/user"
)

func TestEvaluateUsers_ProtectionOverridesMatch(t *testing.T) {
	now := time.Now()
	old := now.Add(-100 * 24 * time.Hour)
	clock := domain.FixedClock{Instant: now}

	users := []domain.User{
		{ID: "1", Username: "root", LastLoginAt: domain.Known(&old), LastActivityAt: domain.Known(&old)},
		{ID: "2", Username: "alice", LastLoginAt: domain.Known(&old), LastActivityAt: domain.Known(&old)},
	}

	policy := user.InactiveUserPolicy{
		LastLogin:    user.LastLoginPolicy{ThresholdDays: 30, Clock: clock, OnUnknown: user.UnknownSkip},
		LastActivity: user.LastActivityPolicy{ThresholdDays: 30, Clock: clock, OnUnknown: user.UnknownSkip},
		Mode:         user.MatchAll,
	}
	protection, err := user.NewProtection([]string{"root"}, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	summary := EvaluateUsers(context.Background(), users, policy, protection)

	matched := summary.Matched()
	if len(matched) != 1 || matched[0].User.Username != "alice" {
		t.Errorf("expected only alice to be matched, got %+v", matched)
	}
	if len(summary.Protected()) != 1 || summary.Protected()[0].User.Username != "root" {
		t.Errorf("expected root to be reported as protected, got %+v", summary.Protected())
	}
}

func TestEvaluateUsers_UnknownActivityIsReportedSeparatelyFromMatch(t *testing.T) {
	clock := domain.FixedClock{Instant: time.Now()}
	users := []domain.User{
		{ID: "1", Username: "ghost", LastLoginAt: domain.Unknown(), LastActivityAt: domain.Unknown()},
	}
	policy := user.InactiveUserPolicy{
		LastLogin:    user.LastLoginPolicy{ThresholdDays: 30, Clock: clock, OnUnknown: user.UnknownSkip},
		LastActivity: user.LastActivityPolicy{ThresholdDays: 30, Clock: clock, OnUnknown: user.UnknownSkip},
		Mode:         user.MatchAll,
	}

	summary := EvaluateUsers(context.Background(), users, policy, nil)

	if len(summary.Matched()) != 0 {
		t.Error("unknown activity must not produce a match under the default skip behavior")
	}
	if len(summary.Unknown()) != 1 {
		t.Error("unknown activity must still be reported so operators see the data-quality gap")
	}
}
