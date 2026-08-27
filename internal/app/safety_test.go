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

func limitsFor(rt domain.ResourceType, maxActions, maxPercentage int) SafetyLimits {
	return SafetyLimits{Limits: map[domain.ResourceType]ResourceLimit{
		rt: {MaxActions: maxActions, MaxPercentage: maxPercentage},
	}}
}

func discovered(rt domain.ResourceType, n int) map[domain.ResourceType]int {
	return map[domain.ResourceType]int{rt: n}
}

func TestCheckGuards_MaxActions(t *testing.T) {
	p := plan(15, domain.ResourceTypeProject)
	limits := limitsFor(domain.ResourceTypeProject, 10, 0)

	violations := CheckGuards(p, discovered(domain.ResourceTypeProject, 100), limits)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(violations), violations)
	}
	if violations[0].Resource != domain.ResourceTypeProject || violations[0].Guard != "max_actions" {
		t.Errorf("unexpected violation: %+v", violations[0])
	}
}

func TestCheckGuards_MaxActionsWithinLimitPasses(t *testing.T) {
	p := plan(10, domain.ResourceTypeProject)
	limits := limitsFor(domain.ResourceTypeProject, 10, 0)

	if violations := CheckGuards(p, discovered(domain.ResourceTypeProject, 100), limits); len(violations) != 0 {
		t.Errorf("expected no violations at the exact limit, got %+v", violations)
	}
}

func TestCheckGuards_MaxPercentage(t *testing.T) {
	p := plan(35, domain.ResourceTypeProject)
	limits := limitsFor(domain.ResourceTypeProject, 1000, 20)

	violations := CheckGuards(p, discovered(domain.ResourceTypeProject, 100), limits)
	if len(violations) != 1 || violations[0].Guard != "max_percentage" {
		t.Fatalf("expected a max_percentage violation, got %+v", violations)
	}
}

func TestCheckGuards_MaxPercentageRoundsUp(t *testing.T) {
	p := plan(1, domain.ResourceTypeProject)
	limits := limitsFor(domain.ResourceTypeProject, 10, 16)
	violations := CheckGuards(p, discovered(domain.ResourceTypeProject, 6), limits)
	if len(violations) != 1 {
		t.Fatalf("expected 1/6 (16.67%%) to exceed a 16%% limit, got %+v", violations)
	}
}

func TestCheckGuards_MaxPercentageDisabledWhenZero(t *testing.T) {
	p := plan(99, domain.ResourceTypeProject)
	limits := limitsFor(domain.ResourceTypeProject, 1000, 0)

	if violations := CheckGuards(p, discovered(domain.ResourceTypeProject, 100), limits); len(violations) != 0 {
		t.Errorf("expected no violations: max_percentage=0 disables the guard, got %+v", violations)
	}
}

func TestCheckGuards_ReportsBothProjectAndUserViolations(t *testing.T) {
	p := plan(5, domain.ResourceTypeProject)
	p.Actions = append(p.Actions, plan(5, domain.ResourceTypeUser).Actions...)
	limits := SafetyLimits{Limits: map[domain.ResourceType]ResourceLimit{
		domain.ResourceTypeProject: {MaxActions: 1},
		domain.ResourceTypeUser:    {MaxActions: 1},
	}}

	violations := CheckGuards(p, map[domain.ResourceType]int{
		domain.ResourceTypeProject: 100, domain.ResourceTypeUser: 100,
	}, limits)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations (one per resource type), got %d: %+v", len(violations), violations)
	}
}

func TestCheckGuards_UnconfiguredResourceTypeDefaultsToBlockAll(t *testing.T) {
	// A resource type with no entry in Limits gets the zero-value
	// ResourceLimit{0, 0} - i.e. it fails closed, blocking any planned
	// action of that type, rather than silently allowing unlimited
	// actions for a type nobody explicitly configured a guard for.
	p := plan(1, domain.ResourceTypeRunner)
	limits := SafetyLimits{Limits: map[domain.ResourceType]ResourceLimit{}}

	violations := CheckGuards(p, nil, limits)
	if len(violations) != 1 || violations[0].Resource != domain.ResourceTypeRunner {
		t.Fatalf("expected an unconfigured resource type to fail closed, got %+v", violations)
	}
}
