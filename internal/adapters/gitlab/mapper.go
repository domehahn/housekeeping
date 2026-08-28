package gitlab

import (
	"strconv"
	"strings"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/domehahn/housekeeping/internal/domain"
)

func mapProject(p *gitlab.Project) domain.Project {
	proj := domain.Project{
		ID:            strconv.FormatInt(p.ID, 10),
		Name:          p.Name,
		Path:          p.Path,
		FullPath:      p.PathWithNamespace,
		Archived:      p.Archived,
		WebURL:        p.WebURL,
		DefaultBranch: p.DefaultBranch,
	}
	if p.CreatedAt != nil {
		proj.CreatedAt = *p.CreatedAt
	}
	if p.Namespace != nil {
		proj.Namespace = p.Namespace.FullPath
	}
	// last_activity_at is a standard field on the Projects API and is
	// always populated for projects the caller can see - see
	// https://docs.gitlab.com/api/projects/. Unlike user activity fields,
	// it does not require elevated permissions, so it is always Known.
	proj.LastActivityAt = domain.Known(p.LastActivityAt)
	return proj
}

func mapGroup(g *gitlab.Group) domain.Group {
	group := domain.Group{
		ID:       strconv.FormatInt(g.ID, 10),
		Name:     g.Name,
		Path:     g.Path,
		FullPath: g.FullPath,
	}
	if g.ParentID != 0 {
		parent := strconv.FormatInt(g.ParentID, 10)
		group.ParentID = &parent
	}
	return group
}

// mapGroupMember maps a direct group membership. Activity fields are left
// Unknown here because the group members API does not return them - they
// must be filled in separately via enrichWithActivity for callers with
// administrator access. See docs/architecture.md "GitLab API limitations".
func mapGroupMember(m *gitlab.GroupMember, groupID string) domain.User {
	return domain.User{
		ID:               strconv.FormatInt(m.ID, 10),
		Username:         m.Username,
		Name:             m.Name,
		State:            mapUserState(m.State),
		AccessLevel:      mapAccessLevel(m.AccessLevel),
		MembershipOrigin: domain.MembershipDirect,
		GroupID:          groupID,
		WebURL:           m.WebURL,
		LastLoginAt:      domain.Unknown(),
		LastActivityAt:   domain.Unknown(),
	}
}

// enrichWithActivity overlays admin-only activity/login fields obtained
// from a full GET /users/:id response onto an already-mapped User.
func enrichWithActivity(u domain.User, full *gitlab.User) domain.User {
	u.State = mapUserState(full.State)
	u.LastLoginAt = domain.Known(full.LastSignInAt)
	if full.LastActivityOn != nil {
		t := time.Time(*full.LastActivityOn)
		u.LastActivityAt = domain.Known(&t)
	} else {
		u.LastActivityAt = domain.Known(nil)
	}
	return u
}

// mapSelfUser maps the response of GET /user (the authenticated caller's
// own profile), which GitLab always populates with activity/login fields
// regardless of admin status. A nil LastActivityOn here is therefore a
// genuine "known: never active", not missing data.
func mapSelfUser(u *gitlab.User) domain.User {
	user := baseUserFields(u)
	user.LastLoginAt = domain.Known(u.LastSignInAt)
	if u.LastActivityOn != nil {
		t := time.Time(*u.LastActivityOn)
		user.LastActivityAt = domain.Known(&t)
	} else {
		user.LastActivityAt = domain.Known(nil)
	}
	return user
}

// mapOtherUser maps the response of GET /users/:id for a user other than
// the caller. Whether these fields are populated depends on whether the
// caller is an instance administrator (see docs.gitlab.com/api/users -
// "last_sign_in_at" and "last_activity_on" are admin-only). Since this
// mapper cannot itself tell "the caller is not admin" apart from "the
// caller is admin but the user genuinely never signed in", it conservatively
// treats an absent value as Unknown rather than "known: never" - the safe
// default required by docs/adr/0003-unknown-activity-safe-default.md.
func mapOtherUser(u *gitlab.User) domain.User {
	user := baseUserFields(u)
	if u.LastSignInAt != nil {
		user.LastLoginAt = domain.Known(u.LastSignInAt)
	} else {
		user.LastLoginAt = domain.Unknown()
	}
	if u.LastActivityOn != nil {
		t := time.Time(*u.LastActivityOn)
		user.LastActivityAt = domain.Known(&t)
	} else {
		user.LastActivityAt = domain.Unknown()
	}
	return user
}

func baseUserFields(u *gitlab.User) domain.User {
	return domain.User{
		ID:       strconv.FormatInt(u.ID, 10),
		Username: u.Username,
		Name:     u.Name,
		State:    mapUserState(u.State),
		WebURL:   u.WebURL,
	}
}

// mapRunner maps runner details plus explicit project assignments into a
// domain.Runner, splitting that list into in-scope/out-of-scope
// by comparing against inScopeProjectIDs (the set of project IDs the
// current evaluation actually covers) - this is what makes the
// assignments. assessRunnerImpact separately handles implicit group and
// instance reach, which cannot be inferred from Projects alone.
func mapRunner(d *gitlab.RunnerDetails, scope domain.Scope, inScopeProjectIDs map[string]bool) domain.Runner {
	r := domain.Runner{
		ID:          strconv.FormatInt(d.ID, 10),
		Description: d.Description,
		TagList:     append([]string{}, d.TagList...),
		Shared:      d.IsShared,
		RunnerType:  d.RunnerType,
	}
	for _, g := range d.Groups {
		r.OwnerGroupIDs = append(r.OwnerGroupIDs, strconv.FormatInt(g.ID, 10))
	}
	for _, p := range d.Projects {
		id := strconv.FormatInt(p.ID, 10)
		if inScopeProjectIDs[id] {
			r.InScopeProjectPaths = append(r.InScopeProjectPaths, p.PathWithNamespace)
		} else {
			r.OutOfScopeProjectPaths = append(r.OutOfScopeProjectPaths, p.PathWithNamespace)
		}
	}
	assessRunnerImpact(&r, scope)
	return r
}

func assessRunnerImpact(r *domain.Runner, scope domain.Scope) {
	switch r.RunnerType {
	case "project_type":
		if len(r.InScopeProjectPaths) == 0 {
			r.ImpactReason = "project runner is no longer assigned to any project in the evaluated scope"
			return
		}
		r.ImpactKnown = true // project assignments are explicit in RunnerDetails.Projects
	case "group_type":
		if !scope.Recursive {
			r.ImpactReason = "group runners also reach descendant subgroups; rerun against the owning group with --recursive"
			return
		}
		if len(r.OwnerGroupIDs) == 0 {
			r.ImpactReason = "runner owner group was not returned by GitLab"
			return
		}
		inScopeGroups := map[string]bool{}
		for _, id := range scope.GroupIDs {
			inScopeGroups[id] = true
		}
		for _, id := range r.OwnerGroupIDs {
			if !inScopeGroups[id] {
				r.ImpactReason = "runner is inherited from an ancestor group outside the evaluated scope"
				return
			}
		}
		r.ImpactKnown = true
	case "instance_type":
		r.ImpactReason = "instance runner impact is instance-wide and cannot be proven from a group-scoped query"
	default:
		r.ImpactReason = "unknown GitLab runner type " + strconv.Quote(r.RunnerType)
	}
}

func mapUserMembership(m *gitlab.UserMembership) domain.Membership {
	sourceType := domain.MembershipSourceProject
	if strings.EqualFold(m.SourceType, "Namespace") {
		sourceType = domain.MembershipSourceGroup
	}
	return domain.Membership{
		SourceType:  sourceType,
		SourceID:    strconv.FormatInt(m.SourceID, 10),
		SourceName:  m.SourceName,
		AccessLevel: mapAccessLevel(m.AccessLevel),
	}
}

func mapUserState(state string) domain.UserState {
	switch state {
	case "active":
		return domain.UserStateActive
	case "blocked", "ldap_blocked", "banned":
		return domain.UserStateBlocked
	case "deactivated":
		return domain.UserStateDeactivated
	default:
		return domain.UserStateUnknown
	}
}

func mapAccessLevel(level gitlab.AccessLevelValue) domain.AccessLevel {
	switch level {
	case gitlab.GuestPermissions:
		return domain.AccessLevelGuest
	case gitlab.ReporterPermissions:
		return domain.AccessLevelReporter
	case gitlab.DeveloperPermissions:
		return domain.AccessLevelDeveloper
	case gitlab.MaintainerPermissions:
		return domain.AccessLevelMaintainer
	case gitlab.OwnerPermissions:
		return domain.AccessLevelOwner
	default:
		return domain.AccessLevelUnknown
	}
}
