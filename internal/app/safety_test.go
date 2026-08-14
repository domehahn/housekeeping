package app

import (
	"testing"

	"github.com/domehahn/housekeeping/internal/domain"
)

func plan(n int, resourceType domain.ResourceType) domain.Plan {
	actions := make([]domain.PlannedAction, n)
	for i := range actions {
		actions[i] = domain.PlannedAction{ResourceType: resourceType, ResourceID: "id", Action: domain.ActionReport}
	}
	return domain.Plan{Actions: actions}
}

func TestCheckGuards_MaxActions(t *testing.T) {
	p := plan(15, domain.ResourceTypeProject)
	limits := SafetyLimits{MaxActionsProjects: 10, MaxActionsUsers: 20}

	violations := CheckGuards(p, 100, 0, limits)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(violations), violations)
	}
	if violations[0].Resource != "projects" || violations[0].Guard != "max_actions" {
		t.Errorf("unexpected violation: %+v", violations[0])
	}
}

func TestCheckGuards_MaxActionsWithinLimitPasses(t *testing.T) {
	p := plan(10, domain.ResourceTypeProject)
	limits := SafetyLimits{MaxActionsProjects: 10, MaxActionsUsers: 20}

	if violations := CheckGuards(p, 100, 0, limits); len(violations) != 0 {
		t.Errorf("expected no violations at the exact limit, got %+v", violations)
	}
}

func TestCheckGuards_MaxPercentage(t *testing.T) {
	p := plan(35, domain.ResourceTypeProject)
	limits := SafetyLimits{MaxActionsProjects: 1000, MaxActionsUsers: 1000, MaxPercentageProjects: 20}

	violations := CheckGuards(p, 100, 0, limits)
	if len(violations) != 1 || violations[0].Guard != "max_percentage" {
		t.Fatalf("expected a max_percentage violation, got %+v", violations)
	}
}

func TestCheckGuards_MaxPercentageDisabledWhenZero(t *testing.T) {
	p := plan(99, domain.ResourceTypeProject)
	limits := SafetyLimits{MaxActionsProjects: 1000, MaxActionsUsers: 1000, MaxPercentageProjects: 0}

	if violations := CheckGuards(p, 100, 0, limits); len(violations) != 0 {
		t.Errorf("expected no violations: max_percentage=0 disables the guard, got %+v", violations)
	}
}

func TestCheckGuards_ReportsBothProjectAndUserViolations(t *testing.T) {
	p := plan(5, domain.ResourceTypeProject)
	p.Actions = append(p.Actions, plan(5, domain.ResourceTypeUser).Actions...)
	limits := SafetyLimits{MaxActionsProjects: 1, MaxActionsUsers: 1}

	violations := CheckGuards(p, 100, 100, limits)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations (one per resource type), got %d: %+v", len(violations), violations)
	}
}
