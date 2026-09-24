package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/domehahn/housekeeping/internal/app"
	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/output"
	"github.com/domehahn/housekeeping/pkg/version"
)

// newBranchesCmd exposes branch discovery and deletion as a separate command
// family because branches belong to one project, not to a group policy.
func newBranchesCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{Use: "branches", Short: "List and plan deletion of project branches"}
	cmd.AddCommand(newBranchesListCmd(e), newBranchesPlanCmd(e))
	return cmd
}

func newBranchesListCmd(e *env) *cobra.Command {
	var projectID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List branches of a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(projectID) == "" {
				return exitErr(ExitInvalidConfiguration, fmt.Errorf("--project is required"))
			}
			client, err := e.requireClient()
			if err != nil {
				return err
			}
			project, err := client.GetProject(cmd.Context(), projectID)
			if err != nil {
				return wrapProviderErr(err)
			}
			branches, err := client.ListBranches(cmd.Context(), project.ID)
			if err != nil {
				return wrapProviderErr(err)
			}
			return renderBranches(cmd, e, project, branches)
		},
	}
	cmd.Flags().StringVar(&projectID, "project", "", "project ID or full path")
	return cmd
}

func newBranchesPlanCmd(e *env) *cobra.Command {
	var projectID string
	var selected []string
	var merged bool
	var outputPath string
	var maxActions, maxPercentage int
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Create a reviewable plan to delete selected project branches",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(projectID) == "" {
				return exitErr(ExitInvalidConfiguration, fmt.Errorf("--project is required"))
			}
			if len(selected) == 0 && !merged {
				return exitErr(ExitInvalidConfiguration, fmt.Errorf("select branches with --branch or use --merged"))
			}
			client, err := e.requireClient()
			if err != nil {
				return err
			}
			project, err := client.GetProject(cmd.Context(), projectID)
			if err != nil {
				return wrapProviderErr(err)
			}
			branches, err := client.ListBranches(cmd.Context(), project.ID)
			if err != nil {
				return wrapProviderErr(err)
			}

			selectedSet := map[string]bool{}
			for _, name := range selected {
				selectedSet[name] = true
			}
			var targets []domain.Branch
			found := map[string]bool{}
			for _, branch := range branches {
				if selectedSet[branch.Name] {
					found[branch.Name] = true
				}
				if (selectedSet[branch.Name] || (merged && branch.Merged)) && !branch.Protected && !branch.Default {
					targets = append(targets, branch)
				}
			}
			for _, name := range selected {
				if !found[name] {
					return exitErr(ExitInvalidConfiguration, fmt.Errorf("branch %q was not found in project %s", name, project.FullPath))
				}
			}

			info, err := client.Info(cmd.Context())
			if err != nil {
				return wrapProviderErr(err)
			}
			plan := app.BuildBranchDeletionPlan(info.Provider, info.Instance, project, targets, e.clock)
			// Branch actions reuse the project safety bucket for backwards
			// compatible configuration. The denominator is the project's
			// branch population, not the single containing project.
			plan.Discovery = map[domain.ResourceType]int{domain.ResourceTypeProject: len(branches)}
			plan.ExpiresAt = e.clock.Now().Add(24 * time.Hour)
			plan.Provenance = &domain.PlanProvenance{ToolVersion: version.Version, ToolCommit: version.Commit, ConfigDigest: e.cfg.ComputeConfigDigest()}
			limits, err := resolveSafetyLimits(e, maxActions, maxPercentage, domain.ResourceTypeProject)
			if err != nil {
				return exitErr(ExitInvalidConfiguration, err)
			}
			if violations := app.CheckGuards(plan, plan.Discovery, limits); len(violations) > 0 {
				for _, violation := range violations {
					cmd.PrintErrln("SAFETY GUARD:", violation)
				}
				return exitErr(ExitSafetyGuardTriggered, fmt.Errorf("branch deletion plan exceeds configured safety guards"))
			}
			if outputPath != "" {
				if err := app.SavePlan(outputPath, plan); err != nil {
					return exitErr(ExitGeneralError, err)
				}
				cmd.Printf("Plan written to %s (%d branch action(s)).\n", outputPath, len(plan.Actions))
			}
			return output.Render(cmd.OutOrStdout(), e.format, planTable(plan), plan)
		},
	}
	cmd.Flags().StringVar(&projectID, "project", "", "project ID or full path")
	cmd.Flags().StringArrayVar(&selected, "branch", nil, "branch name to delete; repeatable")
	cmd.Flags().BoolVar(&merged, "merged", false, "select all merged, non-protected, non-default branches")
	cmd.Flags().StringVar(&outputPath, "output-plan", "", "write the plan as JSON to this path")
	cmd.Flags().IntVar(&maxActions, "max-actions", 0, "override safety.max_actions.projects (must be explicit)")
	cmd.Flags().IntVar(&maxPercentage, "max-percentage", 0, "override safety.max_percentage.projects (must be explicit)")
	return cmd
}

func renderBranches(cmd *cobra.Command, e *env, project domain.Project, branches []domain.Branch) error {
	rows := make([][]string, 0, len(branches))
	for _, branch := range branches {
		rows = append(rows, []string{branch.Name, boolStr(branch.Protected), boolStr(branch.Default), boolStr(branch.Merged), branch.WebURL})
	}
	return output.Render(cmd.OutOrStdout(), e.format, output.Table{
		Headers: []string{"Branch", "Protected", "Default", "Merged", "URL"},
		Rows:    rows,
		Footer:  fmt.Sprintf("%d branch(es) in %s", len(branches), project.FullPath),
	}, branches)
}
