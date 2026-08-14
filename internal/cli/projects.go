package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/domehahn/housekeeping/internal/app"
	"github.com/domehahn/housekeeping/internal/config"
	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/output"
	"github.com/domehahn/housekeeping/internal/policy/project"
	"github.com/domehahn/housekeeping/internal/provider"
)

// projectFlags are the flags shared by `projects evaluate` and
// `projects plan` (list only needs scope flags, handled separately).
type projectFlags struct {
	inactiveFor string
	include     []string
	exclude     []string
}

func newProjectsCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{Use: "projects", Short: "Discover, evaluate, and plan cleanup of projects"}
	cmd.AddCommand(newProjectsListCmd(e), newProjectsEvaluateCmd(e), newProjectsPlanCmd(e))
	return cmd
}

func resolveGroupFlag(e *env, cmd *cobra.Command) (string, bool, error) {
	group := e.cfg.Scope.Group
	if group == "" {
		return "", false, exitErr(ExitInvalidConfiguration, fmt.Errorf("--group (or scope.group in config) is required"))
	}
	return group, e.cfg.Scope.Recursive, nil
}

func newProjectsListCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List projects discovered within a scope",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := e.requireClient()
			if err != nil {
				return err
			}
			group, recursive, err := resolveGroupFlag(e, cmd)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			scope, err := app.ResolveScope(ctx, client, group, recursive)
			if err != nil {
				return wrapProviderErr(err)
			}
			projects, err := app.DiscoverProjects(ctx, client, scope)
			if err != nil {
				return wrapProviderErr(err)
			}

			rows := make([][]string, 0, len(projects))
			for _, p := range projects {
				rows = append(rows, []string{p.ID, p.FullPath, archivedStr(p.Archived), formatTimestamp(p.LastActivityAt, e.clock)})
			}
			table := output.Table{
				Headers: []string{"ID", "Path", "Archived", "Last Activity"},
				Rows:    rows,
				Footer:  fmt.Sprintf("%d project(s) discovered in %s", len(projects), scope.Path),
			}
			return output.Render(cmd.OutOrStdout(), e.format, table, projects)
		},
	}
}

func newProjectsEvaluateCmd(e *env) *cobra.Command {
	flags := &projectFlags{}
	cmd := &cobra.Command{
		Use:   "evaluate",
		Short: "Evaluate projects against cleanup policies without producing a plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := e.requireClient()
			if err != nil {
				return err
			}
			summary, scope, err := runProjectEvaluation(cmd, e, client, flags)
			if err != nil {
				return err
			}
			return renderProjectEvaluation(cmd, e, scope, summary)
		},
	}
	bindProjectFlags(cmd, flags)
	return cmd
}

func newProjectsPlanCmd(e *env) *cobra.Command {
	flags := &projectFlags{}
	var actionStr, outputPlan string
	var maxActions, maxPercentage int
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Evaluate projects and produce a reviewable, saveable cleanup plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			action, err := parseProjectAction(actionStr)
			if err != nil {
				return exitErr(ExitInvalidConfiguration, err)
			}

			client, err := e.requireClient()
			if err != nil {
				return err
			}
			summary, scope, err := runProjectEvaluation(cmd, e, client, flags)
			if err != nil {
				return err
			}

			info, err := client.Info(cmd.Context())
			if err != nil {
				return wrapProviderErr(err)
			}

			matched := summary.Matched()
			plan := app.BuildProjectPlan(info.Provider, info.Instance, scope, matched, action, e.clock)

			limits := resolveSafetyLimits(e, maxActions, maxPercentage, true)
			if violations := app.CheckGuards(plan, summary.Discovered(), 0, limits); len(violations) > 0 {
				for _, v := range violations {
					cmd.PrintErrln("SAFETY GUARD:", v.Error())
				}
				return exitErr(ExitSafetyGuardTriggered, fmt.Errorf("plan exceeds configured safety guards; adjust policy or pass an explicit override"))
			}

			if outputPlan != "" {
				if err := app.SavePlan(outputPlan, plan); err != nil {
					return exitErr(ExitGeneralError, err)
				}
				cmd.Printf("Plan written to %s (%d action(s)).\n", outputPlan, len(plan.Actions))
			}

			if err := renderProjectEvaluation(cmd, e, scope, summary); err != nil {
				return err
			}
			return output.Render(cmd.OutOrStdout(), e.format, planTable(plan), plan)
		},
	}
	bindProjectFlags(cmd, flags)
	cmd.Flags().StringVar(&actionStr, "action", "report", "action to plan: report|delete|archive")
	cmd.Flags().StringVar(&outputPlan, "output-plan", "", "write the plan as JSON to this path")
	cmd.Flags().IntVar(&maxActions, "max-actions", 0, "override safety.max_actions.projects (must be explicit)")
	cmd.Flags().IntVar(&maxPercentage, "max-percentage", 0, "override safety.max_percentage.projects (must be explicit)")
	return cmd
}

func bindProjectFlags(cmd *cobra.Command, flags *projectFlags) {
	cmd.Flags().StringVar(&flags.inactiveFor, "inactive-for", "", "match projects with no activity for longer than this (e.g. 90d)")
	cmd.Flags().StringSliceVar(&flags.include, "include", nil, "additional include regex for project path/slug (repeatable)")
	cmd.Flags().StringSliceVar(&flags.exclude, "exclude", nil, "additional exclude regex/path for project path/slug (repeatable); excludes always win")
}

func runProjectEvaluation(cmd *cobra.Command, e *env, client provider.Client, flags *projectFlags) (app.ProjectEvaluationSummary, domain.Scope, error) {
	group, recursive, err := resolveGroupFlag(e, cmd)
	if err != nil {
		return app.ProjectEvaluationSummary{}, domain.Scope{}, err
	}
	ctx := cmd.Context()
	scope, err := app.ResolveScope(ctx, client, group, recursive)
	if err != nil {
		return app.ProjectEvaluationSummary{}, domain.Scope{}, wrapProviderErr(err)
	}
	projects, err := app.DiscoverProjects(ctx, client, scope)
	if err != nil {
		return app.ProjectEvaluationSummary{}, domain.Scope{}, wrapProviderErr(err)
	}

	policies, err := buildProjectPolicies(e, flags)
	if err != nil {
		return app.ProjectEvaluationSummary{}, domain.Scope{}, exitErr(ExitInvalidConfiguration, err)
	}
	protection, err := project.NewProtection(e.cfg.Projects.Protection.Paths, e.cfg.Projects.Protection.Regex)
	if err != nil {
		return app.ProjectEvaluationSummary{}, domain.Scope{}, exitErr(ExitInvalidConfiguration, err)
	}

	summary := app.EvaluateProjects(ctx, projects, policies, protection)
	return summary, scope, nil
}

func buildProjectPolicies(e *env, flags *projectFlags) ([]domain.ProjectPolicy, error) {
	var policies []domain.ProjectPolicy

	days := e.cfg.Projects.Inactive.Days
	enabled := e.cfg.Projects.Inactive.Enabled
	if flags.inactiveFor != "" {
		d, err := config.ParseDuration(flags.inactiveFor)
		if err != nil {
			return nil, err
		}
		days = int(d.Hours() / 24)
		enabled = true
	}
	if enabled {
		policies = append(policies, project.InactivePolicy{ThresholdDays: days, Clock: e.clock})
	}

	if e.cfg.Projects.Archived.Enabled {
		policies = append(policies, project.ArchivedPolicy{})
	}

	include := append(append([]string{}, e.cfg.Projects.Include...), flags.include...)
	exclude := append(append([]string{}, e.cfg.Projects.Exclude...), flags.exclude...)
	if len(include) > 0 || len(exclude) > 0 {
		namePolicy, err := project.NewNamePolicy(include, exclude)
		if err != nil {
			return nil, err
		}
		policies = append(policies, namePolicy)
	}

	if len(policies) == 0 {
		return nil, fmt.Errorf("no project policy enabled: set --inactive-for, projects.inactive.enabled, projects.archived.enabled, or include/exclude patterns")
	}
	return policies, nil
}

func parseProjectAction(s string) (domain.ActionType, error) {
	switch s {
	case "report", "":
		return domain.ActionReport, nil
	case "delete":
		return domain.ActionDeleteProject, nil
	case "archive":
		return domain.ActionArchiveProject, nil
	default:
		return "", fmt.Errorf("unsupported project action %q (supported: report|delete|archive)", s)
	}
}

func renderProjectEvaluation(cmd *cobra.Command, e *env, scope domain.Scope, summary app.ProjectEvaluationSummary) error {
	matched := summary.Matched()
	rows := make([][]string, 0, len(matched))
	for _, m := range matched {
		rows = append(rows, []string{m.Project.ID, m.Project.FullPath, joinReasons(m.Evaluation.Reasons)})
	}
	table := output.Table{
		Headers: []string{"ID", "Path", "Reasons"},
		Rows:    rows,
		Footer: fmt.Sprintf("Evaluated %d, matched %d, protected %d in scope %s",
			summary.Discovered(), len(matched), len(summary.Protected()), scope.Path),
	}
	return output.Render(cmd.OutOrStdout(), e.format, table, summary)
}

func archivedStr(a bool) string {
	if a {
		return "yes"
	}
	return "no"
}

func joinReasons(reasons []string) string {
	out := ""
	for i, r := range reasons {
		if i > 0 {
			out += "; "
		}
		out += r
	}
	return out
}
