package cli

import (
	"fmt"
	"maps"
	"path/filepath"
	"strings"

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
		Long: `pipelines manages adding one or more CI runner tags to projects' .gitlab-ci.yml.

It ensures every requested tag is present in the document-wide default.tags
block (creating it if missing), and appends missing tags to any job that
already defines its own tags: list (so a job overriding default: still gets them).
A job with no tags: of its own is deliberately left untouched - it
already inherits from default:.

Changes are never committed directly: "pipelines plan" followed by
"execute --apply" opens one Merge Request per affected project. A human
always reviews and merges it - nothing here merges automatically.

Use "pipelines analyze" to inspect GitLab's include-expanded effective
configuration. Included sources are never modified. See
docs/adr/0005-ci-tag-management-scope.md for the full rationale.`,
	}
	cmd.AddCommand(newPipelinesListCmd(e), newPipelinesEvaluateCmd(e), newPipelinesAnalyzeCmd(e), newPipelinesPlanCmd(e), newPipelinesProposalsCmd(e))
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
	var tags, includeProjects, excludeProjects []string
	cmd := &cobra.Command{
		Use:   "evaluate",
		Short: "Check every project's .gitlab-ci.yml for CI tags, without producing a plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			normalizedTags, err := app.NormalizeTags(tags)
			if err != nil {
				return exitErr(ExitInvalidConfiguration, err)
			}
			client, err := e.requireClient()
			if err != nil {
				return err
			}
			summary, scope, err := runPipelineTagEvaluation(cmd, e, client, normalizedTags, includeProjects, excludeProjects)
			if err != nil {
				return err
			}
			return renderPipelineTagEvaluation(cmd, e, scope, summary)
		},
	}
	addPipelineSelectionFlags(cmd, &tags, &includeProjects, &excludeProjects)
	return cmd
}

func newPipelinesPlanCmd(e *env) *cobra.Command {
	var tags, includeProjects, excludeProjects []string
	var outputPlan string
	var maxActions, maxPercentage, batchSize int
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Evaluate .gitlab-ci.yml tags and produce a reviewable, saveable plan of Merge Request proposals",
		RunE: func(cmd *cobra.Command, args []string) error {
			normalizedTags, err := app.NormalizeTags(tags)
			if err != nil {
				return exitErr(ExitInvalidConfiguration, err)
			}
			if batchSize < 0 {
				return exitErr(ExitInvalidConfiguration, fmt.Errorf("--batch-size must not be negative"))
			}
			if batchSize > 0 && outputPlan == "" {
				return exitErr(ExitInvalidConfiguration, fmt.Errorf("--batch-size requires --output-plan"))
			}
			client, err := e.requireClient()
			if err != nil {
				return err
			}
			summary, scope, err := runPipelineTagEvaluation(cmd, e, client, normalizedTags, includeProjects, excludeProjects)
			if err != nil {
				return err
			}

			info, err := client.Info(cmd.Context())
			if err != nil {
				return wrapProviderErr(err)
			}

			matched := summary.Matched()
			plan := app.BuildPipelineTagPlan(info.Provider, info.Instance, scope, matched, normalizedTags, e.clock)

			limits := resolveSafetyLimits(e, maxActions, maxPercentage, domain.ResourceTypePipelineConfig)
			discovered := map[domain.ResourceType]int{domain.ResourceTypePipelineConfig: summary.Discovered()}
			plans := []domain.Plan{plan}
			if batchSize > 0 {
				plans, err = app.BatchPlan(plan, batchSize)
				if err != nil {
					return exitErr(ExitInvalidConfiguration, err)
				}
				percentageLimits := limits
				percentageLimits.Limits = maps.Clone(limits.Limits)
				limit := percentageLimits.Limits[domain.ResourceTypePipelineConfig]
				limit.MaxActions = len(plan.Actions)
				percentageLimits.Limits[domain.ResourceTypePipelineConfig] = limit
				if err := reportPipelineGuardViolations(cmd, plan, discovered, percentageLimits); err != nil {
					return err
				}
				for _, batch := range plans {
					if err := reportPipelineGuardViolations(cmd, batch, nil, limits); err != nil {
						return err
					}
				}
			} else if err := reportPipelineGuardViolations(cmd, plan, discovered, limits); err != nil {
				return err
			}

			if outputPlan != "" {
				for index, currentPlan := range plans {
					path := batchPlanPath(outputPlan, index, len(plans))
					if err := app.SavePlan(path, currentPlan); err != nil {
						return exitErr(ExitGeneralError, err)
					}
					cmd.Printf("Plan written to %s (%d action(s)).\n", path, len(currentPlan.Actions))
				}
			}

			if err := renderPipelineTagEvaluation(cmd, e, scope, summary); err != nil {
				return err
			}
			return output.Render(cmd.OutOrStdout(), e.format, planTable(plan), plan)
		},
	}
	addPipelineSelectionFlags(cmd, &tags, &includeProjects, &excludeProjects)
	cmd.Flags().StringVar(&outputPlan, "output-plan", "", "write the plan as JSON to this path")
	cmd.Flags().IntVar(&batchSize, "batch-size", 0, "split matched projects into deterministic plans of at most N actions")
	cmd.Flags().IntVar(&maxActions, "max-actions", 0, "override safety.max_actions.pipeline_tags (must be explicit)")
	cmd.Flags().IntVar(&maxPercentage, "max-percentage", 0, "override safety.max_percentage.pipeline_tags (must be explicit)")
	return cmd
}

func runPipelineTagEvaluation(cmd *cobra.Command, e *env, client provider.Client, tags, includeProjects, excludeProjects []string) (app.PipelineTagEvaluationSummary, domain.Scope, error) {
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
	projects, err = app.FilterPipelineProjects(projects, includeProjects, excludeProjects)
	if err != nil {
		return app.PipelineTagEvaluationSummary{}, domain.Scope{}, exitErr(ExitInvalidConfiguration, err)
	}

	protection, err := project.NewProtection(e.cfg.Projects.Protection.Paths, e.cfg.Projects.Protection.Regex)
	if err != nil {
		return app.PipelineTagEvaluationSummary{}, domain.Scope{}, exitErr(ExitInvalidConfiguration, err)
	}

	summary := app.EvaluatePipelineTags(ctx, client, projects, tags, protection, e.cfg.Performance.Workers)
	summary.Scope = scope
	return summary, scope, nil
}

func addPipelineSelectionFlags(cmd *cobra.Command, tags, includes, excludes *[]string) {
	cmd.Flags().StringArrayVar(tags, "tag", nil, "CI tag to check for/add; repeat for multiple tags (required)")
	cmd.Flags().StringArrayVar(includes, "include-project", nil, "include project full paths matching this regex; repeatable")
	cmd.Flags().StringArrayVar(excludes, "exclude-project", nil, "exclude project full paths matching this regex; repeatable; takes precedence")
}

func reportPipelineGuardViolations(cmd *cobra.Command, plan domain.Plan, discovered map[domain.ResourceType]int, limits app.SafetyLimits) error {
	violations := app.CheckGuards(plan, discovered, limits)
	if len(violations) == 0 {
		return nil
	}
	for _, violation := range violations {
		cmd.PrintErrln("SAFETY GUARD:", violation.Error())
	}
	return exitErr(ExitSafetyGuardTriggered, fmt.Errorf("plan exceeds configured safety guards; adjust policy, batching, filters, or pass an explicit override"))
}

func batchPlanPath(path string, index, total int) string {
	if total <= 1 {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	return fmt.Sprintf("%s-%03d%s", base, index+1, ext)
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
