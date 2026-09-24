package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/domehahn/housekeeping/internal/app"
	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/output"
	projectpolicy "github.com/domehahn/housekeeping/internal/policy/project"
	userpolicy "github.com/domehahn/housekeeping/internal/policy/user"
	"github.com/domehahn/housekeeping/internal/provider"
	"github.com/domehahn/housekeeping/pkg/version"
)

// executeFlags holds every flag `execute` accepts.
type executeFlags struct {
	apply                         bool
	nonInteractive                bool
	confirmScope                  string
	maxActions, maxPercentage     int
	confirmOutOfScopeImpact       int
	autoMergeIfNoApprovalRequired bool
	allowStalePlan                string
	confirmInstanceWideUserBlock  bool
}

func newExecuteCmd(e *env) *cobra.Command {
	flags := &executeFlags{}

	cmd := &cobra.Command{
		Use:   "execute <plan-file>",
		Short: "Simulate (default) or apply (--apply) any saved plan",
		Args:  cobra.ExactArgs(1),
		Long: `execute reads a plan file produced by "projects plan", "branches plan", "users plan",
"pipelines plan", or "runners plan" and carries out its actions.

Without --apply, every action is only simulated: nothing is changed. This
is the default and cannot be bypassed accidentally.

With --apply, actions are actually performed. In an interactive terminal
this requires typing an explicit confirmation phrase; in a non-interactive
context (--non-interactive, or stdin is not a TTY) you must additionally
pass --confirm-scope matching the plan's scope path, as an extra guard
against unattended misfires (e.g. a misconfigured CI job).

--auto-merge-if-no-approval-required (opt-in, off by default) merges a
pipeline-tag Merge Request (add-pipeline-tag/replace-pipeline-tag)
immediately after it is opened, but only when the project's approval
rules require zero approvals for it - GitLab is still told to wait for
the Merge Request's own pipeline to succeed first. A Merge Request that
does require approval is left open and listed in a separate report so it
can be handed to approvers (e.g. pasted into a Teams channel).`,
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
		"required, and must exactly equal the plan's explicit out-of-scope project assignments, when a runner-tag action has such assignments")
	cmd.Flags().BoolVar(&flags.autoMergeIfNoApprovalRequired, "auto-merge-if-no-approval-required", false,
		"merge a pipeline-tag Merge Request immediately if its project requires zero approvals (waits for its pipeline to succeed); Merge Requests needing approval are reported instead")
	cmd.Flags().StringVar(&flags.allowStalePlan, "allow-stale-plan", "", "emergency override for executing a stale plan; must match the plan's exact hash")
	cmd.Flags().BoolVar(&flags.confirmInstanceWideUserBlock, "confirm-instance-wide-user-block", false,
		"required explicit acknowledgement when applying a plan that contains instance-wide user block actions")
	return cmd
}

func runExecute(cmd *cobra.Command, e *env, planPath string, flags *executeFlags) error {
	plan, err := app.LoadPlan(planPath)
	if err != nil {
		return exitErr(ExitPlanValidationFailed, err)
	}

	// Check plan freshness / stale plan
	maxAge := e.cfg.Execution.MaxPlanAge
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	planAge := e.clock.Now().Sub(plan.CreatedAt)
	if planAge > maxAge || plan.IsExpired(e.clock.Now()) {
		if flags.allowStalePlan == "" || flags.allowStalePlan != plan.Hash {
			return exitErr(ExitStalePlan, fmt.Errorf("plan is stale (created %s ago at %s, maximum allowed plan age is %s); regenerate the plan or pass --allow-stale-plan=%s to force execution", planAge.Truncate(time.Second), plan.CreatedAt.Format(time.RFC3339), maxAge, plan.Hash))
		}
	}

	// Check configuration digest integrity
	if plan.Provenance != nil && plan.Provenance.ConfigDigest != "" {
		currentConfigDigest := e.cfg.ComputeConfigDigest()
		if currentConfigDigest != plan.Provenance.ConfigDigest {
			return exitErr(ExitConfigDrift, fmt.Errorf("safety-relevant configuration changed since plan creation (plan digest %s, current %s); regenerate the plan before executing", plan.Provenance.ConfigDigest, currentConfigDigest))
		}
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

	if err := checkExecuteSafetyGuards(cmd, e, ctx, client, plan, flags); err != nil {
		return err
	}

	if flags.apply {
		if outOfScope := totalOutOfScopeImpact(plan); outOfScope > 0 {
			if err := confirmOutOfScopeImpact(cmd, plan, outOfScope, flags.confirmOutOfScopeImpact); err != nil {
				return err
			}
		}
		if err := confirmApply(cmd, plan, flags.nonInteractive, flags.confirmScope, flags.confirmInstanceWideUserBlock); err != nil {
			return err
		}
	} else {
		cmd.Println("Dry run: no changes will be made. Pass --apply to execute for real.")
	}

	return runAndReportExecution(cmd, e, ctx, client, plan, flags)
}

// checkExecuteSafetyGuards re-checks both absolute max-actions and percentage guards
// against the plan's action counts, embedded discovery metadata, and live rediscovered population.
func checkExecuteSafetyGuards(cmd *cobra.Command, e *env, ctx context.Context, client provider.Client, plan domain.Plan, flags *executeFlags) error {
	limits, err := resolvePlanSafetyLimits(e, flags.maxActions, flags.maxPercentage, plan)
	if err != nil {
		return exitErr(ExitInvalidConfiguration, err)
	}

	// 1. Evaluate against stored plan discovery snapshot
	violations := app.CheckGuards(plan, plan.Discovery, limits)

	// 2. Perform live population rediscovery if client is available
	presentTypes := map[domain.ResourceType]bool{}
	for _, a := range plan.Actions {
		presentTypes[a.ResourceType] = true
	}
	var rTypes []domain.ResourceType
	for rt := range presentTypes {
		rTypes = append(rTypes, rt)
	}

	if client != nil && len(rTypes) > 0 {
		liveDiscovery, rediscoveryErr := app.RediscoverScopePopulation(ctx, client, plan.Scope.Scope(), rTypes)
		if rediscoveryErr != nil {
			// Fail closed if any resource type in the plan has a percentage guard enabled
			for _, rt := range rTypes {
				if limits.Limit(rt).MaxPercentage > 0 {
					cmd.PrintErrln("SAFETY GUARD ERROR: live population rediscovery failed:", rediscoveryErr)
					return exitErr(ExitSafetyGuardTriggered, fmt.Errorf("cannot evaluate live percentage guard because scope population rediscovery failed: %w", rediscoveryErr))
				}
			}
		} else {
			// Evaluate against live population
			liveViolations := app.CheckGuards(plan, liveDiscovery, limits)
			violations = append(violations, liveViolations...)
		}
	}

	if len(violations) == 0 {
		return nil
	}
	seenMsg := map[string]bool{}
	for _, v := range violations {
		if !seenMsg[v.Error()] {
			seenMsg[v.Error()] = true
			cmd.PrintErrln("SAFETY GUARD:", v.Error())
		}
	}
	return exitErr(ExitSafetyGuardTriggered, fmt.Errorf("plan exceeds configured safety guards"))
}

func runAndReportExecution(cmd *cobra.Command, e *env, ctx context.Context, client provider.Client, plan domain.Plan, flags *executeFlags) error {
	if flags.apply && e.cfg.Audit.RequiredForApply && e.flags.auditLogPath == "" && e.cfg.Audit.Path == "" {
		return exitErr(ExitInvalidConfiguration, fmt.Errorf("audit log is required for --apply execution (set audit.required_for_apply or pass --audit-log)"))
	}

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

	runID := fmt.Sprintf("run-%d", e.clock.Now().UnixNano())
	actor := ""
	if me, err := client.CurrentUser(ctx); err == nil {
		actor = me.Username
	}

	summary := app.Execute(ctx, client, plan, app.ExecuteOptions{
		Apply:                         flags.apply,
		Revalidate:                    e.cfg.Execution.Revalidate,
		FailFast:                      e.cfg.Execution.FailFast,
		ProjectProtection:             projectProtection,
		UserProtection:                userProtection,
		AutoMergeIfNoApprovalRequired: flags.autoMergeIfNoApprovalRequired,
		AuditWriter:                   auditLog,
		RunID:                         runID,
		ConfigDigest:                  e.cfg.ComputeConfigDigest(),
		ToolVersion:                   version.Version,
		ToolCommit:                    version.Commit,
		AuthenticatedActor:            actor,
	})

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

// totalOutOfScopeImpact sums out-of-scope impact across every mutating runner action
// in the plan.
func totalOutOfScopeImpact(plan domain.Plan) int {
	total := 0
	for _, a := range plan.Actions {
		total += a.OutOfScopeImpact()
	}
	return total
}

// confirmOutOfScopeImpact requires an explicit, exact-match
// --confirm-out-of-scope-impact=<N> before a plan touching a runner with
// explicit assignments outside the evaluated scope can proceed at all - in both
// interactive and non-interactive contexts, since this risk is
// independent of TTY-ness. It lists every affected out-of-scope project
// path so the operator can actually look at them before deciding.
func confirmOutOfScopeImpact(cmd *cobra.Command, plan domain.Plan, total, confirmed int) error {
	if confirmed != total {
		cmd.PrintErrln("WARNING: this plan changes tags on a runner explicitly assigned to projects outside the evaluated scope:")
		for _, p := range outOfScopeProjectPaths(plan) {
			cmd.PrintErrln("  -", p)
		}
		return exitErr(ExitGeneralError, fmt.Errorf(
			"this plan affects %d project(s) outside the evaluated scope; pass --confirm-out-of-scope-impact=%d to proceed", total, total))
	}
	return nil
}

// outOfScopeProjectPaths collects every unique out-of-scope project path across
// every mutating runner action in the plan.
func outOfScopeProjectPaths(plan domain.Plan) []string {
	var paths []string
	seen := map[string]bool{}
	for _, a := range plan.Actions {
		if a.OutOfScopeImpact() > 0 {
			for _, p := range a.OutOfScopeProjectPaths {
				if !seen[p] {
					seen[p] = true
					paths = append(paths, p)
				}
			}
		}
	}
	return paths
}

// confirmApply enforces the interactive/non-interactive confirmation
// requirements before any destructive call is made.
func confirmApply(cmd *cobra.Command, plan domain.Plan, nonInteractive bool, confirmScope string, confirmInstanceWideUserBlock bool) error {
	userBlockCount := 0
	for _, a := range plan.Actions {
		if a.Action == domain.ActionBlockUser {
			userBlockCount++
		}
	}

	interactive := !nonInteractive && isTerminal(os.Stdin)

	if !interactive {
		if confirmScope == "" {
			return exitErr(ExitGeneralError, fmt.Errorf("--apply in a non-interactive context requires --confirm-scope=%q", plan.Scope.Path))
		}
		if confirmScope != plan.Scope.Path {
			return exitErr(ExitGeneralError, fmt.Errorf("--confirm-scope %q does not match plan scope %q", confirmScope, plan.Scope.Path))
		}
		if userBlockCount > 0 && !confirmInstanceWideUserBlock {
			return exitErr(ExitGeneralError, fmt.Errorf("--apply in a non-interactive context with user block action(s) requires --confirm-instance-wide-user-block"))
		}
		return nil
	}

	cmd.Println("WARNING")
	cmd.Println()
	cmd.Printf("You are about to execute %d action(s).\n\n", len(plan.Actions))
	cmd.Printf("Provider: %s\nInstance: %s\nScope:    %s\n\n", plan.Provider, plan.Instance, plan.Scope.Path)
	if outOfScopePaths := outOfScopeProjectPaths(plan); len(outOfScopePaths) > 0 {
		cmd.Println("This plan changes tags on a runner explicitly assigned to projects OUTSIDE this scope:")
		for _, p := range outOfScopePaths {
			cmd.Println("  -", p)
		}
		cmd.Println()
	}
	if userBlockCount > 0 {
		cmd.Printf("WARNING: This plan contains %d instance-wide user block action(s).\n", userBlockCount)
		cmd.Println("Blocked users will lose access across ALL projects in the instance.")
		cmd.Println()
	}
	cmd.Println("This operation may be irreversible.")
	cmd.Println()

	var phrase string
	if userBlockCount > 0 {
		phrase = fmt.Sprintf("block %d users instance-wide", userBlockCount)
	} else {
		phrase = fmt.Sprintf("apply %d actions", len(plan.Actions))
	}
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
	if err := output.Render(cmd.OutOrStdout(), e.format, table, summary); err != nil {
		return err
	}

	// The approval report is carried inside `summary` already, so a
	// JSON/YAML render above already included it - only table format
	// needs a second, explicit table here.
	if e.format == output.FormatTable && len(summary.NeedsApproval) > 0 {
		approvalRows := make([][]string, 0, len(summary.NeedsApproval))
		for _, na := range summary.NeedsApproval {
			approvalRows = append(approvalRows, []string{na.ResourceID, na.ResourceName, na.MergeRequestURL})
		}
		approvalTable := output.Table{
			Headers: []string{"Project ID", "Project", "Merge Request URL"},
			Rows:    approvalRows,
			Footer: fmt.Sprintf("%d merge request(s) require approval before they can be merged - hand these links to an approver",
				len(summary.NeedsApproval)),
		}
		return output.RenderTable(cmd.OutOrStdout(), approvalTable)
	}
	return nil
}
