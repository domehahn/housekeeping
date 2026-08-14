package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/domehahn/housekeeping/internal/app"
	"github.com/domehahn/housekeeping/internal/audit"
	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/output"
)

func newExecuteCmd(e *env) *cobra.Command {
	var apply, nonInteractive bool
	var confirmScope string
	var maxActions, maxPercentage int

	cmd := &cobra.Command{
		Use:   "execute <plan-file>",
		Short: "Simulate (default) or apply (--apply) a saved cleanup plan",
		Args:  cobra.ExactArgs(1),
		Long: `execute reads a plan file produced by "projects plan" or "users plan" and
carries out its actions.

Without --apply, every action is only simulated: nothing is changed. This
is the default and cannot be bypassed accidentally.

With --apply, actions are actually performed. In an interactive terminal
this requires typing an explicit confirmation phrase; in a non-interactive
context (--non-interactive, or stdin is not a TTY) you must additionally
pass --confirm-scope matching the plan's scope path, as an extra guard
against unattended misfires (e.g. a misconfigured CI job).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			planPath := args[0]
			plan, err := app.LoadPlan(planPath)
			if err != nil {
				return exitErr(ExitPlanValidationFailed, err)
			}

			client, err := e.requireClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			info, err := client.Info(ctx)
			if err != nil {
				return wrapProviderErr(err)
			}
			if err := app.VerifyAgainstInstance(plan, info.Provider, info.Instance); err != nil {
				return exitErr(ExitPlanValidationFailed, err)
			}

			isProjects := resourceMajority(plan) == domain.ResourceTypeProject
			limits := resolveSafetyLimits(e, maxActions, maxPercentage, isProjects)
			// The percentage guard needs a "discovered" total that a plan
			// file alone does not carry (that check already ran, against
			// the real total, when the plan was created) - so it is
			// deliberately disabled here and only the absolute max-actions
			// guard, which needs no discovered count, is re-checked.
			limits.MaxPercentageProjects, limits.MaxPercentageUsers = 0, 0
			if violations := app.CheckGuards(plan, 0, 0, limits); len(violations) > 0 {
				for _, v := range violations {
					cmd.PrintErrln("SAFETY GUARD:", v.Error())
				}
				return exitErr(ExitSafetyGuardTriggered, fmt.Errorf("plan exceeds configured maximum action count"))
			}

			if apply {
				if err := confirmApply(cmd, plan, nonInteractive, confirmScope); err != nil {
					return err
				}
			} else {
				cmd.Println("Dry run: no changes will be made. Pass --apply to execute for real.")
			}

			auditLog, err := e.auditWriter()
			if err != nil {
				return err
			}
			defer auditLog.Close()

			summary := app.Execute(ctx, client, plan, app.ExecuteOptions{
				Apply:      apply,
				Revalidate: e.cfg.Execution.Revalidate,
				FailFast:   e.cfg.Execution.FailFast,
			})

			for _, o := range summary.Outcomes {
				rec := audit.FromOutcome(e.clock.Now(), plan.Provider, plan.Instance, plan.Scope.Path, o)
				_ = auditLog.Write(rec)
			}

			if err := renderExecutionSummary(cmd, e, summary); err != nil {
				return err
			}

			if summary.AllFailed() {
				return exitErr(ExitGeneralError, fmt.Errorf("all %d action(s) failed", len(summary.Outcomes)))
			}
			if summary.Partial() {
				return exitErr(ExitPartialExecution, fmt.Errorf("execution completed with %d failure(s) out of %d action(s)", summary.CountByResult(domain.ResultFailed), len(summary.Outcomes)))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&apply, "apply", false, "actually perform the planned actions (default is a dry run)")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "never prompt; requires --confirm-scope when combined with --apply")
	cmd.Flags().StringVar(&confirmScope, "confirm-scope", "", "must equal the plan's scope path; required for --apply --non-interactive")
	cmd.Flags().IntVar(&maxActions, "max-actions", 0, "override the safety.max_actions guard for this run's resource type (must be explicit)")
	cmd.Flags().IntVar(&maxPercentage, "max-percentage", 0, "override the safety.max_percentage guard for this run's resource type (must be explicit)")
	return cmd
}

func resourceMajority(plan domain.Plan) domain.ResourceType {
	projects, users := 0, 0
	for _, a := range plan.Actions {
		if a.ResourceType == domain.ResourceTypeProject {
			projects++
		} else {
			users++
		}
	}
	if projects >= users {
		return domain.ResourceTypeProject
	}
	return domain.ResourceTypeUser
}

// confirmApply enforces the interactive/non-interactive confirmation
// requirements before any destructive call is made.
func confirmApply(cmd *cobra.Command, plan domain.Plan, nonInteractive bool, confirmScope string) error {
	interactive := !nonInteractive && isTerminal(os.Stdin)

	if !interactive {
		if confirmScope == "" {
			return exitErr(ExitGeneralError, fmt.Errorf("--apply in a non-interactive context requires --confirm-scope=%q", plan.Scope.Path))
		}
		if confirmScope != plan.Scope.Path {
			return exitErr(ExitGeneralError, fmt.Errorf("--confirm-scope %q does not match plan scope %q", confirmScope, plan.Scope.Path))
		}
		return nil
	}

	cmd.Println("WARNING")
	cmd.Println()
	cmd.Printf("You are about to execute %d action(s).\n\n", len(plan.Actions))
	cmd.Printf("Provider: %s\nInstance: %s\nScope:    %s\n\n", plan.Provider, plan.Instance, plan.Scope.Path)
	cmd.Println("This operation may be irreversible.")
	cmd.Println()
	phrase := fmt.Sprintf("apply %d actions", len(plan.Actions))
	cmd.Printf("Type %q to continue: ", phrase)

	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	if strings.TrimSpace(line) != phrase {
		return exitErr(ExitGeneralError, fmt.Errorf("confirmation phrase did not match; aborting"))
	}
	return nil
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func renderExecutionSummary(cmd *cobra.Command, e *env, summary app.ExecutionSummary) error {
	rows := make([][]string, 0, len(summary.Outcomes))
	for _, o := range summary.Outcomes {
		rows = append(rows, []string{string(o.Action.ResourceType), o.Action.ResourceID, o.Action.ResourceName, string(o.Action.Action), string(o.Result), o.Detail})
	}
	table := output.Table{
		Headers: []string{"Type", "ID", "Name", "Action", "Result", "Detail"},
		Rows:    rows,
		Footer: fmt.Sprintf("Planned: %d  Success: %d  Dry-run: %d  Skipped: %d  Failed: %d",
			len(summary.Outcomes),
			summary.CountByResult(domain.ResultSuccess),
			summary.CountByResult(domain.ResultDryRun),
			summary.CountByResult(domain.ResultSkippedAlreadyDone)+summary.CountByResult(domain.ResultSkippedRevalidate),
			summary.CountByResult(domain.ResultFailed)),
	}
	return output.Render(cmd.OutOrStdout(), e.format, table, summary)
}
