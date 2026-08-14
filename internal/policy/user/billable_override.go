package user

import (
	"fmt"

	"github.com/domehahn/housekeeping/internal/domain"
)

// BillableSeatOverride re-evaluates a user who was NOT matched by the
// standard inactivity policy solely because of recent global activity
// elsewhere on the instance (see LastLoginPolicy/LastActivityPolicy - both
// use instance-wide, not per-group, timestamps). If the user counts as
// billable in the group being cleaned (per GitLab's own billable-members
// determination) but holds no membership at or above Threshold in any
// *other* group, the override treats them as a match anyway: whatever
// activity was protecting them was not tied to a role that costs a paid
// seat elsewhere, so it does not justify keeping the billable-but-unused
// membership in the target group.
//
// This is a deliberate, documented approximation for "other" groups, not
// the same guarantee GitLab itself gives for the target group: whether a
// given access level counts as billable is tier-dependent (e.g. Guest is
// billable on Premium but not on Free/Ultimate) and this adapter has no
// way to query that for a group it does not have Owner access to. Only
// the *target* group's billable status comes from GitLab's own
// authoritative endpoint; other groups are judged by a configurable
// access-level threshold (default: Developer or higher). See
// docs/adr/0004-billable-seat-override.md.
//
// Disabled by default - this overrides the otherwise-protective effect of
// global activity, so it must be turned on deliberately
// (users.inactive.ignore_global_activity_if_non_billable_elsewhere).
type BillableSeatOverride struct {
	Enabled   bool
	Threshold domain.AccessLevel
}

// Evaluate decides whether to override protection for a single user.
//
// targetGroupID/targetGroupPath identify the group being cleaned.
// targetBillable must come from GitLab's authoritative billable-members
// list for that group - never approximated. otherMemberships is the
// user's full cross-instance membership list (including, harmlessly, the
// target group itself - it is excluded from the "elsewhere" check
// automatically).
func (o BillableSeatOverride) Evaluate(targetGroupID, targetGroupPath string, targetBillable bool, otherMemberships []domain.Membership) domain.Evaluation {
	if !o.Enabled {
		return domain.Evaluation{Match: false, Reasons: []string{"billable seat override: disabled"}}
	}
	if !targetBillable {
		return domain.Evaluation{
			Match:   false,
			Reasons: []string{fmt.Sprintf("billable seat override: user does not count as billable in %q, override does not apply", targetGroupPath)},
		}
	}

	for _, m := range otherMemberships {
		if m.SourceType != domain.MembershipSourceGroup {
			continue
		}
		if m.SourceID == targetGroupID {
			continue // the target group's own membership is what we're deciding about, not "elsewhere"
		}
		if m.AccessLevel.AtLeast(o.Threshold) {
			return domain.Evaluation{
				Match: false,
				Reasons: []string{fmt.Sprintf(
					"billable seat override: user holds a %s membership in %q elsewhere (>= threshold %s), protection stands",
					m.AccessLevel, m.SourceName, o.Threshold,
				)},
			}
		}
	}

	return domain.Evaluation{
		Match: true,
		Reasons: []string{fmt.Sprintf(
			"billable seat override: billable in %q but holds no membership at or above %q in any other group on the instance",
			targetGroupPath, o.Threshold,
		)},
	}
}
