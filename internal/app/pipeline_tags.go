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
// project discovered in a scope against a desired CI tag.
type PipelineTagEvaluationSummary struct {
	Scope   domain.Scope
	Tags    []string
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
		}
	}
	return reasons
}
