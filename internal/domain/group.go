package domain

// Group is a provider-independent representation of a group / organization
// / namespace that can contain projects, subgroups, and members.
type Group struct {
	ID       string
	Name     string
	Path     string
	FullPath string

	// ParentID is nil for a top-level group.
	ParentID *string
}

// ScopeType identifies what kind of resource a Scope anchors to. Only
// "group" is implemented for the GitLab MVP; the type exists so other
// anchors (organization, project) can be added later without changing
// call sites that just pass a Scope around.
type ScopeType string

const (
	ScopeTypeGroup ScopeType = "group"
)

// Scope describes the boundary of a discovery/evaluation run in
// provider-independent terms. Adapters translate it into whatever
// provider-specific lookups are required (e.g. resolving a GitLab group
// path to a numeric ID and walking subgroups).
type Scope struct {
	Type ScopeType
	// ID is the resolved provider ID of the anchor resource, populated by
	// the adapter once the scope has been resolved. May be empty before
	// resolution.
	ID string
	// Path is the human-provided path/slug identifying the anchor (e.g.
	// "company/platform").
	Path string
	// Recursive controls whether subgroups are included.
	Recursive bool
	// GroupIDs is the full set of group IDs this scope covers: just the
	// anchor group's ID when Recursive is false, or the anchor plus every
	// descendant subgroup's ID when true. Populated by ScopeResolver.
	GroupIDs []string
}
