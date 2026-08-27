package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/domehahn/housekeeping/internal/app"
	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/output"
	"github.com/domehahn/housekeeping/internal/policy/project"
	"github.com/domehahn/housekeeping/internal/provider"
)

func newPipelinesCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipelines",
		Short: "Discover, evaluate, and plan CI tag additions to .gitlab-ci.yml across a scope",
		Long: `pipelines manages adding a CI runner tag to projects' .gitlab-ci.yml.

It ensures the tag is present in the document-wide default.tags block
(creating it if missing), and appends the tag to any job that already
defines its own tags: list (so a job overriding default: still gets it).
A job with no tags: of its own is deliberately left untouched - it
already inherits from default:.

Changes are never committed directly: "pipelines plan" followed by
"execute --apply" opens one Merge Request per affected project. A human
always reviews and merges it - nothing here merges automatically.

See docs/adr/0005-ci-tag-management-scope.md for the full rationale and
known limitations (e.g. jobs defined only via include: from another file
or project are not covered).`,
	}
	cmd.AddCommand(newPipelinesListCmd(e), newPipelinesEvaluateCmd(e), newPipelinesPlanCmd(e))
	return cmd
}

func newPipelinesListCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List projects in scope and whether each has a .gitlab-ci.yml",
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

			presence := app.DiscoverPipelineConfigs(ctx, client, projects, e.cfg.Performance.Workers)
			rows := make([][]string, 0, len(presence))
			for _, p := range presence {
				status := "no"
				if p.Err != nil {
					status = "error: " + p.Err.Error()
				} else if p.Exists {
					status = "yes"
				}
				rows = append(rows, []string{p.Project.ID, p.Project.FullPath, status})
			}
			table := output.Table{
				Headers: []string{"ID", "Path", "Has .gitlab-ci.yml"},
				Rows:    rows,
				Footer:  fmt.Sprintf("%d project(s) discovered in %s", len(presence), scope.Path),
			}
			return output.Render(cmd.OutOrStdout(), e.format, table, presence)
		},
	}
}

func newPipelinesEvaluateCmd(e *env) *cobra.Command {
	var tag string
	cmd := &cobra.Command{
		Use:   "evaluate",
		Short: "Check every project's .gitlab-ci.yml for a CI tag, without producing a plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tag == "" {
				return exitErr(ExitInvalidConfiguration, fmt.Errorf("--tag is required"))
			}
			client, err := e.requireClient()
			if err != nil {
				return err
			}
			summary, scope, err := runPipelineTagEvaluation(cmd, e, client, tag)
			if err != nil {
				return err
			}
			return renderPipelineTagEvaluation(cmd, e, scope, summary)
		},
	}
	cmd.Flags().StringVar(&tag, "tag", "", "the CI tag to check for/add (required)")
	return cmd
}

func newPipelinesPlanCmd(e *env) *cobra.Command {
	var tag, outputPlan string
	var maxActions, maxPercentage int
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Evaluate .gitlab-ci.yml tags and produce a reviewable, saveable plan of Merge Request proposals",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tag == "" {
				return exitErr(ExitInvalidConfiguration, fmt.Errorf("--tag is required"))
			}
			client, err := e.requireClient()
			if err != nil {
				return err
			}
			summary, scope, err := runPipelineTagEvaluation(cmd, e, client, tag)
			if err != nil {
				return err
			}

			info, err := client.Info(cmd.Context())
			if err != nil {
				return wrapProviderErr(err)
			}

			matched := summary.Matched()
			plan := app.BuildPipelineTagPlan(info.Provider, info.Instance, scope, matched, tag, e.clock)

			limits := resolveSafetyLimits(e, maxActions, maxPercentage, domain.ResourceTypePipelineConfig)
			discovered := map[domain.ResourceType]int{domain.ResourceTypePipelineConfig: summary.Discovered()}
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

			if err := renderPipelineTagEvaluation(cmd, e, scope, summary); err != nil {
				return err
			}
			return output.Render(cmd.OutOrStdout(), e.format, planTable(plan), plan)
		},
	}
	cmd.Flags().StringVar(&tag, "tag", "", "the CI tag to check for/add (required)")
	cmd.Flags().StringVar(&outputPlan, "output-plan", "", "write the plan as JSON to this path")
	cmd.Flags().IntVar(&maxActions, "max-actions", 0, "override safety.max_actions.pipeline_tags (must be explicit)")
	cmd.Flags().IntVar(&maxPercentage, "max-percentage", 0, "override safety.max_percentage.pipeline_tags (must be explicit)")
	return cmd
}

func runPipelineTagEvaluation(cmd *cobra.Command, e *env, client provider.Client, tag string) (app.PipelineTagEvaluationSummary, domain.Scope, error) {
	group, recursive, err := resolveGroupFlag(e, cmd)
	if err != nil {
		return app.PipelineTagEvaluationSummary{}, domain.Scope{}, err
	}
	ctx := cmd.Context()
	scope, err := app.ResolveScope(ctx, client, group, recursive)
	if err != nil {
		return app.PipelineTagEvaluationSummary{}, domain.Scope{}, wrapProviderErr(err)
	}
	projects, err := app.DiscoverProjects(ctx, client, scope)
	if err != nil {
		return app.PipelineTagEvaluationSummary{}, domain.Scope{}, wrapProviderErr(err)
	}

	protection, err := project.NewProtection(e.cfg.Projects.Protection.Paths, e.cfg.Projects.Protection.Regex)
	if err != nil {
		return app.PipelineTagEvaluationSummary{}, domain.Scope{}, exitErr(ExitInvalidConfiguration, err)
	}

	summary := app.EvaluatePipelineTags(ctx, client, projects, tag, protection, e.cfg.Performance.Workers)
	summary.Scope = scope
	return summary, scope, nil
}

func renderPipelineTagEvaluation(cmd *cobra.Command, e *env, scope domain.Scope, summary app.PipelineTagEvaluationSummary) error {
	matched := summary.Matched()
	rows := make([][]string, 0, len(matched))
	for _, m := range matched {
		rows = append(rows, []string{m.Project.ID, m.Project.FullPath, joinReasons(m.Reasons)})
	}
	table := output.Table{
		Headers: []string{"ID", "Path", "Reasons"},
		Rows:    rows,
		Footer: fmt.Sprintf("Evaluated %d, matched %d, protected %d in scope %s",
			summary.Discovered(), len(matched), len(summary.Protected()), scope.Path),
	}
	return output.Render(cmd.OutOrStdout(), e.format, table, summary)
}
