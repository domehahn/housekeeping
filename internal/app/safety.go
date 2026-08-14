package app

import (
	"fmt"

	"github.com/domehahn/housekeeping/internal/domain"
)

// SafetyLimits are the guard thresholds execution must never silently
// exceed. A zero MaxPercentage disables that particular guard (there is no
// meaningful "0% allowed" use case); a zero MaxActions is a valid, strict
// "block all actions of this type" setting.
type SafetyLimits struct {
	MaxActionsProjects    int
	MaxActionsUsers       int
	MaxPercentageProjects int // 0 = disabled
	MaxPercentageUsers    int // 0 = disabled

	// MaxActionsOverridden/MaxPercentageOverridden record whether the
	// operator explicitly raised a limit via a CLI flag, purely so that
	// override can be surfaced in output/audit logs. They do not change
	// guard behavior.
	MaxActionsOverridden bool
}

// GuardViolation describes a single exceeded safety guard.
type GuardViolation struct {
	Resource string // "projects" | "users"
	Guard    string // "max_actions" | "max_percentage"
	Limit    int
	Actual   int
	Message  string
}

func (v GuardViolation) Error() string { return v.Message }

// CheckGuards evaluates a plan's action counts, split by resource type,
// against the configured limits and the number of resources that were
// discovered/evaluated (needed for the percentage guard). It returns every
// violated guard so operators see the full picture in one pass rather than
// fixing one limit only to immediately hit the next.
func CheckGuards(plan domain.Plan, discoveredProjects, discoveredUsers int, limits SafetyLimits) []GuardViolation {
	projectActions, userActions := countActionsByResource(plan)

	var violations []GuardViolation

	if projectActions > limits.MaxActionsProjects {
		violations = append(violations, GuardViolation{
			Resource: "projects", Guard: "max_actions",
			Limit: limits.MaxActionsProjects, Actual: projectActions,
			Message: fmt.Sprintf("planned project actions (%d) exceed configured maximum (%d)", projectActions, limits.MaxActionsProjects),
		})
	}
	if userActions > limits.MaxActionsUsers {
		violations = append(violations, GuardViolation{
			Resource: "users", Guard: "max_actions",
			Limit: limits.MaxActionsUsers, Actual: userActions,
			Message: fmt.Sprintf("planned user actions (%d) exceed configured maximum (%d)", userActions, limits.MaxActionsUsers),
		})
	}

	if limits.MaxPercentageProjects > 0 && discoveredProjects > 0 {
		pct := percentage(projectActions, discoveredProjects)
		if pct > limits.MaxPercentageProjects {
			violations = append(violations, GuardViolation{
				Resource: "projects", Guard: "max_percentage",
				Limit: limits.MaxPercentageProjects, Actual: pct,
				Message: fmt.Sprintf("planned project actions (%d of %d, %d%%) exceed configured maximum percentage (%d%%)", projectActions, discoveredProjects, pct, limits.MaxPercentageProjects),
			})
		}
	}
	if limits.MaxPercentageUsers > 0 && discoveredUsers > 0 {
		pct := percentage(userActions, discoveredUsers)
		if pct > limits.MaxPercentageUsers {
			violations = append(violations, GuardViolation{
				Resource: "users", Guard: "max_percentage",
				Limit: limits.MaxPercentageUsers, Actual: pct,
				Message: fmt.Sprintf("planned user actions (%d of %d, %d%%) exceed configured maximum percentage (%d%%)", userActions, discoveredUsers, pct, limits.MaxPercentageUsers),
			})
		}
	}

	return violations
}

func countActionsByResource(plan domain.Plan) (projects, users int) {
	for _, a := range plan.Actions {
		switch a.ResourceType {
		case domain.ResourceTypeProject:
			projects++
		case domain.ResourceTypeUser:
			users++
		}
	}
	return
}

func percentage(part, total int) int {
	if total == 0 {
		return 0
	}
	return (part * 100) / total
}
