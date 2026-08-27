package gitlab

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go"

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
// opens a Merge Request back to the default branch. It never commits
// directly to the default branch - see
// docs/adr/0005-ci-tag-management-scope.md. Returns the created MR's web
// URL.
//
// If the branch already exists (a 409 from CreateBranch), that is
// classified as provider.KindConflict rather than treated as a hard
// failure - it almost always means a previous run already proposed this
// exact change, and the executor treats a conflict here as
// "already done."
func (a *Adapter) ProposePipelineTagChange(ctx context.Context, projectID string, patchedContent []byte, tag string) (string, error) {
	proj, _, err := a.gl.Projects.GetProject(projectID, &gitlab.GetProjectOptions{}, gitlab.WithContext(ctx))
	if err != nil {
		return "", classify("get project "+projectID, err)
	}
	if proj.DefaultBranch == "" {
		return "", fmt.Errorf("gitlab: project %s has no default branch (empty repository)", projectID)
	}

	branchName := "scm-cleaner/add-tag-" + slugifyTag(tag)
	_, _, err = a.gl.Branches.CreateBranch(projectID, &gitlab.CreateBranchOptions{
		Branch: &branchName,
		Ref:    &proj.DefaultBranch,
	}, gitlab.WithContext(ctx))
	if err != nil {
		return "", classify(fmt.Sprintf("create branch %s on project %s", branchName, projectID), err)
	}

	content := string(patchedContent)
	commitMsg := fmt.Sprintf("scm-cleaner: add CI tag %q to .gitlab-ci.yml", tag)
	_, _, err = a.gl.RepositoryFiles.UpdateFile(projectID, gitlabCIFilePath, &gitlab.UpdateFileOptions{
		Branch:        &branchName,
		Content:       &content,
		CommitMessage: &commitMsg,
	}, gitlab.WithContext(ctx))
	if err != nil {
		return "", classify(fmt.Sprintf("update %s on branch %s of project %s", gitlabCIFilePath, branchName, projectID), err)
	}

	title := fmt.Sprintf("scm-cleaner: add CI tag %q", tag)
	description := fmt.Sprintf(
		"This Merge Request was proposed by scm-cleaner.\n\n"+
			"It adds the CI tag `%s` to `default.tags` in `%s`, and to any job "+
			"that already defines its own `tags:` list. Jobs with no `tags:` of "+
			"their own are left untouched - they already inherit from `default:`.\n\n"+
			"scm-cleaner never merges this automatically - please review the diff "+
			"before merging.", tag, gitlabCIFilePath)
	mr, _, err := a.gl.MergeRequests.CreateMergeRequest(projectID, &gitlab.CreateMergeRequestOptions{
		Title:              &title,
		Description:        &description,
		SourceBranch:       &branchName,
		TargetBranch:       &proj.DefaultBranch,
		RemoveSourceBranch: gitlab.Ptr(true),
	}, gitlab.WithContext(ctx))
	if err != nil {
		return "", classify(fmt.Sprintf("create merge request from %s on project %s", branchName, projectID), err)
	}
	return mr.WebURL, nil
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
