package app

import (
	"context"
	"fmt"
	"sync"

	"github.com/domehahn/housekeeping/internal/ciyaml"
	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/provider"
)

// PipelineTagStatus classifies the outcome of checking one project's
// .gitlab-ci.yml against a desired tag.
type PipelineTagStatus string

const (
	// PipelineTagMissing means the tag is absent from default.tags and/or
	// at least one job's own tags: list - applying the patch would change
	// the file.
	PipelineTagMissing PipelineTagStatus = "missing"
	// PipelineTagPresent means the tag is already everywhere it would be
	// added - applying the patch would be a no-op.
	PipelineTagPresent PipelineTagStatus = "present"
	// PipelineTagNoCIFile means the project has no .gitlab-ci.yml at its
	// default branch - not an error, just nothing to do.
	PipelineTagNoCIFile PipelineTagStatus = "no_ci_file"
	// PipelineTagFetchError means the file (or the project itself) could
	// not be retrieved - a permission or transient error, never treated
	// as "no CI file."
	PipelineTagFetchError PipelineTagStatus = "fetch_error"
	// PipelineTagParseError means the file exists but is not a valid
	// GitLab CI YAML document (see internal/ciyaml).
	PipelineTagParseError PipelineTagStatus = "parse_error"
)

// PipelineTagEvaluation pairs a discovered project with the outcome of
// checking its .gitlab-ci.yml for a desired CI tag.
type PipelineTagEvaluation struct {
	Project          domain.Project
	Status           PipelineTagStatus
	Protected        bool
	ProtectionReason string
	HasIncludes      bool
	Reasons          []string
}

// Matched reports whether this project should be included in a plan: the
// tag is missing somewhere it should be, and the project is not
// protected.
func (e PipelineTagEvaluation) Matched() bool {
	return e.Status == PipelineTagMissing && !e.Protected
}

// PipelineTagEvaluationSummary is the full result of evaluating every
// project discovered in a scope against a desired CI tag (or, when
// Renames is set instead of Tags, a set of tag corrections).
type PipelineTagEvaluationSummary struct {
	Scope   domain.Scope
	Tags    []string
	Renames []domain.TagRename
	Results []PipelineTagEvaluation
}

func (s PipelineTagEvaluationSummary) Discovered() int { return len(s.Results) }

func (s PipelineTagEvaluationSummary) Matched() []PipelineTagEvaluation {
	var out []PipelineTagEvaluation
	for _, r := range s.Results {
		if r.Matched() {
			out = append(out, r)
		}
	}
	return out
}

func (s PipelineTagEvaluationSummary) Protected() []PipelineTagEvaluation {
	var out []PipelineTagEvaluation
	for _, r := range s.Results {
		if r.Protected {
			out = append(out, r)
		}
	}
	return out
}

// EvaluatePipelineTags checks every project's .gitlab-ci.yml for tags,
// using bounded concurrency (workers) since a group can contain many
// projects and each check is a separate network round trip. Protection
// rules (the same domain.ProjectProtectionRule used for project
// inactivity cleanup) are honored here too - a protected project is never
// proposed for a CI tag change.
func EvaluatePipelineTags(
	ctx context.Context,
	reader provider.PipelineConfigProposer,
	projects []domain.Project,
	tags []string,
	protection domain.ProjectProtectionRule,
	workers int,
) PipelineTagEvaluationSummary {
	if workers <= 0 {
		workers = 5
	}
	results := make([]PipelineTagEvaluation, len(projects))

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, proj := range projects {
		wg.Add(1)
		go func(idx int, p domain.Project) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[idx] = PipelineTagEvaluation{Project: p, Status: PipelineTagFetchError, Reasons: []string{"cancelled"}}
				return
			}
			defer func() { <-sem }()
			results[idx] = evaluatePipelineTagsForProject(ctx, reader, p, tags, protection)
		}(i, proj)
	}
	wg.Wait()

	return PipelineTagEvaluationSummary{Tags: append([]string{}, tags...), Results: results}
}

func evaluatePipelineTagsForProject(
	ctx context.Context,
	reader provider.PipelineConfigProposer,
	proj domain.Project,
	tags []string,
	protection domain.ProjectProtectionRule,
) PipelineTagEvaluation {
	eval := PipelineTagEvaluation{Project: proj}

	if protection != nil {
		if protected, reason := protection.IsProtected(proj); protected {
			eval.Protected = true
			eval.ProtectionReason = reason
		}
	}

	content, exists, err := reader.GetPipelineConfig(ctx, proj.ID)
	if err != nil {
		eval.Status = PipelineTagFetchError
		eval.Reasons = []string{fmt.Sprintf("could not fetch .gitlab-ci.yml: %v", err)}
		return eval
	}
	if !exists {
		eval.Status = PipelineTagNoCIFile
		eval.Reasons = []string{"project has no .gitlab-ci.yml at its default branch"}
		return eval
	}

	eval.HasIncludes = ciyaml.HasIncludes(content)

	_, changes, err := ciyaml.AddTags(content, tags)
	if err != nil {
		eval.Status = PipelineTagParseError
		eval.Reasons = []string{fmt.Sprintf(".gitlab-ci.yml could not be parsed: %v", err)}
		return eval
	}

	if len(changes) == 0 {
		eval.Status = PipelineTagPresent
		eval.Reasons = []string{fmt.Sprintf("tags %q already present in default.tags and every job that defines its own tags", tags)}
		return eval
	}

	eval.Status = PipelineTagMissing
	eval.Reasons = describeChanges(changes)
	if eval.HasIncludes {
		eval.Reasons = append(eval.Reasons, "warning: this file has an include: - jobs defined only in included files are not covered")
	}
	return eval
}

// PipelineTagRenameReader is what EvaluatePipelineTagRename needs: reading
// .gitlab-ci.yml plus listing existing scm-cleaner proposals. The second
// capability matters because a project's .gitlab-ci.yml may already be
// fully correct (or never had the old tag merged in the first place)
// while a stale, still-open Merge Request from an earlier, wrong-tag run
// remains open - that project must still be reported as matched so
// execution closes the stale proposal, even though the file itself needs
// no change.
type PipelineTagRenameReader interface {
	provider.PipelineConfigProposer
	provider.PipelineProposalReporter
}

// EvaluatePipelineTagRename checks every project's .gitlab-ci.yml for the
// old tag(s) that need correcting, using the same bounded-concurrency and
// protection handling as EvaluatePipelineTags. Status PipelineTagMissing
// here means "at least one old tag is present and would be replaced, the
// corrected tag still needs adding, or a stale open proposal for an old
// tag needs closing"; PipelineTagPresent means none of that applies - the
// project is already fully correct with no leftover artifact.
func EvaluatePipelineTagRename(
	ctx context.Context,
	reader PipelineTagRenameReader,
	projects []domain.Project,
	renames []domain.TagRename,
	protection domain.ProjectProtectionRule,
	workers int,
) PipelineTagEvaluationSummary {
	if workers <= 0 {
		workers = 5
	}
	results := make([]PipelineTagEvaluation, len(projects))

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, proj := range projects {
		wg.Add(1)
		go func(idx int, p domain.Project) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[idx] = PipelineTagEvaluation{Project: p, Status: PipelineTagFetchError, Reasons: []string{"cancelled"}}
				return
			}
			defer func() { <-sem }()
			results[idx] = evaluatePipelineTagRenameForProject(ctx, reader, p, renames, protection)
		}(i, proj)
	}
	wg.Wait()

	return PipelineTagEvaluationSummary{Renames: append([]domain.TagRename{}, renames...), Results: results}
}

func evaluatePipelineTagRenameForProject(
	ctx context.Context,
	reader PipelineTagRenameReader,
	proj domain.Project,
	renames []domain.TagRename,
	protection domain.ProjectProtectionRule,
) PipelineTagEvaluation {
	eval := PipelineTagEvaluation{Project: proj}

	if protection != nil {
		if protected, reason := protection.IsProtected(proj); protected {
			eval.Protected = true
			eval.ProtectionReason = reason
		}
	}

	content, exists, err := reader.GetPipelineConfig(ctx, proj.ID)
	if err != nil {
		eval.Status = PipelineTagFetchError
		eval.Reasons = []string{fmt.Sprintf("could not fetch .gitlab-ci.yml: %v", err)}
		return eval
	}
	if !exists {
		eval.Status = PipelineTagNoCIFile
		eval.Reasons = []string{"project has no .gitlab-ci.yml at its default branch"}
		return eval
	}

	eval.HasIncludes = ciyaml.HasIncludes(content)

	_, changes, err := ciyaml.ReplaceTags(content, renames)
	if err != nil {
		eval.Status = PipelineTagParseError
		eval.Reasons = []string{fmt.Sprintf(".gitlab-ci.yml could not be parsed: %v", err)}
		return eval
	}

	if len(changes) > 0 {
		eval.Status = PipelineTagMissing
		eval.Reasons = describeChanges(changes)
		if eval.HasIncludes {
			eval.Reasons = append(eval.Reasons, "warning: this file has an include: - jobs defined only in included files are not covered")
		}
		return eval
	}

	// None of the old tags are present in the file. Two further cases can
	// still require action: the corrected tag was never added at all
	// (e.g. the original add-tag Merge Request for this project was
	// never merged), or the file is already fully correct but a stale,
	// still-open proposal for an old tag remains and needs closing.
	_, addChanges, addErr := ciyaml.AddTags(content, newTagsOf(renames))
	if addErr == nil && len(addChanges) > 0 {
		eval.Status = PipelineTagMissing
		eval.Reasons = describeChanges(addChanges)
		eval.Reasons = append(eval.Reasons, "the corrected tag was never added to this file (the original proposal may be unmerged or missing)")
		if eval.HasIncludes {
			eval.Reasons = append(eval.Reasons, "warning: this file has an include: - jobs defined only in included files are not covered")
		}
		return eval
	}

	if staleProposal, ok := findStaleOpenTagProposal(ctx, reader, proj.ID, oldTagsOf(renames)); ok {
		eval.Status = PipelineTagMissing
		eval.Reasons = []string{fmt.Sprintf("an open Merge Request (%s) still proposes an old tag and would be closed as part of this correction", staleProposal)}
		return eval
	}

	eval.Status = PipelineTagPresent
	eval.Reasons = []string{"none of the old tags are present in default.tags or any job's own tags, and no stale proposal exists"}
	return eval
}

// findStaleOpenTagProposal reports the URL of the first still-open
// scm-cleaner proposal found for any of oldTags. A lookup failure for one
// old tag is skipped rather than propagated - this is advisory
// information for evaluate/plan; the actual close attempt at execute time
// is what matters for correctness, and it is independently best-effort.
func findStaleOpenTagProposal(ctx context.Context, reader provider.PipelineProposalReporter, projectID string, oldTags []string) (string, bool) {
	for _, tag := range oldTags {
		proposals, err := reader.ListPipelineTagProposals(ctx, projectID, []string{tag})
		if err != nil {
			continue
		}
		for _, p := range proposals {
			if p.State == "opened" {
				return p.URL, true
			}
		}
	}
	return "", false
}

// newTagsOf and oldTagsOf extract the deduplicated New/Old halves of a set
// of tag renames, preserving first-seen order. Shared between evaluation
// and execution (internal/app/executor.go).
func newTagsOf(renames []domain.TagRename) []string { return tagRenameHalves(renames, false) }
func oldTagsOf(renames []domain.TagRename) []string { return tagRenameHalves(renames, true) }

func tagRenameHalves(renames []domain.TagRename, old bool) []string {
	seen := make(map[string]bool, len(renames))
	out := make([]string, 0, len(renames))
	for _, r := range renames {
		v := r.New
		if old {
			v = r.Old
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// PipelineConfigPresence pairs a project with whether it has a
// .gitlab-ci.yml, for `pipelines list` - no target tag involved.
type PipelineConfigPresence struct {
	Project domain.Project
	Exists  bool
	Err     error
}

// DiscoverPipelineConfigs checks every project for the mere presence of a
// .gitlab-ci.yml, using the same bounded concurrency as
// EvaluatePipelineTags.
func DiscoverPipelineConfigs(ctx context.Context, reader provider.PipelineConfigProposer, projects []domain.Project, workers int) []PipelineConfigPresence {
	if workers <= 0 {
		workers = 5
	}
	results := make([]PipelineConfigPresence, len(projects))

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, proj := range projects {
		wg.Add(1)
		go func(idx int, p domain.Project) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[idx] = PipelineConfigPresence{Project: p, Err: ctx.Err()}
				return
			}
			defer func() { <-sem }()
			_, exists, err := reader.GetPipelineConfig(ctx, p.ID)
			results[idx] = PipelineConfigPresence{Project: p, Exists: exists, Err: err}
		}(i, proj)
	}
	wg.Wait()
	return results
}

func describeChanges(changes []ciyaml.Change) []string {
	reasons := make([]string, 0, len(changes))
	for _, c := range changes {
		switch c.Kind {
		case ciyaml.ChangeDefaultCreated:
			reasons = append(reasons, fmt.Sprintf("tag %q missing from default.tags", c.Tag))
		case ciyaml.ChangeJobAppended:
			reasons = append(reasons, fmt.Sprintf("tag %q missing from job %q's own tags", c.Tag, c.Job))
		case ciyaml.ChangeDefaultReplaced:
			reasons = append(reasons, fmt.Sprintf("tag %q replaced with %q in default.tags", c.OldTag, c.Tag))
		case ciyaml.ChangeJobReplaced:
			reasons = append(reasons, fmt.Sprintf("tag %q replaced with %q in job %q's own tags", c.OldTag, c.Tag, c.Job))
		}
	}
	return reasons
}
