package app

import (
	"context"
	"fmt"

	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/provider"
)

// ProjectDiscoverer is the subset of the provider needed to enumerate
// projects within a scope.
type ProjectDiscoverer interface {
	provider.ScopeResolver
	provider.ProjectReader
}

// ResolveScope resolves a group path into a domain.Scope, honoring
// the recursive flag. Kept separate from EvaluateProjects so callers (e.g.
// `projects list`) that only need discovery, not evaluation, can use it
// directly.
func ResolveScope(ctx context.Context, reader provider.ScopeResolver, groupPath string, recursive bool) (domain.Scope, error) {
	scope, _, err := reader.ResolveGroupScope(ctx, groupPath, recursive)
	if err != nil {
		return domain.Scope{}, fmt.Errorf("resolve group scope %q: %w", groupPath, err)
	}
	return scope, nil
}

// DiscoverProjects lists every project within an already-resolved scope.
func DiscoverProjects(ctx context.Context, reader provider.ProjectReader, scope domain.Scope) ([]domain.Project, error) {
	projects, err := reader.ListProjects(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("list projects in scope %q: %w", scope.Path, err)
	}
	return projects, nil
}

// EvaluateProjects runs every policy (combined with AND across policies,
// mirroring how project cleanup criteria are configured as independent
// enabled/disabled blocks that must all hold) plus the name include/exclude
// policy and protection rule against each discovered project.
func EvaluateProjects(
	ctx context.Context,
	projects []domain.Project,
	policies []domain.ProjectPolicy,
	protection domain.ProjectProtectionRule,
) ProjectEvaluationSummary {
	results := make([]ProjectEvaluation, 0, len(projects))
	for _, p := range projects {
		evals := make([]domain.Evaluation, 0, len(policies))
		for _, policy := range policies {
			evals = append(evals, policy.Evaluate(ctx, p))
		}
		eval := domain.And(evals...)

		protected, reason := false, ""
		if protection != nil {
			protected, reason = protection.IsProtected(p)
		}

		results = append(results, ProjectEvaluation{
			Project:          p,
			Evaluation:       eval,
			Protected:        protected,
			ProtectionReason: reason,
		})
	}
	return ProjectEvaluationSummary{Results: results}
}
