package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/domehahn/housekeeping/internal/app"
	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/output"
	"github.com/domehahn/housekeeping/internal/provider"
)

func newPipelinesAnalyzeCmd(e *env) *cobra.Command {
	var tags, includes, excludes []string
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze GitLab's effective include-expanded CI configuration",
		Long: `analyze asks GitLab to resolve the project's existing CI configuration,
including local/project/remote includes, then reports effective tag coverage.
It is read-only and never modifies an included source.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			normalizedTags, err := app.NormalizeTags(tags)
			if err != nil {
				return exitErr(ExitInvalidConfiguration, err)
			}
			client, err := e.requireClient()
			if err != nil {
				return err
			}
			projects, scope, err := discoverSelectedPipelineProjects(cmd, e, client, includes, excludes)
			if err != nil {
				return err
			}
			results := app.AnalyzeMergedPipelineTags(cmd.Context(), client, projects, normalizedTags, e.cfg.Performance.Workers)
			rows := make([][]string, 0, len(results))
			for _, result := range results {
				rows = append(rows, []string{
					result.Project.ID, result.Project.FullPath, string(result.Status), fmt.Sprintf("%d", len(result.Includes)), joinReasons(result.Reasons),
				})
			}
			table := output.Table{
				Headers: []string{"ID", "Path", "Effective status", "Includes", "Reasons"},
				Rows:    rows,
				Footer:  fmt.Sprintf("Analyzed effective CI configuration for %d project(s) in %s; no files were modified", len(results), scope.Path),
			}
			return output.Render(cmd.OutOrStdout(), e.format, table, results)
		},
	}
	addPipelineSelectionFlags(cmd, &tags, &includes, &excludes)
	return cmd
}

func newPipelinesProposalsCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{Use: "proposals", Short: "Inspect scm-cleaner pipeline-tag Merge Requests"}
	cmd.AddCommand(newPipelinesProposalsStatusCmd(e))
	return cmd
}

func newPipelinesProposalsStatusCmd(e *env) *cobra.Command {
	var tags, includes, excludes []string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show open, merged, and closed pipeline-tag proposals",
		RunE: func(cmd *cobra.Command, args []string) error {
			normalizedTags, err := app.NormalizeTags(tags)
			if err != nil {
				return exitErr(ExitInvalidConfiguration, err)
			}
			client, err := e.requireClient()
			if err != nil {
				return err
			}
			projects, scope, err := discoverSelectedPipelineProjects(cmd, e, client, includes, excludes)
			if err != nil {
				return err
			}
			statuses := app.DiscoverPipelineProposalStatuses(cmd.Context(), client, projects, normalizedTags, e.cfg.Performance.Workers)
			rows := make([][]string, 0, len(statuses))
			counts := map[string]int{}
			for _, status := range statuses {
				state, url := "none", ""
				if status.Error != "" {
					state = "error"
					url = status.Error
				} else if status.Proposal != nil {
					state = status.Proposal.State
					url = status.Proposal.URL
				}
				counts[state]++
				rows = append(rows, []string{status.Project.ID, status.Project.FullPath, state, url})
			}
			table := output.Table{
				Headers: []string{"ID", "Path", "Proposal state", "URL / Error"},
				Rows:    rows,
				Footer: fmt.Sprintf("Latest proposals in %s: opened=%d merged=%d closed=%d locked=%d none=%d errors=%d",
					scope.Path, counts["opened"], counts["merged"], counts["closed"], counts["locked"], counts["none"], counts["error"]),
			}
			return output.Render(cmd.OutOrStdout(), e.format, table, statuses)
		},
	}
	addPipelineSelectionFlags(cmd, &tags, &includes, &excludes)
	return cmd
}

func discoverSelectedPipelineProjects(cmd *cobra.Command, e *env, client provider.Client, includes, excludes []string) ([]domain.Project, domain.Scope, error) {
	group, recursive, err := resolveGroupFlag(e, cmd)
	if err != nil {
		return nil, domain.Scope{}, err
	}
	scope, err := app.ResolveScope(cmd.Context(), client, group, recursive)
	if err != nil {
		return nil, domain.Scope{}, wrapProviderErr(err)
	}
	projects, err := app.DiscoverProjects(cmd.Context(), client, scope)
	if err != nil {
		return nil, domain.Scope{}, wrapProviderErr(err)
	}
	projects, err = app.FilterPipelineProjects(projects, includes, excludes)
	if err != nil {
		return nil, domain.Scope{}, exitErr(ExitInvalidConfiguration, err)
	}
	return projects, scope, nil
}
