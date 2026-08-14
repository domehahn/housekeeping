package gitlab

import (
	"context"
	"fmt"
	"strconv"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/domehahn/housekeeping/internal/domain"
)

// ListProjects lists every project directly owned by the scope's group,
// and - when the scope is recursive - every project owned by its
// descendant subgroups too. Discovery of the descendant group list itself
// happens in ResolveGroupScope; this method assumes scope.ID (and, when
// recursive, the caller having already resolved subgroups) is valid.
//
// GitLab's "list group projects" endpoint accepts include_subgroups=true,
// which is used directly instead of iterating each subgroup separately -
// this is both fewer requests and avoids any risk of missing projects in a
// subgroup that was not otherwise enumerated.
func (a *Adapter) ListProjects(ctx context.Context, scope domain.Scope) ([]domain.Project, error) {
	gid, err := strconv.ParseInt(scope.ID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("gitlab: invalid scope ID %q: %w", scope.ID, err)
	}

	seen := map[int64]bool{}
	var result []domain.Project

	page := 1
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		opts := &gitlab.ListGroupProjectsOptions{
			ListOptions:      gitlab.ListOptions{Page: int64(page), PerPage: 100},
			IncludeSubGroups: gitlab.Ptr(scope.Recursive),
			WithShared:       gitlab.Ptr(false),
			Archived:         nil, // fetch both archived and non-archived; policies decide
		}
		projects, resp, err := a.gl.Groups.ListGroupProjects(gid, opts, gitlab.WithContext(ctx))
		if err != nil {
			return nil, classify(fmt.Sprintf("list projects of group %s", scope.Path), err)
		}
		for _, p := range projects {
			if seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			result = append(result, mapProject(p))
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		page = int(resp.NextPage)
	}
	return result, nil
}

// GetProject fetches the current state of a single project, primarily used
// to revalidate a planned action immediately before executing it.
func (a *Adapter) GetProject(ctx context.Context, projectID string) (domain.Project, error) {
	p, _, err := a.gl.Projects.GetProject(projectID, &gitlab.GetProjectOptions{}, gitlab.WithContext(ctx))
	if err != nil {
		return domain.Project{}, classify("get project "+projectID, err)
	}
	return mapProject(p), nil
}

// DeleteProject deletes a project by its stable numeric ID. GitLab.com and
// recent Self-Managed versions "mark for deletion" first (soft-delete with
// a retention period) unless the project's group has immediate deletion
// enabled; either way this call is the correct, documented API operation -
// this adapter does not attempt to bypass or detect that distinction, it
// simply issues the standard delete request.
func (a *Adapter) DeleteProject(ctx context.Context, projectID string) error {
	_, err := a.gl.Projects.DeleteProject(projectID, &gitlab.DeleteProjectOptions{}, gitlab.WithContext(ctx))
	return classify("delete project "+projectID, err)
}

// ArchiveProject archives a project by its stable numeric ID.
func (a *Adapter) ArchiveProject(ctx context.Context, projectID string) error {
	_, _, err := a.gl.Projects.ArchiveProject(projectID, gitlab.WithContext(ctx))
	return classify("archive project "+projectID, err)
}
