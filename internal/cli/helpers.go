package cli

import (
	"fmt"

	"github.com/domehahn/housekeeping/internal/app"
	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/output"
)

// formatTimestamp renders a domain.Timestamp for table output, making the
// unknown/never/known distinction explicit rather than showing a blank
// cell that could be misread as "no data = inactive".
func formatTimestamp(ts domain.Timestamp, clock domain.Clock) string {
	if !ts.IsKnown() {
		return "unknown"
	}
	if ts.At == nil {
		return "never"
	}
	days, _ := ts.DaysAgo(clock.Now())
	return fmt.Sprintf("%dd ago (%s)", days, ts.At.Format("2006-01-02"))
}

// resolveSafetyLimits builds the full SafetyLimits from config, applying a
// CLI --max-actions/--max-percentage override to only the resource type
// the current command targets. An override must be a positive, explicit
// value - 0 means "not overridden", never "no limit".
func resolveSafetyLimits(e *env, maxActionsOverride, maxPercentageOverride int, isProjects bool) app.SafetyLimits {
	limits := app.SafetyLimits{
		MaxActionsProjects:    e.cfg.Safety.MaxActions.Projects,
		MaxActionsUsers:       e.cfg.Safety.MaxActions.Users,
		MaxPercentageProjects: e.cfg.Safety.MaxPercentage.Projects,
		MaxPercentageUsers:    e.cfg.Safety.MaxPercentage.Users,
	}
	if isProjects {
		if maxActionsOverride > 0 {
			limits.MaxActionsProjects = maxActionsOverride
			limits.MaxActionsOverridden = true
		}
		if maxPercentageOverride > 0 {
			limits.MaxPercentageProjects = maxPercentageOverride
		}
		return limits
	}
	if maxActionsOverride > 0 {
		limits.MaxActionsUsers = maxActionsOverride
		limits.MaxActionsOverridden = true
	}
	if maxPercentageOverride > 0 {
		limits.MaxPercentageUsers = maxPercentageOverride
	}
	return limits
}

// planTable builds the standard table view of a plan's actions.
func planTable(plan domain.Plan) output.Table {
	rows := make([][]string, 0, len(plan.Actions))
	for _, a := range plan.Actions {
		rows = append(rows, []string{
			string(a.ResourceType), a.ResourceID, a.ResourceName, string(a.Action), joinReasons(a.Reason),
		})
	}
	return output.Table{
		Headers: []string{"Type", "ID", "Name", "Action", "Reasons"},
		Rows:    rows,
		Footer:  fmt.Sprintf("Plan: %d action(s) for provider=%s instance=%s scope=%s", len(plan.Actions), plan.Provider, plan.Instance, plan.Scope.Path),
	}
}
