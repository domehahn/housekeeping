package provider

import (
	"context"

	"github.com/domehahn/housekeeping/internal/domain"
)

// Each interface below is a small, single-purpose capability port. The
// application layer depends only on the narrow interfaces a given use case
// actually needs; a concrete adapter (e.g. the GitLab adapter) implements
// all of them on one struct for convenience, but nothing requires that.

// ScopeResolver turns a human-provided scope path (e.g. a group path) into
// a fully resolved domain.Scope with a provider ID populated, and - when
// Recursive is set - walks subgroups to build the effective set of group
// IDs the scope covers.
type ScopeResolver interface {
	ResolveGroupScope(ctx context.Context, path string, recursive bool) (domain.Scope, []domain.Group, error)
}

// ProjectReader lists projects within a resolved scope.
type ProjectReader interface {
	ListProjects(ctx context.Context, scope domain.Scope) ([]domain.Project, error)
}

// ProjectGetter fetches the current state of a single project by ID,
// primarily used to revalidate a planned action immediately before
// executing it.
type ProjectGetter interface {
	GetProject(ctx context.Context, projectID string) (domain.Project, error)
}

// UserGetter fetches the current state of a single user by ID, primarily
// used to revalidate a planned action immediately before executing it.
type UserGetter interface {
	GetUser(ctx context.Context, userID string) (domain.User, error)
}

// ProjectDeleter deletes a single project by its stable provider ID.
type ProjectDeleter interface {
	DeleteProject(ctx context.Context, projectID string) error
}

// ProjectArchiver archives a single project by its stable provider ID.
type ProjectArchiver interface {
	ArchiveProject(ctx context.Context, projectID string) error
}

// GroupMemberReader lists members of a resolved scope (optionally spanning
// subgroups), annotated with how each membership was obtained.
type GroupMemberReader interface {
	ListGroupMembers(ctx context.Context, scope domain.Scope) ([]domain.User, error)
}

// GroupMemberRemover removes a direct member from a specific group. It must
// refuse (return a validation error) to attempt removal of a non-direct
// membership - see docs/architecture.md "GitLab membership semantics".
type GroupMemberRemover interface {
	RemoveGroupMember(ctx context.Context, groupID string, userID string) error
}

// UserBlocker blocks a user account instance-wide. Typically requires
// administrator rights on the provider.
type UserBlocker interface {
	BlockUser(ctx context.Context, userID string) error
}

// BillableMembersReader reports which members of a top-level group count
// against that group's paid seats, using the provider's own authoritative
// billing determination rather than an approximation derived from access
// level (which is tier-dependent and therefore not reliably inferable).
// The returned set contains the IDs of billable users; a user absent from
// it is not billable in that group.
type BillableMembersReader interface {
	ListBillableGroupMembers(ctx context.Context, groupID string) (map[string]bool, error)
}

// UserMembershipReader lists every group a user belongs to across the
// whole instance, independent of any particular scope. Typically requires
// administrator rights. Used only to support the opt-in billable-seat
// override (see policy/user.BillableSeatOverride) - never part of normal
// discovery.
type UserMembershipReader interface {
	ListUserMemberships(ctx context.Context, userID string) ([]domain.Membership, error)
}

// CurrentUserResolver identifies the authenticated caller so it can be
// automatically excluded from destructive user operations.
type CurrentUserResolver interface {
	CurrentUser(ctx context.Context) (domain.User, error)
}

// CapabilitiesReporter reports which operations are actually available.
type CapabilitiesReporter interface {
	Capabilities(ctx context.Context) (Capabilities, error)
}

// InfoReporter reports metadata about the connected instance/identity.
type InfoReporter interface {
	Info(ctx context.Context) (Info, error)
}

// Client aggregates every port a concrete adapter implements. Use cases and
// the CLI wire against this for convenience, but each internal/app
// function still declares only the narrow interfaces it needs as
// parameters, keeping the dependency explicit and independently testable.
type Client interface {
	ScopeResolver
	ProjectReader
	ProjectGetter
	ProjectDeleter
	ProjectArchiver
	GroupMemberReader
	GroupMemberRemover
	UserGetter
	UserBlocker
	CurrentUserResolver
	CapabilitiesReporter
	InfoReporter
	BillableMembersReader
	UserMembershipReader
}
