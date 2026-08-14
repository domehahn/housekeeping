package app

import (
	"context"
	"testing"

	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/policy/user"
	"github.com/domehahn/housekeeping/internal/provider"
)

type fakeBillableReader struct {
	billable         map[string]bool
	billableErr      error
	memberships      map[string][]domain.Membership
	membershipErrFor map[string]error
}

func (f *fakeBillableReader) ListBillableGroupMembers(_ context.Context, _ string) (map[string]bool, error) {
	if f.billableErr != nil {
		return nil, f.billableErr
	}
	return f.billable, nil
}

func (f *fakeBillableReader) ListUserMemberships(_ context.Context, userID string) ([]domain.Membership, error) {
	if err, ok := f.membershipErrFor[userID]; ok {
		return nil, err
	}
	return f.memberships[userID], nil
}

var _ BillableOverrideReader = (*fakeBillableReader)(nil)

func baseSummary() UserEvaluationSummary {
	return UserEvaluationSummary{
		Results: []UserEvaluation{
			{User: domain.User{ID: "1", Username: "alice"}, Evaluation: domain.Evaluation{Match: false, Reasons: []string{"last login: 5 days ago <= threshold 30 days"}}},
			{User: domain.User{ID: "2", Username: "bob"}, Evaluation: domain.Evaluation{Match: false}, Protected: true},
			{User: domain.User{ID: "3", Username: "carol"}, Evaluation: domain.Evaluation{Match: true, Reasons: []string{"already matched"}}},
		},
	}
}

func TestApplyBillableSeatOverride_Disabled(t *testing.T) {
	reader := &fakeBillableReader{}
	result, err := ApplyBillableSeatOverride(context.Background(), reader, domain.Scope{ID: "1", Path: "company"}, baseSummary(), user.BillableSeatOverride{Enabled: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Summary.Matched()) != 1 {
		t.Errorf("expected the disabled override to leave the summary unchanged, got %d matches", len(result.Summary.Matched()))
	}
}

func TestApplyBillableSeatOverride_MatchesBillableUserWithNoElsewhere(t *testing.T) {
	reader := &fakeBillableReader{
		billable:    map[string]bool{"1": true},
		memberships: map[string][]domain.Membership{"1": nil},
	}
	summary := baseSummary()
	override := user.BillableSeatOverride{Enabled: true, Threshold: domain.AccessLevelDeveloper}

	result, err := ApplyBillableSeatOverride(context.Background(), reader, domain.Scope{ID: "1", Path: "company"}, summary, override)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	matched := result.Summary.Matched()
	if len(matched) != 2 {
		t.Fatalf("expected alice to be newly matched alongside carol, got %d: %+v", len(matched), matched)
	}
}

func TestApplyBillableSeatOverride_ProtectedUserNeverOverridden(t *testing.T) {
	reader := &fakeBillableReader{
		billable:    map[string]bool{"2": true},
		memberships: map[string][]domain.Membership{"2": nil},
	}
	summary := baseSummary()
	override := user.BillableSeatOverride{Enabled: true, Threshold: domain.AccessLevelDeveloper}

	result, err := ApplyBillableSeatOverride(context.Background(), reader, domain.Scope{ID: "1", Path: "company"}, summary, override)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range result.Summary.Results {
		if r.User.Username == "bob" && r.Matched() {
			t.Error("a protected user must never be matched by the override, regardless of billable status")
		}
	}
}

func TestApplyBillableSeatOverride_NotBillableUserUnaffected(t *testing.T) {
	reader := &fakeBillableReader{
		billable: map[string]bool{}, // alice is not billable
	}
	summary := baseSummary()
	override := user.BillableSeatOverride{Enabled: true, Threshold: domain.AccessLevelDeveloper}

	result, err := ApplyBillableSeatOverride(context.Background(), reader, domain.Scope{ID: "1", Path: "company"}, summary, override)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Summary.Matched()) != 1 {
		t.Errorf("expected no new matches when the user is not billable in the target group, got %d", len(result.Summary.Matched()))
	}
}

func TestApplyBillableSeatOverride_BillableListErrorFailsSafe(t *testing.T) {
	reader := &fakeBillableReader{billableErr: provider.NewError(provider.KindAuthorization, "list billable members", "not an owner", nil)}
	summary := baseSummary()
	override := user.BillableSeatOverride{Enabled: true, Threshold: domain.AccessLevelDeveloper}

	_, err := ApplyBillableSeatOverride(context.Background(), reader, domain.Scope{ID: "1", Path: "company"}, summary, override)
	if err == nil {
		t.Fatal("expected an error when the billable-members list cannot be retrieved")
	}
}

func TestApplyBillableSeatOverride_MembershipErrorSkipsThatUserOnly(t *testing.T) {
	reader := &fakeBillableReader{
		billable:         map[string]bool{"1": true},
		membershipErrFor: map[string]error{"1": provider.NewError(provider.KindTemporary, "list memberships", "boom", nil)},
	}
	summary := baseSummary()
	override := user.BillableSeatOverride{Enabled: true, Threshold: domain.AccessLevelDeveloper}

	result, err := ApplyBillableSeatOverride(context.Background(), reader, domain.Scope{ID: "1", Path: "company"}, summary, override)
	if err != nil {
		t.Fatalf("unexpected top-level error: %v", err)
	}
	if len(result.Warnings) != 1 {
		t.Errorf("expected 1 warning for alice's failed membership lookup, got %d: %v", len(result.Warnings), result.Warnings)
	}
	if len(result.Summary.Matched()) != 1 {
		t.Error("a user whose membership lookup failed must not be matched (fail safe)")
	}
}
