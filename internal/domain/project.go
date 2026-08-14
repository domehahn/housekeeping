package domain

import "time"

// Project is a provider-independent representation of a repository /
// project hosted on an SCM platform. Adapters map their native API types
// into this struct; nothing outside an adapter may construct a Project from
// a provider SDK type directly.
type Project struct {
	// ID is the provider's stable identifier for the project. It is what
	// destructive operations must reference, never a name or path.
	ID string

	Name     string
	Path     string
	FullPath string
	Archived bool

	CreatedAt      time.Time
	LastActivityAt Timestamp

	WebURL string

	// Namespace is the full path of the group/namespace the project lives
	// in, used for scoping and protection-rule matching.
	Namespace string
}
