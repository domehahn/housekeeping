package user

import (
	"testing"

	"github.com/domehahn/housekeeping/internal/domain"
)

func TestBillableSeatOverride_Disabled(t *testing.T) {
	o := BillableSeatOverride{Enabled: false, Threshold: domain.AccessLevelDeveloper}
	eval := o.Evaluate("1", "company", true, nil)
	if eval.Match {
		t.Error("a disabled override must never match")
	}
}

func TestBillableSeatOverride_NotBillableInTarget(t *testing.T) {
	o := BillableSeatOverride{Enabled: true, Threshold: domain.AccessLevelDeveloper}
	eval := o.Evaluate("1", "company", false, nil)
	if eval.Match {
		t.Error("override must not apply when the user is not billable in the target group")
	}
}

func TestBillableSeatOverride_BillableElsewhereProtects(t *testing.T) {
	o := BillableSeatOverride{Enabled: true, Threshold: domain.AccessLevelDeveloper}
	other := []domain.Membership{
		{SourceType: domain.MembershipSourceGroup, SourceID: "2", SourceName: "other-group", AccessLevel: domain.AccessLevelMaintainer},
	}
	eval := o.Evaluate("1", "company", true, other)
	if eval.Match {
		t.Error("a Maintainer-level membership elsewhere must keep the user protected")
	}
}

func TestBillableSeatOverride_OnlyGuestReporterElsewhereOverrides(t *testing.T) {
	o := BillableSeatOverride{Enabled: true, Threshold: domain.AccessLevelDeveloper}
	other := []domain.Membership{
		{SourceType: domain.MembershipSourceGroup, SourceID: "2", SourceName: "other-group", AccessLevel: domain.AccessLevelReporter},
		{SourceType: domain.MembershipSourceGroup, SourceID: "3", SourceName: "third-group", AccessLevel: domain.AccessLevelGuest},
	}
	eval := o.Evaluate("1", "company", true, other)
	if !eval.Match {
		t.Error("expected override to match: only Guest/Reporter memberships elsewhere, below the Developer threshold")
	}
}

func TestBillableSeatOverride_TargetGroupMembershipExcludedFromElsewhereCheck(t *testing.T) {
	o := BillableSeatOverride{Enabled: true, Threshold: domain.AccessLevelDeveloper}
	// The target group's own (billable) membership must not count as
	// "elsewhere" - otherwise the override could never fire for exactly
	// the users it is meant for.
	other := []domain.Membership{
		{SourceType: domain.MembershipSourceGroup, SourceID: "1", SourceName: "company", AccessLevel: domain.AccessLevelMaintainer},
	}
	eval := o.Evaluate("1", "company", true, other)
	if !eval.Match {
		t.Error("expected override to match: the only privileged membership is the target group itself, not 'elsewhere'")
	}
}

func TestBillableSeatOverride_ProjectMembershipsIgnored(t *testing.T) {
	o := BillableSeatOverride{Enabled: true, Threshold: domain.AccessLevelDeveloper}
	other := []domain.Membership{
		{SourceType: domain.MembershipSourceProject, SourceID: "99", SourceName: "some-project", AccessLevel: domain.AccessLevelMaintainer},
	}
	eval := o.Evaluate("1", "company", true, other)
	if !eval.Match {
		t.Error("expected override to match: only project-level memberships exist elsewhere, group memberships are what count")
	}
}

func TestAccessLevel_AtLeast(t *testing.T) {
	tests := []struct {
		level domain.AccessLevel
		other domain.AccessLevel
		want  bool
	}{
		{domain.AccessLevelMaintainer, domain.AccessLevelDeveloper, true},
		{domain.AccessLevelDeveloper, domain.AccessLevelDeveloper, true},
		{domain.AccessLevelReporter, domain.AccessLevelDeveloper, false},
		{domain.AccessLevelUnknown, domain.AccessLevelGuest, false},
	}
	for _, tc := range tests {
		if got := tc.level.AtLeast(tc.other); got != tc.want {
			t.Errorf("%s.AtLeast(%s) = %v, want %v", tc.level, tc.other, got, tc.want)
		}
	}
}
