package domain

// UserState is the provider-reported account state.
type UserState string

const (
	UserStateActive      UserState = "active"
	UserStateBlocked     UserState = "blocked"
	UserStateDeactivated UserState = "deactivated"
	UserStateUnknown     UserState = "unknown"
)

// AccessLevel is a coarse, provider-independent membership role, ordered
// from least to most privileged.
type AccessLevel string

const (
	AccessLevelUnknown    AccessLevel = "unknown"
	AccessLevelGuest      AccessLevel = "guest"
	AccessLevelReporter   AccessLevel = "reporter"
	AccessLevelDeveloper  AccessLevel = "developer"
	AccessLevelMaintainer AccessLevel = "maintainer"
	AccessLevelOwner      AccessLevel = "owner"
)

// accessLevelRank orders levels from least to most privileged so callers
// can compare thresholds (e.g. "Developer or higher") without a long
// switch statement. AccessLevelUnknown ranks below Guest deliberately -
// unknown must never be treated as meeting a privilege threshold.
var accessLevelRank = map[AccessLevel]int{
	AccessLevelUnknown:    0,
	AccessLevelGuest:      1,
	AccessLevelReporter:   2,
	AccessLevelDeveloper:  3,
	AccessLevelMaintainer: 4,
	AccessLevelOwner:      5,
}

// AtLeast reports whether this access level is at least as privileged as
// other. An unknown level never satisfies any threshold.
func (a AccessLevel) AtLeast(other AccessLevel) bool {
	if a == AccessLevelUnknown {
		return false
	}
	return accessLevelRank[a] >= accessLevelRank[other]
}

// MembershipOrigin describes how a user's membership in the evaluated scope
// was established. This matters because a group-member-removal action can
// only ever act on a direct membership of the group being evaluated - it
// cannot reach into a parent group or remove a project-only membership.
type MembershipOrigin string

const (
	MembershipDirect    MembershipOrigin = "direct"
	MembershipInherited MembershipOrigin = "inherited"
	MembershipUnknown   MembershipOrigin = "unknown"
)

// User is a provider-independent representation of an account.
type User struct {
	ID       string
	Username string
	Name     string
	State    UserState

	LastLoginAt    Timestamp
	LastActivityAt Timestamp

	AccessLevel      AccessLevel
	MembershipOrigin MembershipOrigin

	// GroupID is the ID of the group the membership was read from (the
	// evaluated scope's group), required to perform a RemoveGroupMember
	// action safely.
	GroupID string

	WebURL string
}

// MembershipSourceType distinguishes what kind of resource a cross-instance
// Membership grants access to.
type MembershipSourceType string

const (
	MembershipSourceGroup   MembershipSourceType = "group"
	MembershipSourceProject MembershipSourceType = "project"
)

// Membership describes a user's access to a single group or project
// elsewhere on the instance, independent of whichever scope is currently
// being evaluated. It exists solely to support cross-group "does this user
// hold a privileged role anywhere else" checks (see
// policy/user.BillableSeatOverride) - it is not used for the normal
// discovery/evaluation flow, and fetching it requires administrator
// rights.
type Membership struct {
	SourceType  MembershipSourceType
	SourceID    string
	SourceName  string
	AccessLevel AccessLevel
}
