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

// GroupMemberGetter fetches one direct group membership for safe
// pre-execution revalidation, including its current access level.
type GroupMemberGetter interface {
	GetGroupMember(ctx context.Context, groupID string, userID string) (domain.User, error)
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

// PipelineConfigProposer reads a project's .gitlab-ci.yml at its default
// branch and, if a change is needed, opens a Merge Request proposing a CI
// tag additions. It never commits directly to the default branch - see
// docs/adr/0005-ci-tag-management-scope.md.
type PipelineConfigProposer interface {
	// GetPipelineConfig fetches a project's .gitlab-ci.yml at its default
	// branch. exists is false (with a nil error) when the project simply
	// has no CI file - that is a normal, expected case, not a failure.
	GetPipelineConfig(ctx context.Context, projectID string) (content []byte, exists bool, err error)
	// ProposePipelineTagChange opens a branch + commit + Merge Request
	// proposing patchedContent as the new .gitlab-ci.yml. tags are used only
	// for the branch name/commit message/MR description, not re-validated
	// here - callers must have already confirmed patchedContent actually
	// differs from the current file.
	ProposePipelineTagChange(ctx context.Context, projectID string, patchedContent []byte, tags []string) (mergeRequestURL string, err error)
	// ProposePipelineTagRename opens a branch + commit + Merge Request
	// replacing an old (wrong) CI tag with the corrected one for every
	// rename pair, and best-effort closes any still-open scm-cleaner
	// proposal that proposed one of the old tags - identified by the same
	// cryptographic tag-set marker ListPipelineTagProposals already uses,
	// so only scm-cleaner's own matching proposals are ever touched, never
	// an unrelated Merge Request. A failure to close an old proposal is
	// never fatal to the rename itself: closedProposalURLs reports only
	// the ones actually closed, and the caller surfaces the difference
	// between attempted and closed rather than the rename silently failing.
	ProposePipelineTagRename(ctx context.Context, projectID string, patchedContent []byte, renames []domain.TagRename) (mergeRequestURL string, closedProposalURLs []string, err error)
	// ClosePipelineTagProposals best-effort closes every still-open
	// scm-cleaner proposal for any of oldTags, without touching
	// .gitlab-ci.yml or opening any new proposal. It exists for the case
	// where a project's file needs no change (already correct, or the
	// old tag was never merged in the first place) but a stale,
	// still-open Merge Request from an earlier, wrong-tag run remains -
	// scoped by the same cryptographic tag-set marker as
	// ListPipelineTagProposals/ProposePipelineTagRename, so only
	// scm-cleaner's own matching, still-open proposals are ever touched.
	ClosePipelineTagProposals(ctx context.Context, projectID string, oldTags []string) (closedProposalURLs []string, err error)
}

// PipelineConfigAnalyzer returns GitLab's effective, include-expanded CI
// configuration. It is read-only and never implies that included sources can
// safely be modified.
type PipelineConfigAnalyzer interface {
	GetMergedPipelineConfig(ctx context.Context, projectID string) (content []byte, includes []domain.PipelineInclude, err error)
}

// PipelineProposalReporter reports scm-cleaner Merge Requests for a project.
type PipelineProposalReporter interface {
	ListPipelineTagProposals(ctx context.Context, projectID string, tags []string) ([]domain.PipelineProposal, error)
}

// RunnerScanner lists runners available to a set of projects. Results include
// explicit project assignments and whether the runner's complete effective
// reach can be proven inside the supplied scope.
type RunnerScanner interface {
	ListRunnersForProjects(ctx context.Context, scope domain.Scope, projectIDs []string) ([]domain.Runner, error)
	GetRunnerForProjects(ctx context.Context, runnerID string, scope domain.Scope, projectIDs []string) (domain.Runner, error)
}

// RunnerTagUpdater reads and updates a runner's tag list. The update call
// replaces the whole list (GitLab's API is not additive), so callers
// fetch the current list first, compute the desired union, and pass that.
type RunnerTagUpdater interface {
	GetRunnerTags(ctx context.Context, runnerID string) ([]string, error)
	UpdateRunnerTags(ctx context.Context, runnerID string, expectedTags, tags []string) error
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
	GroupMemberGetter
	GroupMemberRemover
	UserGetter
	UserBlocker
	CurrentUserResolver
	CapabilitiesReporter
	InfoReporter
	BillableMembersReader
	UserMembershipReader
	PipelineConfigProposer
	PipelineConfigAnalyzer
	PipelineProposalReporter
	RunnerScanner
	RunnerTagUpdater
}
