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
		Short: "Discover, evaluate, and plan CI tag additions to runners available to a scope",
		Long: `runners manages adding a CI tag directly to the tag_list of runners available to
projects in a scope (via the GitLab Runner API), as opposed to
"pipelines", which edits .gitlab-ci.yml files.

Project-runner assignments can be enumerated explicitly. Group runners are
actionable only when the owning group is inside a recursive scope; inherited
ancestor-group runners, instance runners, and any runner whose effective reach
cannot be proven are reported as blocked and omitted from plans. Explicit
out-of-scope project assignments additionally require
--confirm-out-of-scope-impact=<N> at execution time. See
docs/adr/0005-ci-tag-management-scope.md.

Use --replace-tag OLD:NEW instead of --tag to correct a wrong tag already
rolled out to a runner's tag_list (e.g. a case typo like "AKS" vs "aks");
the same out-of-scope-impact guard applies.`,
	}
	cmd.AddCommand(newRunnersListCmd(e), newRunnersEvaluateCmd(e), newRunnersPlanCmd(e))
	return cmd
}

func newRunnersListCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List runners available to projects in scope, with tags and impact status",
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

			runners, err := client.ListRunnersForProjects(ctx, scope, projectIDs(projects))
			if err != nil {
				return wrapProviderErr(err)
			}

			rows := make([][]string, 0, len(runners))
			for _, r := range runners {
				rows = append(rows, []string{
					r.ID, r.Description, r.RunnerType, sharedStr(r.Shared), joinReasons(r.TagList),
					fmt.Sprintf("%d", len(r.OutOfScopeProjectPaths)), impactKnownStr(r),
				})
			}
			table := output.Table{
				Headers: []string{"ID", "Description", "Type", "Shared", "Tags", "Out-of-Scope Projects", "Impact"},
				Rows:    rows,
				Footer:  fmt.Sprintf("%d runner(s) available to projects in %s", len(runners), scope.Path),
			}
			return output.Render(cmd.OutOrStdout(), e.format, table, runners)
		},
	}
}

func newRunnersEvaluateCmd(e *env) *cobra.Command {
	var tag string
	var replaceTags []string
	cmd := &cobra.Command{
		Use:   "evaluate",
		Short: "Check runners available to a scope for a CI tag, without producing a plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireExactlyOneTagMode(singleTagSlice(tag), replaceTags); err != nil {
				return exitErr(ExitInvalidConfiguration, err)
			}
			client, err := e.requireClient()
			if err != nil {
				return err
			}
			if len(replaceTags) > 0 {
				renames, err := parseTagRenames(replaceTags)
				if err != nil {
					return exitErr(ExitInvalidConfiguration, err)
				}
				summary, scope, err := runRunnerTagRenameEvaluation(cmd, e, client, renames)
				if err != nil {
					return err
				}
				return renderRunnerTagRenameEvaluation(cmd, e, scope, summary)
			}
			summary, scope, err := runRunnerTagEvaluation(cmd, e, client, tag)
			if err != nil {
				return err
			}
			return renderRunnerTagEvaluation(cmd, e, scope, summary)
		},
	}
	addRunnerTagFlags(cmd, &tag, &replaceTags)
	return cmd
}

func newRunnersPlanCmd(e *env) *cobra.Command {
	var tag, outputPlan string
	var replaceTags []string
	var maxActions, maxPercentage int
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Evaluate runner tags and produce a reviewable, saveable plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireExactlyOneTagMode(singleTagSlice(tag), replaceTags); err != nil {
				return exitErr(ExitInvalidConfiguration, err)
			}
			client, err := e.requireClient()
			if err != nil {
				return err
			}

			info, err := client.Info(cmd.Context())
			if err != nil {
				return wrapProviderErr(err)
			}

			var plan domain.Plan
			var scope domain.Scope

			if len(replaceTags) > 0 {
				renames, err := parseTagRenames(replaceTags)
				if err != nil {
					return exitErr(ExitInvalidConfiguration, err)
				}
				summary, s, err := runRunnerTagRenameEvaluation(cmd, e, client, renames)
				if err != nil {
					return err
				}
				scope = s
				plan = app.BuildRunnerTagRenamePlan(info.Provider, info.Instance, scope, summary.Matched(), renames, e.clock)

				limits := resolveSafetyLimits(e, maxActions, maxPercentage, domain.ResourceTypeRunner)
				discoveredMap := map[domain.ResourceType]int{domain.ResourceTypeRunner: summary.Discovered()}
				if violations := app.CheckGuards(plan, discoveredMap, limits); len(violations) > 0 {
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
				if err := renderRunnerTagRenameEvaluation(cmd, e, scope, summary); err != nil {
					return err
				}
				return output.Render(cmd.OutOrStdout(), e.format, planTable(plan), plan)
			}

			summary, s, err := runRunnerTagEvaluation(cmd, e, client, tag)
			if err != nil {
				return err
			}
			scope = s

			matched := summary.Matched()
			plan = app.BuildRunnerTagPlan(info.Provider, info.Instance, scope, matched, tag, e.clock)

			limits := resolveSafetyLimits(e, maxActions, maxPercentage, domain.ResourceTypeRunner)
			discoveredMap := map[domain.ResourceType]int{domain.ResourceTypeRunner: summary.Discovered()}
			if violations := app.CheckGuards(plan, discoveredMap, limits); len(violations) > 0 {
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
	addRunnerTagFlags(cmd, &tag, &replaceTags)
	cmd.Flags().StringVar(&outputPlan, "output-plan", "", "write the plan as JSON to this path")
	cmd.Flags().IntVar(&maxActions, "max-actions", 0, "override safety.max_actions.runner_tags (must be explicit)")
	cmd.Flags().IntVar(&maxPercentage, "max-percentage", 0, "override safety.max_percentage.runner_tags (must be explicit)")
	return cmd
}

func addRunnerTagFlags(cmd *cobra.Command, tag *string, replaceTags *[]string) {
	cmd.Flags().StringVar(tag, "tag", "", "the CI tag to check for/add; mutually exclusive with --replace-tag (one of the two is required)")
	cmd.Flags().StringArrayVar(replaceTags, "replace-tag", nil, "OLD:NEW - correct a wrong CI tag already rolled out to a runner's tag_list; repeat for multiple corrections; mutually exclusive with --tag")
}

// singleTagSlice adapts the runners commands' single --tag string flag to
// the shared []string-based requireExactlyOneTagMode check.
func singleTagSlice(tag string) []string {
	if tag == "" {
		return nil
	}
	return []string{tag}
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

	summary, err := app.EvaluateRunnerTags(ctx, client, scope, projectIDs(projects), tag)
	if err != nil {
		return app.RunnerTagEvaluationSummary{}, domain.Scope{}, wrapProviderErr(err)
	}
	return summary, scope, nil
}

func runRunnerTagRenameEvaluation(cmd *cobra.Command, e *env, client provider.Client, renames []domain.TagRename) (app.RunnerTagRenameEvaluationSummary, domain.Scope, error) {
	group, recursive, err := resolveGroupFlag(e, cmd)
	if err != nil {
		return app.RunnerTagRenameEvaluationSummary{}, domain.Scope{}, err
	}
	ctx := cmd.Context()
	scope, err := app.ResolveScope(ctx, client, group, recursive)
	if err != nil {
		return app.RunnerTagRenameEvaluationSummary{}, domain.Scope{}, wrapProviderErr(err)
	}
	projects, err := app.DiscoverProjects(ctx, client, scope)
	if err != nil {
		return app.RunnerTagRenameEvaluationSummary{}, domain.Scope{}, wrapProviderErr(err)
	}

	summary, err := app.EvaluateRunnerTagRename(ctx, client, scope, projectIDs(projects), renames)
	if err != nil {
		return app.RunnerTagRenameEvaluationSummary{}, domain.Scope{}, wrapProviderErr(err)
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
	rows := make([][]string, 0, len(summary.Results))
	for _, m := range summary.Results {
		if !m.Missing {
			continue
		}
		status := "actionable"
		if m.Blocked {
			status = "blocked"
		}
		rows = append(rows, []string{
			m.Runner.ID, m.Runner.Description, sharedStr(m.Runner.Shared),
			status, fmt.Sprintf("%d", len(m.Runner.OutOfScopeProjectPaths)), joinReasons(m.Reasons),
		})
	}
	table := output.Table{
		Headers: []string{"ID", "Description", "Shared", "Status", "Out-of-Scope Projects", "Reasons"},
		Rows:    rows,
		Footer: fmt.Sprintf("Evaluated %d, matched %d, total out-of-scope impact %d, in scope %s",
			summary.Discovered(), len(matched), summary.TotalOutOfScopeImpact(), scope.Path),
	}
	return output.Render(cmd.OutOrStdout(), e.format, table, summary)
}

func renderRunnerTagRenameEvaluation(cmd *cobra.Command, e *env, scope domain.Scope, summary app.RunnerTagRenameEvaluationSummary) error {
	matched := summary.Matched()
	rows := make([][]string, 0, len(summary.Results))
	for _, m := range summary.Results {
		if !m.NeedsRename {
			continue
		}
		status := "actionable"
		if m.Blocked {
			status = "blocked"
		}
		rows = append(rows, []string{
			m.Runner.ID, m.Runner.Description, sharedStr(m.Runner.Shared),
			status, fmt.Sprintf("%d", len(m.Runner.OutOfScopeProjectPaths)), joinReasons(m.Reasons),
		})
	}
	table := output.Table{
		Headers: []string{"ID", "Description", "Shared", "Status", "Out-of-Scope Projects", "Reasons"},
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

func impactKnownStr(r domain.Runner) string {
	if r.ImpactKnown {
		return "known"
	}
	return "blocked: " + r.ImpactReason
}
