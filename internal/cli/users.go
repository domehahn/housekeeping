package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/domehahn/housekeeping/internal/app"
	"github.com/domehahn/housekeeping/internal/config"
	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/output"
	"github.com/domehahn/housekeeping/internal/policy/user"
	"github.com/domehahn/housekeeping/internal/provider"
)

type userFlags struct {
	inactiveFor        string
	lastLoginBefore    string
	lastActivityBefore string
	match              string

	ignoreGlobalActivityIfNonBillableElsewhere bool
	billableThreshold                          string
}

func newUsersCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{Use: "users", Short: "Discover, evaluate, and plan cleanup of users"}
	cmd.AddCommand(newUsersListCmd(e), newUsersEvaluateCmd(e), newUsersPlanCmd(e))
	return cmd
}

func newUsersListCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List users discovered within a scope (direct group members)",
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
			users, err := app.DiscoverUsers(ctx, client, scope)
			if err != nil {
				return wrapProviderErr(err)
			}

			rows := make([][]string, 0, len(users))
			for _, u := range users {
				rows = append(rows, []string{u.ID, u.Username, string(u.AccessLevel), formatTimestamp(u.LastLoginAt, e.clock), formatTimestamp(u.LastActivityAt, e.clock)})
			}
			table := output.Table{
				Headers: []string{"ID", "Username", "Access Level", "Last Login", "Last Activity"},
				Rows:    rows,
				Footer:  fmt.Sprintf("%d user(s) discovered in %s", len(users), scope.Path),
			}
			return output.Render(cmd.OutOrStdout(), e.format, table, users)
		},
	}
}

func newUsersEvaluateCmd(e *env) *cobra.Command {
	flags := &userFlags{}
	cmd := &cobra.Command{
		Use:   "evaluate",
		Short: "Evaluate users against inactivity policy without producing a plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := e.requireClient()
			if err != nil {
				return err
			}
			summary, scope, err := runUserEvaluation(cmd, e, client, flags)
			if err != nil {
				return err
			}
			return renderUserEvaluation(cmd, e, scope, summary)
		},
	}
	bindUserFlags(cmd, flags)
	return cmd
}

func newUsersPlanCmd(e *env) *cobra.Command {
	flags := &userFlags{}
	var actionStr, outputPlan string
	var maxActions, maxPercentage int
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Evaluate users and produce a reviewable, saveable cleanup plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			action, err := parseUserAction(actionStr)
			if err != nil {
				return exitErr(ExitInvalidConfiguration, err)
			}

			client, err := e.requireClient()
			if err != nil {
				return err
			}
			summary, scope, err := runUserEvaluation(cmd, e, client, flags)
			if err != nil {
				return err
			}

			info, err := client.Info(cmd.Context())
			if err != nil {
				return wrapProviderErr(err)
			}

			matched := summary.Matched()
			plan := app.BuildUserPlan(info.Provider, info.Instance, scope, matched, action, e.clock)

			limits := resolveSafetyLimits(e, maxActions, maxPercentage, domain.ResourceTypeUser)
			discovered := map[domain.ResourceType]int{domain.ResourceTypeUser: summary.Discovered()}
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

			if err := renderUserEvaluation(cmd, e, scope, summary); err != nil {
				return err
			}
			return output.Render(cmd.OutOrStdout(), e.format, planTable(plan), plan)
		},
	}
	bindUserFlags(cmd, flags)
	cmd.Flags().StringVar(&actionStr, "action", "report", "action to plan: report|remove-from-group|block")
	cmd.Flags().StringVar(&outputPlan, "output-plan", "", "write the plan as JSON to this path")
	cmd.Flags().IntVar(&maxActions, "max-actions", 0, "override safety.max_actions.users (must be explicit)")
	cmd.Flags().IntVar(&maxPercentage, "max-percentage", 0, "override safety.max_percentage.users (must be explicit)")
	return cmd
}

func bindUserFlags(cmd *cobra.Command, flags *userFlags) {
	cmd.Flags().StringVar(&flags.inactiveFor, "inactive-for", "", "shorthand: sets both --last-login-before and --last-activity-before to the same value")
	cmd.Flags().StringVar(&flags.lastLoginBefore, "last-login-before", "", "match users whose last login is older than this (e.g. 30d)")
	cmd.Flags().StringVar(&flags.lastActivityBefore, "last-activity-before", "", "match users whose last activity is older than this (e.g. 30d)")
	cmd.Flags().StringVar(&flags.match, "match", "", "how to combine login/activity criteria: all|any (default from config, else all)")
	cmd.Flags().BoolVar(&flags.ignoreGlobalActivityIfNonBillableElsewhere, "ignore-global-activity-if-non-billable-elsewhere", false,
		"override protection from global activity for users who are billable in this group but hold no privileged membership elsewhere (requires Owner on this top-level group + admin token; see README)")
	cmd.Flags().StringVar(&flags.billableThreshold, "billable-threshold", "", "access level (guest|reporter|developer|maintainer|owner) that counts as \"binds a seat\" elsewhere for the override above (default developer)")
}

func runUserEvaluation(cmd *cobra.Command, e *env, client provider.Client, flags *userFlags) (app.UserEvaluationSummary, domain.Scope, error) {
	group, recursive, err := resolveGroupFlag(e, cmd)
	if err != nil {
		return app.UserEvaluationSummary{}, domain.Scope{}, err
	}
	ctx := cmd.Context()
	scope, err := app.ResolveScope(ctx, client, group, recursive)
	if err != nil {
		return app.UserEvaluationSummary{}, domain.Scope{}, wrapProviderErr(err)
	}
	users, err := app.DiscoverUsers(ctx, client, scope)
	if err != nil {
		return app.UserEvaluationSummary{}, domain.Scope{}, wrapProviderErr(err)
	}

	policy, err := buildUserPolicy(e, flags)
	if err != nil {
		return app.UserEvaluationSummary{}, domain.Scope{}, exitErr(ExitInvalidConfiguration, err)
	}

	currentUserID := ""
	if e.cfg.Users.ExcludeCurrentUser {
		me, err := client.CurrentUser(ctx)
		if err != nil {
			return app.UserEvaluationSummary{}, domain.Scope{}, wrapProviderErr(fmt.Errorf("resolve current user for self-protection: %w", err))
		}
		currentUserID = me.ID
	}
	protection, err := user.NewProtection(
		e.cfg.Users.Protection.Usernames,
		e.cfg.Users.Protection.Regex,
		e.cfg.Users.Protection.AccessLevels,
		currentUserID,
	)
	if err != nil {
		return app.UserEvaluationSummary{}, domain.Scope{}, exitErr(ExitInvalidConfiguration, err)
	}

	summary := app.EvaluateUsers(ctx, users, policy, protection)

	override, err := buildBillableSeatOverride(e, flags)
	if err != nil {
		return app.UserEvaluationSummary{}, domain.Scope{}, exitErr(ExitInvalidConfiguration, err)
	}
	if override.Enabled {
		result, err := app.ApplyBillableSeatOverride(ctx, client, scope, summary, override)
		if err != nil {
			// Fail safe: the override simply does not apply (base summary
			// is used unchanged) rather than aborting evaluation - a
			// permission gap here must not be mistaken for "no users
			// matched" either, so it is surfaced loudly.
			cmd.PrintErrln("WARNING: billable seat override could not be applied:", err)
		} else {
			for _, w := range result.Warnings {
				cmd.PrintErrln("WARNING:", w)
			}
			summary = result.Summary
		}
	}

	return summary, scope, nil
}

// buildBillableSeatOverride resolves the billable-seat override
// configuration (config file, then --ignore-global-activity-if-non-billable-elsewhere
// / --billable-threshold flags).
func buildBillableSeatOverride(e *env, flags *userFlags) (user.BillableSeatOverride, error) {
	enabled := e.cfg.Users.Inactive.IgnoreGlobalActivityIfNonBillableElsewhere || flags.ignoreGlobalActivityIfNonBillableElsewhere

	thresholdStr := e.cfg.Users.Inactive.BillableAccessLevelThreshold
	if flags.billableThreshold != "" {
		thresholdStr = flags.billableThreshold
	}
	if thresholdStr == "" {
		thresholdStr = string(domain.AccessLevelDeveloper)
	}
	threshold := domain.AccessLevel(thresholdStr)
	if _, ok := map[domain.AccessLevel]bool{
		domain.AccessLevelGuest: true, domain.AccessLevelReporter: true, domain.AccessLevelDeveloper: true,
		domain.AccessLevelMaintainer: true, domain.AccessLevelOwner: true,
	}[threshold]; !ok {
		return user.BillableSeatOverride{}, fmt.Errorf("invalid billable-threshold %q (must be one of guest|reporter|developer|maintainer|owner)", thresholdStr)
	}

	return user.BillableSeatOverride{Enabled: enabled, Threshold: threshold}, nil
}

func buildUserPolicy(e *env, flags *userFlags) (domain.UserPolicy, error) {
	enabled := e.cfg.Users.Inactive.Enabled
	if flags.inactiveFor != "" || flags.lastLoginBefore != "" || flags.lastActivityBefore != "" {
		enabled = true
	}
	if !enabled {
		return nil, fmt.Errorf("no user policy enabled: set --inactive-for, --last-login-before/--last-activity-before, or users.inactive.enabled")
	}

	onUnknown, err := user.ParseUnknownBehavior(e.cfg.Users.UnknownActivity)
	if err != nil {
		return nil, err
	}

	loginDays := e.cfg.Users.Inactive.LastLoginDays
	activityDays := e.cfg.Users.Inactive.LastActivityDays
	matchMode := e.cfg.Users.Inactive.Match

	if flags.inactiveFor != "" {
		d, err := config.ParseDuration(flags.inactiveFor)
		if err != nil {
			return nil, err
		}
		days := int(d.Hours() / 24)
		loginDays, activityDays = days, days
	}
	if flags.lastLoginBefore != "" {
		d, err := config.ParseDuration(flags.lastLoginBefore)
		if err != nil {
			return nil, err
		}
		loginDays = int(d.Hours() / 24)
	}
	if flags.lastActivityBefore != "" {
		d, err := config.ParseDuration(flags.lastActivityBefore)
		if err != nil {
			return nil, err
		}
		activityDays = int(d.Hours() / 24)
	}
	if flags.match != "" {
		matchMode = flags.match
	}

	mode, err := user.ParseMatchMode(matchMode)
	if err != nil {
		return nil, err
	}

	return user.InactiveUserPolicy{
		LastLogin:    user.LastLoginPolicy{ThresholdDays: loginDays, Clock: e.clock, OnUnknown: onUnknown},
		LastActivity: user.LastActivityPolicy{ThresholdDays: activityDays, Clock: e.clock, OnUnknown: onUnknown},
		Mode:         mode,
	}, nil
}

func parseUserAction(s string) (domain.ActionType, error) {
	switch s {
	case "report", "":
		return domain.ActionReport, nil
	case "remove-from-group":
		return domain.ActionRemoveGroupMember, nil
	case "block":
		return domain.ActionBlockUser, nil
	default:
		return "", fmt.Errorf("unsupported user action %q (supported: report|remove-from-group|block)", s)
	}
}

func renderUserEvaluation(cmd *cobra.Command, e *env, scope domain.Scope, summary app.UserEvaluationSummary) error {
	matched := summary.Matched()
	rows := make([][]string, 0, len(matched))
	for _, m := range matched {
		rows = append(rows, []string{m.User.ID, m.User.Username, formatTimestamp(m.User.LastLoginAt, e.clock), formatTimestamp(m.User.LastActivityAt, e.clock), joinReasons(m.Evaluation.Reasons)})
	}
	table := output.Table{
		Headers: []string{"ID", "Username", "Last Login", "Last Activity", "Reasons"},
		Rows:    rows,
		Footer: fmt.Sprintf("Evaluated %d, matched %d, protected %d, unknown activity %d in scope %s",
			summary.Discovered(), len(matched), len(summary.Protected()), len(summary.Unknown()), scope.Path),
	}
	return output.Render(cmd.OutOrStdout(), e.format, table, summary)
}
