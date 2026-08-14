package provider

// Support describes the availability of a single capability. A plain bool
// is not expressive enough: some GitLab operations work for any
// authenticated user, some require group-owner rights, and some require
// instance-admin rights that a typical automation token will not have.
type Support string

const (
	SupportSupported     Support = "supported"
	SupportUnsupported   Support = "unsupported"
	SupportRequiresAdmin Support = "requires-admin"
	SupportRequiresOwner Support = "requires-owner"
	SupportUnknown       Support = "unknown"
)

// Capabilities reports what an adapter can actually do against the
// authenticated credentials. The CLI surfaces this via
// `scm-cleaner provider capabilities` so operators can see, before
// planning anything, which actions are realistically available.
type Capabilities struct {
	ListProjects        Support
	DeleteProjects      Support
	ArchiveProjects     Support
	ListGroups          Support
	ListGroupMembers    Support
	RemoveGroupMember   Support
	BlockUsers          Support
	DeleteUsers         Support
	UserLastLogin       Support
	UserLastActivity    Support
	ProjectLastActivity Support
	BillableMembers     Support
	UserMemberships     Support
}

// Info describes the connected provider instance and identity, surfaced by
// `scm-cleaner provider info` / `doctor`. Never includes token material.
type Info struct {
	Provider        string
	Instance        string
	ServerVersion   string
	AuthenticatedAs string
	IsAdmin         bool
}
