package gitlab

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/domehahn/housekeeping/internal/domain"
)

// ListRunnersForProjects lists every runner used by the given projects,
// de-duplicated by runner ID, each annotated with its full blast radius
// (every project using it, in-scope or not - see mapRunner). Per-project
// runner listing is fanned out with bounded concurrency (Adapter.workers),
// the same pattern used for admin activity enrichment in users.go.
func (a *Adapter) ListRunnersForProjects(ctx context.Context, projectIDs []string) ([]domain.Runner, error) {
	inScope := make(map[string]bool, len(projectIDs))
	for _, id := range projectIDs {
		inScope[id] = true
	}

	runnerIDs, err := a.collectRunnerIDs(ctx, projectIDs)
	if err != nil {
		return nil, err
	}

	runners := make([]domain.Runner, 0, len(runnerIDs))
	var mu sync.Mutex
	var firstErr error
	sem := make(chan struct{}, a.workers)
	var wg sync.WaitGroup

	for id := range runnerIDs {
		wg.Add(1)
		go func(runnerID int64) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			details, _, err := a.gl.Runners.GetRunnerDetails(runnerID, gitlab.WithContext(ctx))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = classify(fmt.Sprintf("get details of runner %d", runnerID), err)
				}
				return
			}
			runners = append(runners, mapRunner(details, inScope))
		}(id)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return runners, nil
}

// collectRunnerIDs lists the runners enabled for each project and returns
// the de-duplicated set of runner IDs across all of them.
func (a *Adapter) collectRunnerIDs(ctx context.Context, projectIDs []string) (map[int64]bool, error) {
	ids := map[int64]bool{}
	for _, pidStr := range projectIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page := 1
		for {
			opts := &gitlab.ListProjectRunnersOptions{
				ListOptions: gitlab.ListOptions{Page: int64(page), PerPage: 100},
			}
			runners, resp, err := a.gl.Runners.ListProjectRunners(pidStr, opts, gitlab.WithContext(ctx))
			if err != nil {
				return nil, classify("list runners of project "+pidStr, err)
			}
			for _, r := range runners {
				ids[r.ID] = true
			}
			if resp == nil || resp.NextPage == 0 {
				break
			}
			page = int(resp.NextPage)
		}
	}
	return ids, nil
}

// GetRunnerTags returns a runner's current tag list, used to revalidate
// immediately before UpdateRunnerTags.
func (a *Adapter) GetRunnerTags(ctx context.Context, runnerID string) ([]string, error) {
	rid, err := strconv.ParseInt(runnerID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("gitlab: invalid runner ID %q: %w", runnerID, err)
	}
	details, _, err := a.gl.Runners.GetRunnerDetails(rid, gitlab.WithContext(ctx))
	if err != nil {
		return nil, classify("get details of runner "+runnerID, err)
	}
	return details.TagList, nil
}

// UpdateRunnerTags replaces a runner's tag list wholesale - GitLab's API
// is not additive, so callers must pass the full desired list (typically
// the current list plus the new tag).
func (a *Adapter) UpdateRunnerTags(ctx context.Context, runnerID string, tags []string) error {
	rid, err := strconv.ParseInt(runnerID, 10, 64)
	if err != nil {
		return fmt.Errorf("gitlab: invalid runner ID %q: %w", runnerID, err)
	}
	_, _, err = a.gl.Runners.UpdateRunnerDetails(rid, &gitlab.UpdateRunnerDetailsOptions{
		TagList: &tags,
	}, gitlab.WithContext(ctx))
	if err != nil {
		return classify("update tags of runner "+runnerID, err)
	}
	return nil
}
