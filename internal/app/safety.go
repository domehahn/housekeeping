package app

import (
	"fmt"

	"github.com/domehahn/housekeeping/internal/domain"
)

// ResourceLimit is the pair of guard thresholds execution must never
// silently exceed for one resource type. A zero MaxPercentage disables
// that particular guard (there is no meaningful "0% allowed" use case); a
// zero MaxActions is a valid, strict "block all actions of this type"
// setting - and is also the default for any resource type nobody
// configured a limit for, so a newly added resource type fails closed
// rather than silently allowing unlimited actions.
type ResourceLimit struct {
	MaxActions    int
	MaxPercentage int // 0 = disabled
}

// SafetyLimits are the guard thresholds for every resource type a plan
// might touch, keyed by domain.ResourceType so adding a new resource type
// (as with pipeline_config/runner CI tag actions) never requires changing
// this struct's shape.
type SafetyLimits struct {
	Limits map[domain.ResourceType]ResourceLimit

	// MaxActionsOverridden records whether the operator explicitly raised
	// a limit via a CLI flag, purely so the override can be surfaced in
	// output/audit logs. It does not change guard behavior.
	MaxActionsOverridden bool
}

// Limit returns the configured limit for a resource type, or the
// zero-value ResourceLimit{0, 0} (block all, no percentage guard) if none
// was configured.
func (s SafetyLimits) Limit(rt domain.ResourceType) ResourceLimit {
	return s.Limits[rt]
}

// GuardViolation describes a single exceeded safety guard.
type GuardViolation struct {
	Resource domain.ResourceType
	Guard    string // "max_actions" | "max_percentage"
	Limit    int
	Actual   int
	Message  string
}

func (v GuardViolation) Error() string { return v.Message }

// resourceLabel renders a resource type as the plural noun phrase used in
// guard messages.
func resourceLabel(rt domain.ResourceType) string {
	switch rt {
	case domain.ResourceTypeProject:
		return "project actions"
	case domain.ResourceTypeUser:
		return "user actions"
	case domain.ResourceTypePipelineConfig:
		return "pipeline tag changes"
	case domain.ResourceTypeRunner:
		return "runner tag changes"
	default:
		return string(rt) + " actions"
	}
}

// CheckGuards evaluates a plan's action counts, split by resource type,
// against the configured limits and the number of resources that were
// discovered/evaluated per type (needed for the percentage guard). It
// returns every violated guard so operators see the full picture in one
// pass rather than fixing one limit only to immediately hit the next.
func CheckGuards(plan domain.Plan, discovered map[domain.ResourceType]int, limits SafetyLimits) []GuardViolation {
	actionCounts := countActionsByResource(plan)

	// Evaluate every resource type that appears either in the plan's
	// actions or in the discovered counts, so a resource type with zero
	// planned actions but a configured limit is still considered (it
	// simply never violates), and vice versa.
	seen := map[domain.ResourceType]bool{}
	for rt := range actionCounts {
		seen[rt] = true
	}
	for rt := range discovered {
		seen[rt] = true
	}

	var violations []GuardViolation
	for rt := range seen {
		actions := actionCounts[rt]
		limit := limits.Limit(rt)

		if actions > limit.MaxActions {
			violations = append(violations, GuardViolation{
				Resource: rt, Guard: "max_actions",
				Limit: limit.MaxActions, Actual: actions,
				Message: fmt.Sprintf("planned %s (%d) exceed configured maximum (%d)", resourceLabel(rt), actions, limit.MaxActions),
			})
		}

		total := discovered[rt]
		if limit.MaxPercentage > 0 && total > 0 {
			pct := percentage(actions, total)
			if pct > limit.MaxPercentage {
				violations = append(violations, GuardViolation{
					Resource: rt, Guard: "max_percentage",
					Limit: limit.MaxPercentage, Actual: pct,
					Message: fmt.Sprintf("planned %s (%d of %d, %d%%) exceed configured maximum percentage (%d%%)", resourceLabel(rt), actions, total, pct, limit.MaxPercentage),
				})
			}
		}
	}
	return violations
}

func countActionsByResource(plan domain.Plan) map[domain.ResourceType]int {
	counts := map[domain.ResourceType]int{}
	for _, a := range plan.Actions {
		counts[a.ResourceType]++
	}
	return counts
}

func percentage(part, total int) int {
	if total == 0 {
		return 0
	}
	// Round up so a safety limit can never be bypassed by integer
	// truncation (for example, 1/6 is 16.67%, not a permitted 16%).
	return (part*100 + total - 1) / total
}
