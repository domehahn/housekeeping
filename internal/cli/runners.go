package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/domehahn/housekeeping/internal/app"
	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/output"
	"github.com/domehahn/housekeeping/internal/provider"
)

func newRunnersCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runners",
		Short: "Discover, evaluate, and plan CI tag additions to the runners used by a scope",
		Long: `runners manages adding a CI tag directly to the tag_list of runners used by
projects in a scope (via the GitLab Runner API), as opposed to
"pipelines", which edits .gitlab-ci.yml files.

If a runner is SHARED, changing its tags can affect projects outside the
evaluated scope. Every command here reports that "blast radius" (the
out-of-scope projects using each runner), and "execute --apply" refuses
to proceed on a plan touching a shared runner used outside scope unless
you pass --confirm-out-of-scope-impact=<N> matching the exact total -
see docs/adr/0005-ci-tag-management-scope.md.`,
	}
	cmd.AddCommand(newRunnersListCmd(e), newRunnersEvaluateCmd(e), newRunnersPlanCmd(e))
	return cmd
}

func newRunnersListCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List runners used by projects in scope, with their tags and blast radius",
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

			runners, err := client.ListRunnersForProjects(ctx, projectIDs(projects))
			if err != nil {
				return wrapProviderErr(err)
			}

			rows := make([][]string, 0, len(runners))
			for _, r := range runners {
				rows = append(rows, []string{
					r.ID, r.Description, sharedStr(r.Shared), joinReasons(r.TagList),
					fmt.Sprintf("%d", len(r.OutOfScopeProjectPaths)),
				})
			}
			table := output.Table{
				Headers: []string{"ID", "Description", "Shared", "Tags", "Out-of-Scope Projects"},
				Rows:    rows,
				Footer:  fmt.Sprintf("%d runner(s) used by projects in %s", len(runners), scope.Path),
			}
			return output.Render(cmd.OutOrStdout(), e.format, table, runners)
		},
	}
}

func newRunnersEvaluateCmd(e *env) *cobra.Command {
	var tag string
	cmd := &cobra.Command{
		Use:   "evaluate",
		Short: "Check runners used by a scope for a CI tag, without producing a plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tag == "" {
				return exitErr(ExitInvalidConfiguration, fmt.Errorf("--tag is required"))
			}
			client, err := e.requireClient()
			if err != nil {
				return err
			}
			summary, scope, err := runRunnerTagEvaluation(cmd, e, client, tag)
			if err != nil {
				return err
			}
			return renderRunnerTagEvaluation(cmd, e, scope, summary)
		},
	}
	cmd.Flags().StringVar(&tag, "tag", "", "the CI tag to check for/add (required)")
	return cmd
}

func newRunnersPlanCmd(e *env) *cobra.Command {
	var tag, outputPlan string
	var maxActions, maxPercentage int
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Evaluate runner tags and produce a reviewable, saveable plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tag == "" {
				return exitErr(ExitInvalidConfiguration, fmt.Errorf("--tag is required"))
			}
			client, err := e.requireClient()
			if err != nil {
				return err
			}
			summary, scope, err := runRunnerTagEvaluation(cmd, e, client, tag)
			if err != nil {
				return err
			}

			info, err := client.Info(cmd.Context())
			if err != nil {
				return wrapProviderErr(err)
			}

			matched := summary.Matched()
			plan := app.BuildRunnerTagPlan(info.Provider, info.Instance, scope, matched, tag, e.clock)

			limits := resolveSafetyLimits(e, maxActions, maxPercentage, domain.ResourceTypeRunner)
			discovered := map[domain.ResourceType]int{domain.ResourceTypeRunner: summary.Discovered()}
			if violations := app.CheckGuards(plan, discovered, limits); len(violations) > 0 {
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

			if err := renderRunnerTagEvaluation(cmd, e, scope, summary); err != nil {
				return err
			}
			return output.Render(cmd.OutOrStdout(), e.format, planTable(plan), plan)
		},
	}
	cmd.Flags().StringVar(&tag, "tag", "", "the CI tag to check for/add (required)")
	cmd.Flags().StringVar(&outputPlan, "output-plan", "", "write the plan as JSON to this path")
	cmd.Flags().IntVar(&maxActions, "max-actions", 0, "override safety.max_actions.runner_tags (must be explicit)")
	cmd.Flags().IntVar(&maxPercentage, "max-percentage", 0, "override safety.max_percentage.runner_tags (must be explicit)")
	return cmd
}

func runRunnerTagEvaluation(cmd *cobra.Command, e *env, client provider.Client, tag string) (app.RunnerTagEvaluationSummary, domain.Scope, error) {
	group, recursive, err := resolveGroupFlag(e, cmd)
	if err != nil {
		return app.RunnerTagEvaluationSummary{}, domain.Scope{}, err
	}
	ctx := cmd.Context()
	scope, err := app.ResolveScope(ctx, client, group, recursive)
	if err != nil {
		return app.RunnerTagEvaluationSummary{}, domain.Scope{}, wrapProviderErr(err)
	}
	projects, err := app.DiscoverProjects(ctx, client, scope)
	if err != nil {
		return app.RunnerTagEvaluationSummary{}, domain.Scope{}, wrapProviderErr(err)
	}

	summary, err := app.EvaluateRunnerTags(ctx, client, projectIDs(projects), tag)
	if err != nil {
		return app.RunnerTagEvaluationSummary{}, domain.Scope{}, wrapProviderErr(err)
	}
	return summary, scope, nil
}

// projectIDs extracts just the IDs from a project list, for calls that
// only need to know which projects are in scope (e.g. runner discovery).
func projectIDs(projects []domain.Project) []string {
	ids := make([]string, len(projects))
	for i, p := range projects {
		ids[i] = p.ID
	}
	return ids
}

func renderRunnerTagEvaluation(cmd *cobra.Command, e *env, scope domain.Scope, summary app.RunnerTagEvaluationSummary) error {
	matched := summary.Matched()
	rows := make([][]string, 0, len(matched))
	for _, m := range matched {
		rows = append(rows, []string{
			m.Runner.ID, m.Runner.Description, sharedStr(m.Runner.Shared),
			fmt.Sprintf("%d", len(m.Runner.OutOfScopeProjectPaths)), joinReasons(m.Reasons),
		})
	}
	table := output.Table{
		Headers: []string{"ID", "Description", "Shared", "Out-of-Scope Projects", "Reasons"},
		Rows:    rows,
		Footer: fmt.Sprintf("Evaluated %d, matched %d, total out-of-scope impact %d, in scope %s",
			summary.Discovered(), len(matched), summary.TotalOutOfScopeImpact(), scope.Path),
	}
	return output.Render(cmd.OutOrStdout(), e.format, table, summary)
}

func sharedStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
