package gitlab

import (
	"context"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/domehahn/housekeeping/internal/domain"
)

// ResolveGroupScope resolves a group path to a domain.Scope and, when
// recursive is true, walks all descendant subgroups (handling pagination
// at every level) to build the full set of groups the scope covers.
//
// GitLab semantics used here: GET /groups/:id/subgroups only returns the
// direct children of :id, so recursion is performed by this adapter one
// level at a time rather than relying on a single "give me everything"
// endpoint - GitLab does not offer one for subgroups.
func (a *Adapter) ResolveGroupScope(ctx context.Context, path string, recursive bool) (domain.Scope, []domain.Group, error) {
	root, _, err := a.gl.Groups.GetGroup(path, &gitlab.GetGroupOptions{}, gitlab.WithContext(ctx))
	if err != nil {
		return domain.Scope{}, nil, classify("resolve group "+path, err)
	}
	rootGroup := mapGroup(root)

	scope := domain.Scope{
		Type:      domain.ScopeTypeGroup,
		ID:        rootGroup.ID,
		Path:      rootGroup.FullPath,
		Recursive: recursive,
		GroupIDs:  []string{rootGroup.ID},
	}

	groups := []domain.Group{rootGroup}
	if !recursive {
		return scope, groups, nil
	}

	descendants, err := a.listAllSubgroups(ctx, root.ID)
	if err != nil {
		return domain.Scope{}, nil, err
	}
	groups = append(groups, descendants...)
	for _, g := range descendants {
		scope.GroupIDs = append(scope.GroupIDs, g.ID)
	}
	return scope, groups, nil
}

// listAllSubgroups performs a breadth-first walk of the subgroup tree
// rooted at gid, paginating fully at every level and de-duplicating by ID
// in case GitLab ever reports a group more than once.
func (a *Adapter) listAllSubgroups(ctx context.Context, gid int64) ([]domain.Group, error) {
	seen := map[int64]bool{gid: true}
	queue := []int64{gid}
	var result []domain.Group

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		page := 1
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			opts := &gitlab.ListSubGroupsOptions{
				ListOptions: gitlab.ListOptions{Page: int64(page), PerPage: 100},
			}
			subgroups, resp, err := a.gl.Groups.ListSubGroups(current, opts, gitlab.WithContext(ctx))
			if err != nil {
				return nil, classify(fmt.Sprintf("list subgroups of group %d", current), err)
			}
			for _, sg := range subgroups {
				if seen[sg.ID] {
					continue
				}
				seen[sg.ID] = true
				result = append(result, mapGroup(sg))
				queue = append(queue, sg.ID)
			}
			if resp == nil || resp.NextPage == 0 {
				break
			}
			page = int(resp.NextPage)
		}
	}
	return result, nil
}
