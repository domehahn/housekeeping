package app

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/domehahn/housekeeping/internal/ciyaml"
	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/provider"
)

// NormalizeTags trims, de-duplicates, and sorts tags so plans and proposal
// branches are deterministic regardless of CLI flag order.
func NormalizeTags(values []string) ([]string, error) {
	seen := make(map[string]bool, len(values))
	tags := make([]string, 0, len(values))
	for _, value := range values {
		tag := strings.TrimSpace(value)
		if tag == "" {
			return nil, fmt.Errorf("tags must not be empty")
		}
		if !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("at least one --tag is required")
	}
	slices.Sort(tags)
	return tags, nil
}

// FilterPipelineProjects applies full-path include/exclude regular expressions.
// Excludes always win. With no include patterns, every non-excluded project is
// selected.
func FilterPipelineProjects(projects []domain.Project, includes, excludes []string) ([]domain.Project, error) {
	includeRE, err := compileProjectPatterns("include", includes)
	if err != nil {
		return nil, err
	}
	excludeRE, err := compileProjectPatterns("exclude", excludes)
	if err != nil {
		return nil, err
	}
	selected := make([]domain.Project, 0, len(projects))
	for _, project := range projects {
		if matchesAny(excludeRE, project.FullPath) {
			continue
		}
		if len(includeRE) == 0 || matchesAny(includeRE, project.FullPath) {
			selected = append(selected, project)
		}
	}
	return selected, nil
}

func compileProjectPatterns(kind string, patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("compile %s-project pattern %q: %w", kind, pattern, err)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

func matchesAny(patterns []*regexp.Regexp, value string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

// BatchPlan splits a plan into deterministic resource-path/ID order. A batch
// remains an ordinary independently hashed plan and therefore passes through
// all normal execute-time safety checks.
func BatchPlan(plan domain.Plan, size int) ([]domain.Plan, error) {
	if size <= 0 {
		return nil, fmt.Errorf("batch size must be greater than zero")
	}
	actions := append([]domain.PlannedAction{}, plan.Actions...)
	slices.SortStableFunc(actions, func(a, b domain.PlannedAction) int {
		if byName := strings.Compare(a.ResourceName, b.ResourceName); byName != 0 {
			return byName
		}
		return strings.Compare(a.ResourceID, b.ResourceID)
	})
	if len(actions) == 0 {
		copyOfPlan := plan
		copyOfPlan.Hash = ""
		copyOfPlan.Actions = nil
		return []domain.Plan{copyOfPlan}, nil
	}
	batches := make([]domain.Plan, 0, (len(actions)+size-1)/size)
	for start := 0; start < len(actions); start += size {
		end := min(start+size, len(actions))
		batch := plan
		batch.Hash = ""
		batch.Actions = append([]domain.PlannedAction{}, actions[start:end]...)
		batches = append(batches, batch)
	}
	return batches, nil
}

// MergedPipelineAnalysis describes the effective configuration GitLab obtains
// after resolving include directives. It is analysis-only.
type MergedPipelineAnalysis struct {
	Project  domain.Project           `json:"project" yaml:"project"`
	Status   PipelineTagStatus        `json:"status" yaml:"status"`
	Includes []domain.PipelineInclude `json:"includes,omitempty" yaml:"includes,omitempty"`
	Reasons  []string                 `json:"reasons" yaml:"reasons"`
}

func AnalyzeMergedPipelineTags(ctx context.Context, analyzer provider.PipelineConfigAnalyzer, projects []domain.Project, tags []string, workers int) []MergedPipelineAnalysis {
	if workers <= 0 {
		workers = 5
	}
	results := make([]MergedPipelineAnalysis, len(projects))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, project := range projects {
		wg.Add(1)
		go func(index int, p domain.Project) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[index] = MergedPipelineAnalysis{Project: p, Status: PipelineTagFetchError, Reasons: []string{"cancelled"}}
				return
			}
			defer func() { <-sem }()
			content, includes, err := analyzer.GetMergedPipelineConfig(ctx, p.ID)
			if err != nil {
				results[index] = MergedPipelineAnalysis{Project: p, Status: PipelineTagFetchError, Reasons: []string{"could not obtain merged CI configuration: " + err.Error()}}
				return
			}
			_, changes, err := ciyaml.AddTags(content, tags)
			if err != nil {
				results[index] = MergedPipelineAnalysis{Project: p, Status: PipelineTagParseError, Includes: includes, Reasons: []string{"merged CI configuration could not be parsed: " + err.Error()}}
				return
			}
			status := PipelineTagPresent
			reasons := []string{"all requested tags are effective for default and explicit job tag lists"}
			if len(changes) > 0 {
				status = PipelineTagMissing
				reasons = describeChanges(changes)
			}
			results[index] = MergedPipelineAnalysis{Project: p, Status: status, Includes: includes, Reasons: reasons}
		}(i, project)
	}
	wg.Wait()
	return results
}

// PipelineProposalStatus pairs a project with a proposal or an explicit
// no-proposal/error status.
type PipelineProposalStatus struct {
	Project  domain.Project           `json:"project" yaml:"project"`
	Proposal *domain.PipelineProposal `json:"proposal,omitempty" yaml:"proposal,omitempty"`
	Error    string                   `json:"error,omitempty" yaml:"error,omitempty"`
}

func DiscoverPipelineProposalStatuses(ctx context.Context, reporter provider.PipelineProposalReporter, projects []domain.Project, tags []string, workers int) []PipelineProposalStatus {
	if workers <= 0 {
		workers = 5
	}
	results := make([]PipelineProposalStatus, len(projects))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, project := range projects {
		wg.Add(1)
		go func(index int, p domain.Project) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[index] = PipelineProposalStatus{Project: p, Error: "cancelled"}
				return
			}
			defer func() { <-sem }()
			proposals, err := reporter.ListPipelineTagProposals(ctx, p.ID, tags)
			if err != nil {
				results[index] = PipelineProposalStatus{Project: p, Error: err.Error()}
				return
			}
			if len(proposals) == 0 {
				results[index] = PipelineProposalStatus{Project: p}
				return
			}
			proposal := proposals[0]
			proposal.ProjectPath = p.FullPath
			results[index] = PipelineProposalStatus{Project: p, Proposal: &proposal}
		}(i, project)
	}
	wg.Wait()
	return results
}
