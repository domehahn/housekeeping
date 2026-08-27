package gitlab

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/provider"
)

// ListGroupMembers lists the direct members of every group in scope
// (scope.GroupIDs), de-duplicated by user ID.
//
// Deliberate simplification: GitLab's "list group members" endpoint
// (GET /groups/:id/members) returns only *direct* members - not members
// inherited from a parent group - which is exactly the guarantee a later
// RemoveGroupMember action needs (see docs/architecture.md "GitLab
// membership semantics"). If a user is a direct member of more than one
// group within a recursive scope, only the first membership encountered
// (root group first, then subgroups in the order ResolveGroupScope
// returned them) is kept; that occurrence's GroupID is what any
// remove-from-group action will target. This is documented in the README
// limitations section.
//
// Activity/login fields (last_sign_in_at, last_activity_on) are not part
// of the members API response at all. When the authenticated token
// belongs to an instance administrator, this method makes one additional
// GET /users/:id call per member (bounded by Adapter.workers) to fill
// them in. For non-admin tokens they remain domain.Unknown, per GitLab's
// documented restriction that these fields are admin-only.
func (a *Adapter) ListGroupMembers(ctx context.Context, scope domain.Scope) ([]domain.User, error) {
	groupIDs := scope.GroupIDs
	if len(groupIDs) == 0 {
		groupIDs = []string{scope.ID}
	}

	seen := map[string]bool{}
	var members []domain.User

	for _, gidStr := range groupIDs {
		gid, err := strconv.ParseInt(gidStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("gitlab: invalid group ID %q: %w", gidStr, err)
		}

		page := 1
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			opts := &gitlab.ListGroupMembersOptions{
				ListOptions: gitlab.ListOptions{Page: int64(page), PerPage: 100},
			}
			raw, resp, err := a.gl.Groups.ListGroupMembers(gid, opts, gitlab.WithContext(ctx))
			if err != nil {
				return nil, classify(fmt.Sprintf("list members of group %d", gid), err)
			}
			for _, m := range raw {
				u := mapGroupMember(m, gidStr)
				if seen[u.ID] {
					continue
				}
				seen[u.ID] = true
				members = append(members, u)
			}
			if resp == nil || resp.NextPage == 0 {
				break
			}
			page = int(resp.NextPage)
		}
	}

	isAdmin, err := a.isCurrentUserAdmin(ctx)
	if err != nil {
		// Non-fatal: proceed with unknown activity data rather than
		// failing discovery entirely (a permission error must never be
		// mistaken for "no users").
		return members, nil
	}
	if !isAdmin {
		return members, nil
	}

	a.enrichActivity(ctx, members)
	return members, nil
}

// enrichActivity fills in LastLoginAt/LastActivityAt for each member using
// bounded concurrency. Per-user failures are tolerated (that user's fields
// simply stay Unknown) so one bad lookup cannot abort the whole run.
func (a *Adapter) enrichActivity(ctx context.Context, members []domain.User) {
	sem := make(chan struct{}, a.workers)
	var wg sync.WaitGroup

	for i := range members {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			if ctx.Err() != nil {
				return
			}
			id, err := strconv.ParseInt(members[idx].ID, 10, 64)
			if err != nil {
				return
			}
			full, _, err := a.gl.Users.GetUser(id, gitlab.GetUsersOptions{}, gitlab.WithContext(ctx))
			if err != nil {
				return
			}
			members[idx] = enrichWithActivity(members[idx], full)
		}(i)
	}
	wg.Wait()
}

func (a *Adapter) isCurrentUserAdmin(ctx context.Context) (bool, error) {
	me, _, err := a.gl.Users.CurrentUser(gitlab.WithContext(ctx))
	if err != nil {
		return false, classify("get current user", err)
	}
	return me.IsAdmin, nil
}

// RemoveGroupMember removes a direct member from the given group. It
// refuses to run against a membership this adapter did not itself
// establish as direct (callers are expected to only pass a groupID that
// came from a User.GroupID populated by ListGroupMembers).
func (a *Adapter) RemoveGroupMember(ctx context.Context, groupID string, userID string) error {
	gid, err := strconv.ParseInt(groupID, 10, 64)
	if err != nil {
		return fmt.Errorf("gitlab: invalid group ID %q: %w", groupID, err)
	}
	uid, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return fmt.Errorf("gitlab: invalid user ID %q: %w", userID, err)
	}
	_, err = a.gl.GroupMembers.RemoveGroupMember(gid, uid, &gitlab.RemoveGroupMemberOptions{}, gitlab.WithContext(ctx))
	if err != nil {
		return classify(fmt.Sprintf("remove member %s from group %s", userID, groupID), err)
	}
	return nil
}

// GetGroupMember fetches the direct membership that a remove-from-group
// action targets. The direct endpoint deliberately does not resolve inherited
// membership, because inherited access cannot safely be removed here.
func (a *Adapter) GetGroupMember(ctx context.Context, groupID string, userID string) (domain.User, error) {
	gid, err := strconv.ParseInt(groupID, 10, 64)
	if err != nil {
		return domain.User{}, fmt.Errorf("gitlab: invalid group ID %q: %w", groupID, err)
	}
	uid, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return domain.User{}, fmt.Errorf("gitlab: invalid user ID %q: %w", userID, err)
	}
	member, _, err := a.gl.GroupMembers.GetGroupMember(gid, uid, gitlab.WithContext(ctx))
	if err != nil {
		return domain.User{}, classify(fmt.Sprintf("get member %s of group %s", userID, groupID), err)
	}
	return mapGroupMember(member, groupID), nil
}

// BlockUser blocks a user account instance-wide. Requires administrator
// rights; a non-admin token receives a provider.KindAuthorization error.
func (a *Adapter) BlockUser(ctx context.Context, userID string) error {
	uid, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return fmt.Errorf("gitlab: invalid user ID %q: %w", userID, err)
	}
	if err := a.gl.Users.BlockUser(uid, gitlab.WithContext(ctx)); err != nil {
		return classify("block user "+userID, err)
	}
	return nil
}

// CurrentUser identifies the authenticated caller.
func (a *Adapter) CurrentUser(ctx context.Context) (domain.User, error) {
	me, _, err := a.gl.Users.CurrentUser(gitlab.WithContext(ctx))
	if err != nil {
		return domain.User{}, classify("get current user", err)
	}
	return mapSelfUser(me), nil
}

// GetUser fetches the current state of a single user, primarily used to
// revalidate a planned action immediately before executing it. Activity
// fields are only populated when the token belongs to an instance
// administrator; otherwise they remain domain.Unknown.
func (a *Adapter) GetUser(ctx context.Context, userID string) (domain.User, error) {
	uid, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return domain.User{}, fmt.Errorf("gitlab: invalid user ID %q: %w", userID, err)
	}
	full, _, err := a.gl.Users.GetUser(uid, gitlab.GetUsersOptions{}, gitlab.WithContext(ctx))
	if err != nil {
		return domain.User{}, classify("get user "+userID, err)
	}
	return mapOtherUser(full), nil
}

// ListBillableGroupMembers returns the set of member IDs that GitLab itself
// counts as billable for the given group. This endpoint only works against
// a top-level group (not a subgroup) and requires the caller to hold the
// Owner role there - see
// https://docs.gitlab.com/api/members/#list-all-billable-members-of-a-group.
// It deliberately does not attempt to approximate billability from access
// level itself, since GitLab's actual rule (e.g. whether Guest counts as
// billable) depends on the group's subscription tier, which this adapter
// has no reliable way to determine independently.
func (a *Adapter) ListBillableGroupMembers(ctx context.Context, groupID string) (map[string]bool, error) {
	gid, err := strconv.ParseInt(groupID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("gitlab: invalid group ID %q: %w", groupID, err)
	}

	billable := map[string]bool{}
	page := 1
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		opts := &gitlab.ListBillableGroupMembersOptions{
			ListOptions: gitlab.ListOptions{Page: int64(page), PerPage: 100},
		}
		members, resp, err := a.gl.Groups.ListBillableGroupMembers(gid, opts, gitlab.WithContext(ctx))
		if err != nil {
			return nil, classify(fmt.Sprintf("list billable members of group %d", gid), err)
		}
		for _, m := range members {
			billable[strconv.FormatInt(m.ID, 10)] = true
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		page = int(resp.NextPage)
	}
	return billable, nil
}

// ListUserMemberships lists every group and project a user belongs to
// across the whole instance. Requires the token to belong to an instance
// administrator - see
// https://docs.gitlab.com/api/users/#list-projects-and-groups-that-a-user-is-a-member-of.
// Only used to support the opt-in billable-seat override; never called as
// part of ordinary discovery.
func (a *Adapter) ListUserMemberships(ctx context.Context, userID string) ([]domain.Membership, error) {
	uid, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("gitlab: invalid user ID %q: %w", userID, err)
	}

	var memberships []domain.Membership
	page := 1
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		opts := &gitlab.GetUserMembershipOptions{
			ListOptions: gitlab.ListOptions{Page: int64(page), PerPage: 100},
			Type:        gitlab.Ptr("Namespace"), // groups only, not individual projects
		}
		raw, resp, err := a.gl.Users.GetUserMemberships(uid, opts, gitlab.WithContext(ctx))
		if err != nil {
			return nil, classify(fmt.Sprintf("list memberships of user %s", userID), err)
		}
		for _, m := range raw {
			memberships = append(memberships, mapUserMembership(m))
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		page = int(resp.NextPage)
	}
	return memberships, nil
}

var _ provider.Client = (*Adapter)(nil)
