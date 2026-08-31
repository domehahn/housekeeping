package app

import (
	"github.com/domehahn/housekeeping/internal/domain"
)

// BuildProjectPlan turns matched (non-protected) project evaluations into a
// domain.Plan. Resource IDs, never names alone, are used to identify the
// target of each action - see docs/architecture.md "Resource identity".
func BuildProjectPlan(
	providerName, instance string,
	scope domain.Scope,
	matched []ProjectEvaluation,
	action domain.ActionType,
	clock domain.Clock,
) domain.Plan {
	actions := make([]domain.PlannedAction, 0, len(matched))
	now := clock.Now()
	for _, m := range matched {
		actions = append(actions, domain.PlannedAction{
			ResourceType: domain.ResourceTypeProject,
			ResourceID:   m.Project.ID,
			ResourceName: m.Project.FullPath,
			Action:       action,
			Reason:       m.Evaluation.Reasons,
			EvaluatedAt:  now,
		})
	}
	return domain.Plan{
		Version:   domain.PlanVersion,
		Provider:  providerName,
		Instance:  instance,
		Scope:     toPlanScope(scope),
		CreatedAt: now,
		Actions:   actions,
	}
}

// BuildUserPlan turns matched (non-protected) user evaluations into a
// domain.Plan.
func BuildUserPlan(
	providerName, instance string,
	scope domain.Scope,
	matched []UserEvaluation,
	action domain.ActionType,
	clock domain.Clock,
) domain.Plan {
	actions := make([]domain.PlannedAction, 0, len(matched))
	now := clock.Now()
	for _, m := range matched {
		actions = append(actions, domain.PlannedAction{
			ResourceType: domain.ResourceTypeUser,
			ResourceID:   m.User.ID,
			ResourceName: m.User.Username,
			GroupID:      m.User.GroupID,
			AccessLevel:  m.User.AccessLevel,
			Action:       action,
			Reason:       m.Evaluation.Reasons,
			EvaluatedAt:  now,
		})
	}
	return domain.Plan{
		Version:   domain.PlanVersion,
		Provider:  providerName,
		Instance:  instance,
		Scope:     toPlanScope(scope),
		CreatedAt: now,
		Actions:   actions,
	}
}

// BuildPipelineTagPlan turns matched (non-protected) pipeline-tag
// evaluations into a domain.Plan of ActionAddPipelineTag actions.
func BuildPipelineTagPlan(
	providerName, instance string,
	scope domain.Scope,
	matched []PipelineTagEvaluation,
	tags []string,
	clock domain.Clock,
) domain.Plan {
	actions := make([]domain.PlannedAction, 0, len(matched))
	now := clock.Now()
	for _, m := range matched {
		actions = append(actions, domain.PlannedAction{
			ResourceType: domain.ResourceTypePipelineConfig,
			ResourceID:   m.Project.ID,
			ResourceName: m.Project.FullPath,
			TagValues:    append([]string{}, tags...),
			Action:       domain.ActionAddPipelineTag,
			Reason:       m.Reasons,
			EvaluatedAt:  now,
		})
	}
	return domain.Plan{
		Version:   domain.PlanVersion,
		Provider:  providerName,
		Instance:  instance,
		Scope:     toPlanScope(scope),
		CreatedAt: now,
		Actions:   actions,
	}
}

// BuildRunnerTagPlan turns matched runner-tag evaluations into a
// domain.Plan of ActionAddRunnerTag actions. Scope here is informational
// only (runners are not themselves scoped the way projects/users are) -
// GroupID/AccessLevel are left empty.
func BuildRunnerTagPlan(
	providerName, instance string,
	scope domain.Scope,
	matched []RunnerTagEvaluation,
	tag string,
	clock domain.Clock,
) domain.Plan {
	actions := make([]domain.PlannedAction, 0, len(matched))
	now := clock.Now()
	for _, m := range matched {
		actions = append(actions, domain.PlannedAction{
			ResourceType:           domain.ResourceTypeRunner,
			ResourceID:             m.Runner.ID,
			ResourceName:           m.Runner.Description,
			TagValues:              []string{tag},
			Action:                 domain.ActionAddRunnerTag,
			Reason:                 m.Reasons,
			EvaluatedAt:            now,
			OutOfScopeProjectCount: m.OutOfScopeProjectCount(),
			OutOfScopeProjectPaths: m.Runner.OutOfScopeProjectPaths,
		})
	}
	return domain.Plan{
		Version:   domain.PlanVersion,
		Provider:  providerName,
		Instance:  instance,
		Scope:     toPlanScope(scope),
		CreatedAt: now,
		Actions:   actions,
	}
}

// BuildPipelineTagRenamePlan turns matched pipeline-tag-rename evaluations
// into a domain.Plan of ActionReplacePipelineTag actions.
func BuildPipelineTagRenamePlan(
	providerName, instance string,
	scope domain.Scope,
	matched []PipelineTagEvaluation,
	renames []domain.TagRename,
	clock domain.Clock,
) domain.Plan {
	actions := make([]domain.PlannedAction, 0, len(matched))
	now := clock.Now()
	for _, m := range matched {
		actions = append(actions, domain.PlannedAction{
			ResourceType: domain.ResourceTypePipelineConfig,
			ResourceID:   m.Project.ID,
			ResourceName: m.Project.FullPath,
			TagRenames:   append([]domain.TagRename{}, renames...),
			Action:       domain.ActionReplacePipelineTag,
			Reason:       m.Reasons,
			EvaluatedAt:  now,
		})
	}
	return domain.Plan{
		Version:   domain.PlanVersion,
		Provider:  providerName,
		Instance:  instance,
		Scope:     toPlanScope(scope),
		CreatedAt: now,
		Actions:   actions,
	}
}

// BuildRunnerTagRenamePlan turns matched runner-tag-rename evaluations
// into a domain.Plan of ActionReplaceRunnerTag actions. Scope here is
// informational only, exactly as in BuildRunnerTagPlan.
func BuildRunnerTagRenamePlan(
	providerName, instance string,
	scope domain.Scope,
	matched []RunnerTagRenameEvaluation,
	renames []domain.TagRename,
	clock domain.Clock,
) domain.Plan {
	actions := make([]domain.PlannedAction, 0, len(matched))
	now := clock.Now()
	for _, m := range matched {
		actions = append(actions, domain.PlannedAction{
			ResourceType:           domain.ResourceTypeRunner,
			ResourceID:             m.Runner.ID,
			ResourceName:           m.Runner.Description,
			TagRenames:             append([]domain.TagRename{}, renames...),
			Action:                 domain.ActionReplaceRunnerTag,
			Reason:                 m.Reasons,
			EvaluatedAt:            now,
			OutOfScopeProjectCount: m.OutOfScopeProjectCount(),
			OutOfScopeProjectPaths: m.Runner.OutOfScopeProjectPaths,
		})
	}
	return domain.Plan{
		Version:   domain.PlanVersion,
		Provider:  providerName,
		Instance:  instance,
		Scope:     toPlanScope(scope),
		CreatedAt: now,
		Actions:   actions,
	}
}

func toPlanScope(scope domain.Scope) domain.PlanScope {
	return domain.PlanScope{
		Type:      scope.Type,
		ID:        scope.ID,
		Path:      scope.Path,
		Recursive: scope.Recursive,
	}
}
