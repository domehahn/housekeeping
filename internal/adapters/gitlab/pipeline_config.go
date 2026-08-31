package gitlab

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/provider"
)

const gitlabCIFilePath = ".gitlab-ci.yml"

// GetPipelineConfig fetches a project's .gitlab-ci.yml at its default
// branch. A project with no CI file is a normal case (exists=false, no
// error), not a failure - most repositories in a large group won't have
// one, and that must never be conflated with a real fetch error.
func (a *Adapter) GetPipelineConfig(ctx context.Context, projectID string) ([]byte, bool, error) {
	proj, _, err := a.gl.Projects.GetProject(projectID, &gitlab.GetProjectOptions{}, gitlab.WithContext(ctx))
	if err != nil {
		return nil, false, classify("get project "+projectID, err)
	}
	if proj.DefaultBranch == "" {
		// Empty repository - there is no ref to read a file from yet.
		return nil, false, nil
	}

	content, _, err := a.gl.RepositoryFiles.GetRawFile(projectID, gitlabCIFilePath, &gitlab.GetRawFileOptions{Ref: &proj.DefaultBranch}, gitlab.WithContext(ctx))
	if err != nil {
		wrapped := classify("get "+gitlabCIFilePath+" for project "+projectID, err)
		var pErr *provider.Error
		if errors.As(wrapped, &pErr) && pErr.Kind == provider.KindNotFound {
			return nil, false, nil
		}
		return nil, false, wrapped
	}
	return content, true, nil
}

// ProposePipelineTagChange opens a branch off the project's default
// branch, commits patchedContent as the new .gitlab-ci.yml on it, and
// opens or reuses a Merge Request back to the default branch. The branch name
// includes a content digest, and every step is safe to retry after a partial
// failure. It never commits directly to the default branch - see
// docs/adr/0005-ci-tag-management-scope.md. Returns the created MR's web
// URL.
func (a *Adapter) ProposePipelineTagChange(ctx context.Context, projectID string, patchedContent []byte, tags []string) (string, error) {
	proj, _, err := a.gl.Projects.GetProject(projectID, &gitlab.GetProjectOptions{}, gitlab.WithContext(ctx))
	if err != nil {
		return "", classify("get project "+projectID, err)
	}
	if proj.DefaultBranch == "" {
		return "", fmt.Errorf("gitlab: project %s has no default branch (empty repository)", projectID)
	}

	branchName := proposalBranchName(tags, patchedContent)
	if err := a.ensureProposalBranch(ctx, projectID, branchName, proj.DefaultBranch); err != nil {
		return "", err
	}

	commitMsg := fmt.Sprintf("scm-cleaner: add CI tags %s to .gitlab-ci.yml", strings.Join(canonicalTags(tags), ", "))
	if err := a.ensureProposalContent(ctx, projectID, branchName, patchedContent, commitMsg); err != nil {
		return "", err
	}
	if url, ok, err := a.findOpenProposal(ctx, projectID, branchName, proj.DefaultBranch); err != nil || ok {
		return url, err
	}

	tagList := strings.Join(canonicalTags(tags), ", ")
	title := fmt.Sprintf("scm-cleaner: add CI tags %s", tagList)
	description := fmt.Sprintf(
		"This Merge Request was proposed by scm-cleaner.\n\n"+
			"It adds the CI tags `%s` to `default.tags` in `%s`, and to any job "+
			"that already defines its own `tags:` list. Jobs with no `tags:` of "+
			"their own are left untouched - they already inherit from `default:`.\n\n"+
			"scm-cleaner never merges this automatically - please review the diff "+
			"before merging.\n\n%s", tagList, gitlabCIFilePath, proposalTagMarker(tags))
	mr, _, err := a.gl.MergeRequests.CreateMergeRequest(projectID, &gitlab.CreateMergeRequestOptions{
		Title:              &title,
		Description:        &description,
		SourceBranch:       &branchName,
		TargetBranch:       &proj.DefaultBranch,
		RemoveSourceBranch: gitlab.Ptr(true),
	}, gitlab.WithContext(ctx))
	if err != nil {
		classified := classify(fmt.Sprintf("create merge request from %s on project %s", branchName, projectID), err)
		var pErr *provider.Error
		if errors.As(classified, &pErr) && pErr.Kind == provider.KindConflict {
			if url, ok, findErr := a.findOpenProposal(ctx, projectID, branchName, proj.DefaultBranch); findErr == nil && ok {
				return url, nil
			}
		}
		return "", classified
	}
	return mr.WebURL, nil
}

// ProposePipelineTagRename opens a branch off the project's default branch,
// commits patchedContent (the result of ciyaml.ReplaceTags) as the new
// .gitlab-ci.yml on it, and opens or reuses a Merge Request back to the
// default branch. Before doing so, it best-effort closes any still-open
// scm-cleaner proposal that proposed one of the old tags - see
// closeSupersededProposals. It never commits directly to the default
// branch - see docs/adr/0005-ci-tag-management-scope.md.
func (a *Adapter) ProposePipelineTagRename(ctx context.Context, projectID string, patchedContent []byte, renames []domain.TagRename) (string, []string, error) {
	proj, _, err := a.gl.Projects.GetProject(projectID, &gitlab.GetProjectOptions{}, gitlab.WithContext(ctx))
	if err != nil {
		return "", nil, classify("get project "+projectID, err)
	}
	if proj.DefaultBranch == "" {
		return "", nil, fmt.Errorf("gitlab: project %s has no default branch (empty repository)", projectID)
	}

	branchName := renameBranchName(renames, patchedContent)
	if err := a.ensureProposalBranch(ctx, projectID, branchName, proj.DefaultBranch); err != nil {
		return "", nil, err
	}

	commitMsg := fmt.Sprintf("scm-cleaner: rename CI tag(s) %s in .gitlab-ci.yml", renameSummary(renames))
	if err := a.ensureProposalContent(ctx, projectID, branchName, patchedContent, commitMsg); err != nil {
		return "", nil, err
	}

	closedURLs := a.closeSupersededProposals(ctx, projectID, renames)

	if url, ok, err := a.findOpenProposal(ctx, projectID, branchName, proj.DefaultBranch); err != nil || ok {
		return url, closedURLs, err
	}

	summary := renameSummary(renames)
	title := fmt.Sprintf("scm-cleaner: rename CI tag(s) %s", summary)
	description := fmt.Sprintf(
		"This Merge Request was proposed by scm-cleaner.\n\n"+
			"It corrects the CI tag(s) %s in `default.tags` and any job that "+
			"already had the old tag in its own `tags:` list, in `%s`. Only "+
			"locations that actually had the old tag are touched.\n\n"+
			"scm-cleaner never merges this automatically - please review the diff "+
			"before merging.\n\n%s", summary, gitlabCIFilePath, renameProposalMarker(renames))
	mr, _, err := a.gl.MergeRequests.CreateMergeRequest(projectID, &gitlab.CreateMergeRequestOptions{
		Title:              &title,
		Description:        &description,
		SourceBranch:       &branchName,
		TargetBranch:       &proj.DefaultBranch,
		RemoveSourceBranch: gitlab.Ptr(true),
	}, gitlab.WithContext(ctx))
	if err != nil {
		classified := classify(fmt.Sprintf("create merge request from %s on project %s", branchName, projectID), err)
		var pErr *provider.Error
		if errors.As(classified, &pErr) && pErr.Kind == provider.KindConflict {
			if url, ok, findErr := a.findOpenProposal(ctx, projectID, branchName, proj.DefaultBranch); findErr == nil && ok {
				return url, closedURLs, nil
			}
		}
		return "", closedURLs, classified
	}
	return mr.WebURL, closedURLs, nil
}

// closeSupersededProposals looks up, for every rename's old tag, any
// still-open scm-cleaner proposal that proposed exactly that old tag
// (via ListPipelineTagProposals' existing cryptographic tag-set marker
// match, so only scm-cleaner's own matching proposals are ever touched)
// and closes it. This is entirely best-effort: a lookup or close failure
// is silently skipped rather than propagated, since failing to tidy up an
// old, now-superseded proposal must never block or fail the actual
// rename. Callers surface only what was actually closed.
func (a *Adapter) closeSupersededProposals(ctx context.Context, projectID string, renames []domain.TagRename) []string {
	var closedURLs []string
	seen := make(map[string]bool)
	for _, r := range renames {
		proposals, err := a.ListPipelineTagProposals(ctx, projectID, []string{r.Old})
		if err != nil {
			continue
		}
		for _, p := range proposals {
			if p.State != "opened" || p.IID == 0 || seen[p.URL] {
				continue
			}
			if _, _, closeErr := a.gl.MergeRequests.UpdateMergeRequest(projectID, p.IID, &gitlab.UpdateMergeRequestOptions{
				StateEvent: gitlab.Ptr("close"),
			}, gitlab.WithContext(ctx)); closeErr != nil {
				continue
			}
			seen[p.URL] = true
			closedURLs = append(closedURLs, p.URL)
		}
	}
	return closedURLs
}

func renameBranchName(renames []domain.TagRename, content []byte) string {
	sum := sha256.Sum256(content)
	slug := slugifyTag(renameSlugSource(renames))
	if len(slug) > 40 {
		slug = slug[:40]
	}
	return fmt.Sprintf("scm-cleaner/rename-tag-%s-%x", slug, sum[:6])
}

func renameSlugSource(renames []domain.TagRename) string {
	parts := make([]string, 0, len(renames))
	for _, r := range canonicalRenames(renames) {
		parts = append(parts, r.Old+"-to-"+r.New)
	}
	return strings.Join(parts, "-")
}

func canonicalRenames(renames []domain.TagRename) []domain.TagRename {
	out := append([]domain.TagRename{}, renames...)
	slices.SortFunc(out, func(a, b domain.TagRename) int {
		if a.Old != b.Old {
			return strings.Compare(a.Old, b.Old)
		}
		return strings.Compare(a.New, b.New)
	})
	return out
}

func renameProposalMarker(renames []domain.TagRename) string {
	var sb strings.Builder
	for _, r := range canonicalRenames(renames) {
		sb.WriteString(r.Old)
		sb.WriteString("\x00")
		sb.WriteString(r.New)
		sb.WriteString("\x01")
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return fmt.Sprintf("<!-- scm-cleaner-tag-renames-sha256: %x -->", sum)
}

func renameSummary(renames []domain.TagRename) string {
	parts := make([]string, 0, len(renames))
	for _, r := range canonicalRenames(renames) {
		parts = append(parts, fmt.Sprintf("%s -> %s", r.Old, r.New))
	}
	return strings.Join(parts, ", ")
}

func proposalBranchName(tags []string, content []byte) string {
	sum := sha256.Sum256(content)
	slug := slugifyTag(strings.Join(canonicalTags(tags), "-"))
	if len(slug) > 40 {
		slug = slug[:40]
	}
	return fmt.Sprintf("scm-cleaner/add-tag-%s-%x", slug, sum[:6])
}

func proposalBranchPrefix(tags []string) string {
	slug := slugifyTag(strings.Join(canonicalTags(tags), "-"))
	if len(slug) > 40 {
		slug = slug[:40]
	}
	return "scm-cleaner/add-tag-" + slug + "-"
}

func canonicalTags(tags []string) []string {
	canonical := append([]string{}, tags...)
	slices.Sort(canonical)
	return slices.Compact(canonical)
}

func proposalTagMarker(tags []string) string {
	sum := sha256.Sum256([]byte(strings.Join(canonicalTags(tags), "\x00")))
	return fmt.Sprintf("<!-- scm-cleaner-tags-sha256: %x -->", sum)
}

func (a *Adapter) ensureProposalBranch(ctx context.Context, projectID, branchName, defaultBranch string) error {
	_, _, err := a.gl.Branches.GetBranch(projectID, branchName, gitlab.WithContext(ctx))
	if err == nil {
		return nil
	}
	classified := classify("get proposal branch "+branchName, err)
	var pErr *provider.Error
	if !errors.As(classified, &pErr) || pErr.Kind != provider.KindNotFound {
		return classified
	}
	_, _, err = a.gl.Branches.CreateBranch(projectID, &gitlab.CreateBranchOptions{Branch: &branchName, Ref: &defaultBranch}, gitlab.WithContext(ctx))
	if err != nil {
		classified = classify(fmt.Sprintf("create branch %s on project %s", branchName, projectID), err)
		// A concurrent/retried invocation may create the deterministic branch
		// between our GET and POST. Its content is verified in the next step.
		if errors.As(classified, &pErr) && pErr.Kind == provider.KindConflict {
			return nil
		}
		return classified
	}
	return nil
}

func (a *Adapter) ensureProposalContent(ctx context.Context, projectID, branchName string, patchedContent []byte, commitMsg string) error {
	current, _, err := a.gl.RepositoryFiles.GetRawFile(projectID, gitlabCIFilePath, &gitlab.GetRawFileOptions{Ref: &branchName}, gitlab.WithContext(ctx))
	if err != nil {
		return classify(fmt.Sprintf("read %s on branch %s", gitlabCIFilePath, branchName), err)
	}
	if bytes.Equal(current, patchedContent) {
		return nil
	}
	content := string(patchedContent)
	_, _, err = a.gl.RepositoryFiles.UpdateFile(projectID, gitlabCIFilePath, &gitlab.UpdateFileOptions{
		Branch: &branchName, Content: &content, CommitMessage: &commitMsg,
	}, gitlab.WithContext(ctx))
	if err != nil {
		return classify(fmt.Sprintf("update %s on branch %s of project %s", gitlabCIFilePath, branchName, projectID), err)
	}
	return nil
}

// GetMergedPipelineConfig returns GitLab's read-only, include-expanded CI
// configuration at the default branch.
func (a *Adapter) GetMergedPipelineConfig(ctx context.Context, projectID string) ([]byte, []domain.PipelineInclude, error) {
	includeJobs := true
	result, _, err := a.gl.Validate.ProjectLint(projectID, &gitlab.ProjectLintOptions{IncludeJobs: &includeJobs}, gitlab.WithContext(ctx))
	if err != nil {
		return nil, nil, classify("get merged CI configuration for project "+projectID, err)
	}
	if !result.Valid {
		return nil, nil, provider.NewError(provider.KindValidation, "get merged CI configuration", strings.Join(result.Errors, "; "), nil)
	}
	includes := make([]domain.PipelineInclude, 0, len(result.Includes))
	for _, include := range result.Includes {
		includes = append(includes, domain.PipelineInclude{
			Type: include.Type, Location: include.Location, ContextProject: include.ContextProject, ContextSHA: include.ContextSHA,
		})
	}
	return []byte(result.MergedYaml), includes, nil
}

// ListPipelineTagProposals lists every scm-cleaner proposal matching the
// deterministic branch prefix for the requested tag set.
func (a *Adapter) ListPipelineTagProposals(ctx context.Context, projectID string, tags []string) ([]domain.PipelineProposal, error) {
	state, search := "all", "scm-cleaner"
	orderBy, sortOrder := "updated_at", "desc"
	prefix := proposalBranchPrefix(tags)
	marker := proposalTagMarker(tags)
	page := int64(1)
	var proposals []domain.PipelineProposal
	for {
		mrs, resp, err := a.gl.MergeRequests.ListProjectMergeRequests(projectID, &gitlab.ListProjectMergeRequestsOptions{
			State: &state, Search: &search, OrderBy: &orderBy, Sort: &sortOrder,
			ListOptions: gitlab.ListOptions{Page: page, PerPage: 100},
		}, gitlab.WithContext(ctx))
		if err != nil {
			return nil, classify("list pipeline-tag merge requests for project "+projectID, err)
		}
		for _, mr := range mrs {
			legacySingleTagTitle := len(tags) == 1 && mr.Title == fmt.Sprintf("scm-cleaner: add CI tag %q", tags[0])
			if strings.HasPrefix(mr.SourceBranch, prefix) && (strings.Contains(mr.Description, marker) || legacySingleTagTitle) {
				proposals = append(proposals, domain.PipelineProposal{
					ProjectID: projectID, IID: mr.IID, Title: mr.Title, State: mr.State, SourceBranch: mr.SourceBranch, URL: mr.WebURL,
				})
			}
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		page = resp.NextPage
	}
	return proposals, nil
}

func (a *Adapter) findOpenProposal(ctx context.Context, projectID, sourceBranch, targetBranch string) (string, bool, error) {
	state := "opened"
	mrs, _, err := a.gl.MergeRequests.ListProjectMergeRequests(projectID, &gitlab.ListProjectMergeRequestsOptions{
		State: &state, SourceBranch: &sourceBranch, TargetBranch: &targetBranch,
		ListOptions: gitlab.ListOptions{PerPage: 1},
	}, gitlab.WithContext(ctx))
	if err != nil {
		return "", false, classify("find existing pipeline-tag merge request", err)
	}
	if len(mrs) == 0 {
		return "", false, nil
	}
	return mrs[0].WebURL, true, nil
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9-]+`)

// slugifyTag turns an arbitrary tag value into a safe git branch name
// component.
func slugifyTag(tag string) string {
	s := strings.ToLower(strings.TrimSpace(tag))
	s = nonSlugChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "tag"
	}
	return s
}
