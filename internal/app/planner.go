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

func toPlanScope(scope domain.Scope) domain.PlanScope {
	return domain.PlanScope{
		Type:      scope.Type,
		ID:        scope.ID,
		Path:      scope.Path,
		Recursive: scope.Recursive,
	}
}
