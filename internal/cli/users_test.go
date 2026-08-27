package cli

import (
	"testing"

	"github.com/domehahn/housekeeping/internal/config"
	"github.com/domehahn/housekeeping/internal/domain"
)

func TestBuildUserPolicyRequiresExplicitEnablement(t *testing.T) {
	e := &env{cfg: config.Default(), clock: domain.RealClock{}}
	if _, err := buildUserPolicy(e, &userFlags{}); err == nil {
		t.Fatal("disabled user inactivity policy must not silently become a zero-day policy")
	}
}

func TestBuildUserPolicyFlagEnablesPolicy(t *testing.T) {
	e := &env{cfg: config.Default(), clock: domain.RealClock{}}
	if _, err := buildUserPolicy(e, &userFlags{inactiveFor: "30d"}); err != nil {
		t.Fatalf("explicit inactivity flag should enable the policy: %v", err)
	}
}
