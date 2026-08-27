package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/domehahn/housekeeping/internal/app"
	"github.com/domehahn/housekeeping/internal/audit"
	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/output"
	projectpolicy "github.com/domehahn/housekeeping/internal/policy/project"
	userpolicy "github.com/domehahn/housekeeping/internal/policy/user"
	"github.com/domehahn/housekeeping/internal/provider"
)

// executeFlags holds every flag `execute` accepts.
type executeFlags struct {
	apply                     bool
	nonInteractive            bool
	confirmScope              string
	maxActions, maxPercentage int
	confirmOutOfScopeImpact   int
}

func newExecuteCmd(e *env) *cobra.Command {
	flags := &executeFlags{}

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
			return runExecute(cmd, e, args[0], flags)
		},
	}

	cmd.Flags().BoolVar(&flags.apply, "apply", false, "actually perform the planned actions (default is a dry run)")
	cmd.Flags().BoolVar(&flags.nonInteractive, "non-interactive", false, "never prompt; requires --confirm-scope when combined with --apply")
	cmd.Flags().StringVar(&flags.confirmScope, "confirm-scope", "", "must equal the plan's scope path; required for --apply --non-interactive")
	cmd.Flags().IntVar(&flags.maxActions, "max-actions", 0, "override the safety.max_actions guard for this run's resource type (must be explicit)")
	cmd.Flags().IntVar(&flags.maxPercentage, "max-percentage", 0, "override the safety.max_percentage guard for this run's resource type (must be explicit)")
	cmd.Flags().IntVar(&flags.confirmOutOfScopeImpact, "confirm-out-of-scope-impact", 0,
		"required, and must exactly equal the plan's total out-of-scope project impact, when the plan contains any runner-tag action affecting a shared runner used outside the evaluated scope")
	return cmd
}

func runExecute(cmd *cobra.Command, e *env, planPath string, flags *executeFlags) error {
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

	if err := checkExecuteSafetyGuards(cmd, e, plan, flags); err != nil {
		return err
	}

	if flags.apply {
		if outOfScope := totalOutOfScopeImpact(plan); outOfScope > 0 {
			if err := confirmOutOfScopeImpact(cmd, plan, outOfScope, flags.confirmOutOfScopeImpact); err != nil {
				return err
			}
		}
		if err := confirmApply(cmd, plan, flags.nonInteractive, flags.confirmScope); err != nil {
			return err
		}
	} else {
		cmd.Println("Dry run: no changes will be made. Pass --apply to execute for real.")
	}

	return runAndReportExecution(cmd, e, ctx, client, plan, flags.apply)
}

// checkExecuteSafetyGuards re-checks the absolute max-actions guard
// against the plan's own action counts. The percentage guard needs a
// "discovered" total that a plan file alone does not carry (that check
// already ran, against the real total, when the plan was created), so it
// is deliberately disabled here.
func checkExecuteSafetyGuards(cmd *cobra.Command, e *env, plan domain.Plan, flags *executeFlags) error {
	limits := resolveSafetyLimits(e, flags.maxActions, flags.maxPercentage, resourceMajority(plan))
	for rt, l := range limits.Limits {
		l.MaxPercentage = 0
		limits.Limits[rt] = l
	}
	violations := app.CheckGuards(plan, nil, limits)
	if len(violations) == 0 {
		return nil
	}
	for _, v := range violations {
		cmd.PrintErrln("SAFETY GUARD:", v.Error())
	}
	return exitErr(ExitSafetyGuardTriggered, fmt.Errorf("plan exceeds configured maximum action count"))
}

func runAndReportExecution(cmd *cobra.Command, e *env, ctx context.Context, client provider.Client, plan domain.Plan, apply bool) error {
	auditLog, err := e.auditWriter()
	if err != nil {
		return err
	}

	projectProtection, err := projectpolicy.NewProtection(e.cfg.Projects.Protection.Paths, e.cfg.Projects.Protection.Regex)
	if err != nil {
		_ = auditLog.Close()
		return exitErr(ExitInvalidConfiguration, err)
	}
	userProtection, err := userpolicy.NewProtection(
		e.cfg.Users.Protection.Usernames,
		e.cfg.Users.Protection.Regex,
		e.cfg.Users.Protection.AccessLevels,
		"", // app.Execute independently resolves and always protects the caller.
	)
	if err != nil {
		_ = auditLog.Close()
		return exitErr(ExitInvalidConfiguration, err)
	}

	summary := app.Execute(ctx, client, plan, app.ExecuteOptions{
		Apply:             apply,
		Revalidate:        e.cfg.Execution.Revalidate,
		FailFast:          e.cfg.Execution.FailFast,
		ProjectProtection: projectProtection,
		UserProtection:    userProtection,
	})

	for _, o := range summary.Outcomes {
		rec := audit.FromOutcome(e.clock.Now(), plan.Provider, plan.Instance, plan.Scope.Path, o)
		if err := auditLog.Write(rec); err != nil {
			_ = auditLog.Close()
			return exitErr(ExitGeneralError, err)
		}
	}
	if err := auditLog.Close(); err != nil {
		return exitErr(ExitGeneralError, fmt.Errorf("close audit log: %w", err))
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
}

// resourceMajority picks which resource type a plan-level --max-actions/
// --max-percentage override applies to: the type with the most planned
// actions, with a fixed, deterministic tie-break order.
func resourceMajority(plan domain.Plan) domain.ResourceType {
	counts := map[domain.ResourceType]int{}
	for _, a := range plan.Actions {
		counts[a.ResourceType]++
	}
	best := domain.ResourceTypeProject
	bestCount := -1
	for _, rt := range []domain.ResourceType{
		domain.ResourceTypeProject, domain.ResourceTypeUser,
		domain.ResourceTypePipelineConfig, domain.ResourceTypeRunner,
	} {
		if counts[rt] > bestCount {
			best, bestCount = rt, counts[rt]
		}
	}
	return best
}

// totalOutOfScopeImpact sums OutOfScopeProjectCount across every
// ActionAddRunnerTag in the plan - the number of projects a shared-runner
// tag change would affect outside the scope that was actually evaluated.
func totalOutOfScopeImpact(plan domain.Plan) int {
	total := 0
	for _, a := range plan.Actions {
		if a.Action == domain.ActionAddRunnerTag {
			total += a.OutOfScopeProjectCount
		}
	}
	return total
}

// confirmOutOfScopeImpact requires an explicit, exact-match
// --confirm-out-of-scope-impact=<N> before a plan touching a shared
// runner used outside the evaluated scope can proceed at all - in both
// interactive and non-interactive contexts, since this risk is
// independent of TTY-ness. It lists every affected out-of-scope project
// path so the operator can actually look at them before deciding.
func confirmOutOfScopeImpact(cmd *cobra.Command, plan domain.Plan, total, confirmed int) error {
	if confirmed != total {
		cmd.PrintErrln("WARNING: this plan changes tags on at least one shared runner used by projects outside the evaluated scope:")
		for _, p := range outOfScopeProjectPaths(plan) {
			cmd.PrintErrln("  -", p)
		}
		return exitErr(ExitGeneralError, fmt.Errorf(
			"this plan affects %d project(s) outside the evaluated scope; pass --confirm-out-of-scope-impact=%d to proceed", total, total))
	}
	return nil
}

// outOfScopeProjectPaths collects every out-of-scope project path across
// every ActionAddRunnerTag in the plan.
func outOfScopeProjectPaths(plan domain.Plan) []string {
	var paths []string
	for _, a := range plan.Actions {
		if a.Action == domain.ActionAddRunnerTag {
			paths = append(paths, a.OutOfScopeProjectPaths...)
		}
	}
	return paths
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
	if outOfScopePaths := outOfScopeProjectPaths(plan); len(outOfScopePaths) > 0 {
		cmd.Println("This plan changes tags on a shared runner used by projects OUTSIDE this scope:")
		for _, p := range outOfScopePaths {
			cmd.Println("  -", p)
		}
		cmd.Println()
	}
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
