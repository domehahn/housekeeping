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

// resolveSafetyLimits builds the full SafetyLimits from config for every
// resource type, applying a CLI --max-actions/--max-percentage override
// to only the resource type the current command targets. An override must
// be a positive, explicit value - 0 means "not overridden", never "no
// limit".
func resolveSafetyLimits(e *env, maxActionsOverride, maxPercentageOverride int, target domain.ResourceType) (app.SafetyLimits, error) {
	if maxActionsOverride < 0 {
		return app.SafetyLimits{}, fmt.Errorf("--max-actions must be non-negative, got %d", maxActionsOverride)
	}
	if maxPercentageOverride < 0 || maxPercentageOverride > 100 {
		return app.SafetyLimits{}, fmt.Errorf("--max-percentage must be between 0 and 100, got %d", maxPercentageOverride)
	}
	limits := app.SafetyLimits{
		Limits: map[domain.ResourceType]app.ResourceLimit{
			domain.ResourceTypeProject: {
				MaxActions: e.cfg.Safety.MaxActions.Projects, MaxPercentage: e.cfg.Safety.MaxPercentage.Projects,
			},
			domain.ResourceTypeUser: {
				MaxActions: e.cfg.Safety.MaxActions.Users, MaxPercentage: e.cfg.Safety.MaxPercentage.Users,
			},
			domain.ResourceTypePipelineConfig: {
				MaxActions: e.cfg.Safety.MaxActions.PipelineTags, MaxPercentage: e.cfg.Safety.MaxPercentage.PipelineTags,
			},
			domain.ResourceTypeRunner: {
				MaxActions: e.cfg.Safety.MaxActions.RunnerTags, MaxPercentage: e.cfg.Safety.MaxPercentage.RunnerTags,
			},
		},
	}
	if maxActionsOverride > 0 || maxPercentageOverride > 0 {
		l := limits.Limits[target]
		if maxActionsOverride > 0 {
			l.MaxActions = maxActionsOverride
			limits.MaxActionsOverridden = true
		}
		if maxPercentageOverride > 0 {
			l.MaxPercentage = maxPercentageOverride
		}
		limits.Limits[target] = l
	}
	return limits, nil
}

// resolvePlanSafetyLimits builds SafetyLimits for plan execution. For single-resource
// plans, generic overrides apply to the plan's resource type. For mixed-resource
// plans, generic overrides fail closed to prevent one resource type borrowing another's override.
func resolvePlanSafetyLimits(e *env, maxActionsOverride, maxPercentageOverride int, plan domain.Plan) (app.SafetyLimits, error) {
	if maxActionsOverride < 0 {
		return app.SafetyLimits{}, fmt.Errorf("--max-actions must be non-negative, got %d", maxActionsOverride)
	}
	if maxPercentageOverride < 0 || maxPercentageOverride > 100 {
		return app.SafetyLimits{}, fmt.Errorf("--max-percentage must be between 0 and 100, got %d", maxPercentageOverride)
	}

	limits := app.SafetyLimits{
		Limits: map[domain.ResourceType]app.ResourceLimit{
			domain.ResourceTypeProject: {
				MaxActions: e.cfg.Safety.MaxActions.Projects, MaxPercentage: e.cfg.Safety.MaxPercentage.Projects,
			},
			domain.ResourceTypeUser: {
				MaxActions: e.cfg.Safety.MaxActions.Users, MaxPercentage: e.cfg.Safety.MaxPercentage.Users,
			},
			domain.ResourceTypePipelineConfig: {
				MaxActions: e.cfg.Safety.MaxActions.PipelineTags, MaxPercentage: e.cfg.Safety.MaxPercentage.PipelineTags,
			},
			domain.ResourceTypeRunner: {
				MaxActions: e.cfg.Safety.MaxActions.RunnerTags, MaxPercentage: e.cfg.Safety.MaxPercentage.RunnerTags,
			},
		},
	}

	if maxActionsOverride > 0 || maxPercentageOverride > 0 {
		present := map[domain.ResourceType]bool{}
		for _, a := range plan.Actions {
			present[a.ResourceType] = true
		}
		if len(present) > 1 {
			return app.SafetyLimits{}, fmt.Errorf("generic --max-actions/--max-percentage overrides cannot be applied to a mixed-resource plan with actions for %d resource types; set per-resource safety limits in config instead", len(present))
		}
		target := domain.ResourceTypeProject
		for rt := range present {
			target = rt
		}

		l := limits.Limits[target]
		if maxActionsOverride > 0 {
			l.MaxActions = maxActionsOverride
			limits.MaxActionsOverridden = true
		}
		if maxPercentageOverride > 0 {
			l.MaxPercentage = maxPercentageOverride
		}
		limits.Limits[target] = l
	}

	return limits, nil
}

// planTable builds the standard table view of a plan's actions.
func planTable(plan domain.Plan) output.Table {
	rows := make([][]string, 0, len(plan.Actions))
	for _, a := range plan.Actions {
		name := a.ResourceName
		if a.BranchName != "" {
			name += "#" + a.BranchName
		}
		rows = append(rows, []string{
			string(a.ResourceType), a.ResourceID, name, string(a.Action), joinReasons(a.Reason),
		})
	}
	return output.Table{
		Headers: []string{"Type", "ID", "Name", "Action", "Reasons"},
		Rows:    rows,
		Footer:  fmt.Sprintf("Plan: %d action(s) for provider=%s instance=%s scope=%s", len(plan.Actions), plan.Provider, plan.Instance, plan.Scope.Path),
	}
}
