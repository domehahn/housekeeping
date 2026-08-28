package gitlab

import (
	"context"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/domehahn/housekeeping/internal/provider"
)

// Capabilities reports what this adapter can actually do with the
// currently authenticated credentials. Some entries depend on whether the
// token belongs to an instance administrator, which is only known at
// runtime, so this method makes a `GET /user` call.
//
// Sources for these determinations (see docs/architecture.md for the full
// citation list): the GitLab REST API docs for Projects, Groups, Members,
// and Users, current as of GitLab 17.x.
func (a *Adapter) Capabilities(ctx context.Context) (provider.Capabilities, error) {
	isAdmin, err := a.isCurrentUserAdmin(ctx)
	if err != nil {
		// Even if we can't determine admin status, the non-admin
		// capabilities are still accurate to report.
		isAdmin = false
	}

	loginActivity := provider.SupportRequiresAdmin
	deleteUsers := provider.SupportRequiresAdmin
	blockUsers := provider.SupportRequiresAdmin
	if isAdmin {
		loginActivity = provider.SupportSupported
		deleteUsers = provider.SupportUnsupported // deliberately not implemented, see README limitations
		blockUsers = provider.SupportSupported
	}

	userMemberships := provider.SupportRequiresAdmin
	if isAdmin {
		userMemberships = provider.SupportSupported
	}

	return provider.Capabilities{
		ListProjects:        provider.SupportSupported,
		DeleteProjects:      provider.SupportRequiresOwner,
		ArchiveProjects:     provider.SupportRequiresOwner,
		ListGroups:          provider.SupportSupported,
		ListGroupMembers:    provider.SupportSupported,
		RemoveGroupMember:   provider.SupportRequiresOwner,
		BlockUsers:          blockUsers,
		DeleteUsers:         deleteUsers,
		UserLastLogin:       loginActivity,
		UserLastActivity:    loginActivity,
		ProjectLastActivity: provider.SupportSupported,
		// Billable members requires Owner on a top-level group specifically
		// (not merely any group) - this cannot be determined generically
		// here, so it is reported as requires-owner rather than probed.
		BillableMembers: provider.SupportRequiresOwner,
		UserMemberships: userMemberships,
		// Opening a Merge Request requires at least Developer on the
		// project so the caller can push the proposal branch.
		ProposePipelineTags: provider.SupportRequiresDeveloper,
		// GitLab authorizes runner updates through the manage_runner
		// permission; the role granting it depends on runner ownership.
		UpdateRunnerTags: provider.SupportRequiresManageRunner,
	}, nil
}

// Info reports metadata about the connected instance and identity. Never
// includes token material.
func (a *Adapter) Info(ctx context.Context) (provider.Info, error) {
	me, _, err := a.gl.Users.CurrentUser(gitlab.WithContext(ctx))
	if err != nil {
		return provider.Info{}, classify("get current user", err)
	}

	info := provider.Info{
		Provider:        "gitlab",
		Instance:        a.instance,
		AuthenticatedAs: me.Username,
		IsAdmin:         me.IsAdmin,
	}

	meta, _, err := a.gl.Metadata.GetMetadata(gitlab.WithContext(ctx))
	if err == nil && meta != nil {
		info.ServerVersion = meta.Version
	}
	return info, nil
}
