package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/domehahn/housekeeping/internal/ciyaml"
	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/provider"
)

// Executor is the subset of the provider needed to carry out planned
// actions and revalidate them immediately beforehand.
type Executor interface {
	provider.ProjectGetter
	provider.ProjectDeleter
	provider.ProjectArchiver
	provider.UserGetter
	provider.GroupMemberGetter
	provider.GroupMemberRemover
	provider.UserBlocker
	provider.CurrentUserResolver
	provider.PipelineConfigProposer
	provider.RunnerTagUpdater
}

// ExecuteOptions controls how a plan is carried out.
type ExecuteOptions struct {
	// Apply must be explicitly true to perform any destructive call; when
	// false every action is simulated (ResultDryRun) with no provider I/O
	// for the mutating call itself (revalidation reads still happen so the
	// dry run output reflects current reality).
	Apply bool
	// Revalidate re-fetches each resource immediately before acting and
	// skips the action if the resource's state has since improved (e.g.
	// the user became active again) - see docs/adr and requirement #18.
	Revalidate bool
	// FailFast stops processing further actions after the first failure
	// when true. When false (the default) all actions are attempted and
	// failures are reported individually.
	FailFast bool
	// Current protection rules are evaluated again against live resource
	// state. This catches configuration and role/path changes since planning.
	ProjectProtection domain.ProjectProtectionRule
	UserProtection    domain.UserProtectionRule
}

// ExecutionSummary aggregates the per-action outcomes of a run.
type ExecutionSummary struct {
	Outcomes []domain.ActionOutcome
}

func (s ExecutionSummary) CountByResult(r domain.ExecutionResult) int {
	n := 0
	for _, o := range s.Outcomes {
		if o.Result == r {
			n++
		}
	}
	return n
}

// Partial reports whether the run had a mix of outcomes that is neither a
// clean success nor a clean no-op - i.e. at least one failure occurred
// alongside at least one success or skip.
func (s ExecutionSummary) Partial() bool {
	return s.CountByResult(domain.ResultFailed) > 0 && s.CountByResult(domain.ResultFailed) < len(s.Outcomes)
}

// AllFailed reports whether every attempted action failed.
func (s ExecutionSummary) AllFailed() bool {
	return len(s.Outcomes) > 0 && s.CountByResult(domain.ResultFailed) == len(s.Outcomes)
}

// Execute carries out every action in a plan in order, honoring dry-run,
// revalidation, idempotency, and fail-fast settings. It never stops
// processing on an individual failure unless FailFast is set - a broken
// resource must not prevent unrelated resources in the same plan from
// being cleaned up.
func Execute(ctx context.Context, client Executor, plan domain.Plan, opts ExecuteOptions) ExecutionSummary {
	summary := ExecutionSummary{Outcomes: make([]domain.ActionOutcome, 0, len(plan.Actions))}

	for _, action := range plan.Actions {
		if err := ctx.Err(); err != nil {
			summary.Outcomes = append(summary.Outcomes, domain.ActionOutcome{
				Action: action, Result: domain.ResultFailed, Detail: "execution cancelled: " + err.Error(),
			})
			break
		}

		outcome := executeOne(ctx, client, action, opts)
		summary.Outcomes = append(summary.Outcomes, outcome)

		if opts.FailFast && outcome.Result == domain.ResultFailed {
			break
		}
	}
	return summary
}

func executeOne(ctx context.Context, client Executor, action domain.PlannedAction, opts ExecuteOptions) domain.ActionOutcome {
	// Self-protection is unconditional: even an operator who explicitly turns
	// normal revalidation off must not be able to remove or block the identity
	// whose token is executing the plan.
	if action.ResourceType == domain.ResourceTypeUser && action.Action != domain.ActionReport {
		me, err := client.CurrentUser(ctx)
		if err != nil {
			return domain.ActionOutcome{Action: action, Result: domain.ResultSkippedRevalidate, Detail: "could not verify the authenticated user, skipping to prevent self-removal: " + err.Error()}
		}
		if me.ID == action.ResourceID {
			return domain.ActionOutcome{Action: action, Result: domain.ResultSkippedRevalidate, Detail: "protected: target is the currently authenticated user"}
		}
	}
	if opts.Revalidate {
		if skip, detail := revalidate(ctx, client, action, opts); skip {
			return domain.ActionOutcome{Action: action, Result: domain.ResultSkippedRevalidate, Detail: detail}
		}
	}

	if !opts.Apply {
		return domain.ActionOutcome{Action: action, Result: domain.ResultDryRun, Detail: "simulated (pass --apply to execute)"}
	}

	detail, err := performAction(ctx, client, action)
	if err == nil {
		return domain.ActionOutcome{Action: action, Result: domain.ResultSuccess, Detail: detail}
	}

	var pErr *provider.Error
	if errors.As(err, &pErr) && pErr.Kind == provider.KindNotFound {
		// The resource (or membership, or already-applied tag) is already
		// gone/done - treat as a successful no-op rather than a failure,
		// per the idempotency requirement.
		return domain.ActionOutcome{Action: action, Result: domain.ResultSkippedAlreadyDone, Detail: pErr.Message}
	}

	return domain.ActionOutcome{Action: action, Result: domain.ResultFailed, Detail: err.Error()}
}

// revalidate re-fetches the resource and reports whether the action should
// be skipped because the fact that justified it is no longer true, or
// because the resource could no longer be confirmed at all (a failed
// revalidation is treated as "skip", not "proceed anyway" - see the
// project's "safety over convenience" principle).
func revalidate(ctx context.Context, client Executor, action domain.PlannedAction, opts ExecuteOptions) (skip bool, detail string) {
	switch action.ResourceType {
	case domain.ResourceTypeProject:
		return revalidateProject(ctx, client, action, opts)
	case domain.ResourceTypeUser:
		return revalidateUser(ctx, client, action, opts)
	default:
		return false, ""
	}
}

func revalidateProject(ctx context.Context, client Executor, action domain.PlannedAction, opts ExecuteOptions) (skip bool, detail string) {
	proj, err := client.GetProject(ctx, action.ResourceID)
	if err != nil {
		var pErr *provider.Error
		if errors.As(err, &pErr) && pErr.Kind == provider.KindNotFound {
			return false, "" // let performAction observe the 404 and report already-done
		}
		return true, "revalidation failed, skipping to be safe: " + err.Error()
	}
	if activityChangedSince(proj.LastActivityAt, action.EvaluatedAt) {
		return true, fmt.Sprintf("project has had activity since it was planned (%s)", proj.LastActivityAt.At.Format("2006-01-02"))
	}
	if proj.FullPath != action.ResourceName {
		return true, fmt.Sprintf("project path changed since planning (%q -> %q)", action.ResourceName, proj.FullPath)
	}
	if opts.ProjectProtection != nil {
		if protected, reason := opts.ProjectProtection.IsProtected(proj); protected {
			return true, reason
		}
	}
	return false, ""
}

func revalidateUser(ctx context.Context, client Executor, action domain.PlannedAction, opts ExecuteOptions) (skip bool, detail string) {
	u, err := client.GetUser(ctx, action.ResourceID)
	if err != nil {
		var pErr *provider.Error
		if errors.As(err, &pErr) && pErr.Kind == provider.KindNotFound {
			return false, ""
		}
		return true, "revalidation failed, skipping to be safe: " + err.Error()
	}
	if activityChangedSince(u.LastLoginAt, action.EvaluatedAt) || activityChangedSince(u.LastActivityAt, action.EvaluatedAt) {
		return true, "user has had login or activity since the plan was created"
	}
	if u.Username != action.ResourceName {
		return true, fmt.Sprintf("username changed since planning (%q -> %q)", action.ResourceName, u.Username)
	}
	if action.GroupID != "" {
		if skip, detail := revalidateUserMembership(ctx, client, action, &u); skip {
			return true, detail
		}
	}
	if opts.UserProtection != nil {
		if protected, reason := opts.UserProtection.IsProtected(u); protected {
			return true, reason
		}
	}
	return false, ""
}

// revalidateUserMembership re-fetches a user's direct membership in the
// planned group, updating u in place with the live values so protection
// checks afterward see current data.
func revalidateUserMembership(ctx context.Context, client Executor, action domain.PlannedAction, u *domain.User) (skip bool, detail string) {
	membership, err := client.GetGroupMember(ctx, action.GroupID, action.ResourceID)
	if err != nil {
		var pErr *provider.Error
		if errors.As(err, &pErr) && pErr.Kind == provider.KindNotFound {
			if action.Action == domain.ActionRemoveGroupMember {
				return false, "" // let removal report the membership as already gone
			}
			return true, "target is no longer a direct member of the planned group"
		}
		return true, "membership revalidation failed, skipping to be safe: " + err.Error()
	}
	u.AccessLevel = membership.AccessLevel
	u.GroupID = membership.GroupID
	u.MembershipOrigin = membership.MembershipOrigin
	if membership.MembershipOrigin != domain.MembershipDirect {
		return true, "membership is no longer confirmed as direct"
	}
	if action.AccessLevel != "" && membership.AccessLevel != action.AccessLevel {
		return true, fmt.Sprintf("access level changed since planning (%q -> %q)", action.AccessLevel, membership.AccessLevel)
	}
	return false, ""
}

// activityChangedSince reports whether ts represents a known, non-nil point
// in time that falls after "since" - i.e. the resource had activity that
// post-dates when the plan was created.
func activityChangedSince(ts domain.Timestamp, since time.Time) bool {
	return ts.IsKnown() && ts.At != nil && ts.At.After(since)
}

// performAction carries out a single action's mutating call and returns a
// human-readable detail string for successful outcomes (e.g. a Merge
// Request URL) alongside the usual error.
func performAction(ctx context.Context, client Executor, action domain.PlannedAction) (string, error) {
	switch action.Action {
	case domain.ActionDeleteProject:
		return "", client.DeleteProject(ctx, action.ResourceID)
	case domain.ActionArchiveProject:
		return "", client.ArchiveProject(ctx, action.ResourceID)
	case domain.ActionRemoveGroupMember:
		if action.GroupID == "" {
			return "", fmt.Errorf("remove-from-group action for user %s is missing a group ID", action.ResourceID)
		}
		return "", client.RemoveGroupMember(ctx, action.GroupID, action.ResourceID)
	case domain.ActionBlockUser:
		return "", client.BlockUser(ctx, action.ResourceID)
	case domain.ActionReport:
		return "", nil // report-only actions never mutate anything
	case domain.ActionAddPipelineTag:
		return performPipelineTagAction(ctx, client, action)
	case domain.ActionAddRunnerTag:
		return "", performRunnerTagAction(ctx, client, action)
	default:
		return "", fmt.Errorf("unsupported action type %q", action.Action)
	}
}

// performPipelineTagAction re-fetches the project's .gitlab-ci.yml fresh
// and re-runs the patch - this live re-fetch *is* the revalidation for
// this action type (see the Executor interface doc comment): a file
// changed since planning, or a tag already added by a previous run or a
// human, is naturally handled because the decision is always made against
// current content, never a stale precomputed diff. On success, the
// returned detail is the opened Merge Request's URL.
func performPipelineTagAction(ctx context.Context, client Executor, action domain.PlannedAction) (string, error) {
	content, exists, err := client.GetPipelineConfig(ctx, action.ResourceID)
	if err != nil {
		return "", fmt.Errorf("fetch current .gitlab-ci.yml: %w", err)
	}
	if !exists {
		return "", provider.NewError(provider.KindNotFound, "add pipeline tag", ".gitlab-ci.yml no longer exists", nil)
	}

	patched, changes, err := ciyaml.AddTag(content, action.TagValue)
	if err != nil {
		return "", fmt.Errorf("patch .gitlab-ci.yml: %w", err)
	}
	if len(changes) == 0 {
		// Tag is already present everywhere it would be added - a
		// previous run's Merge Request was likely already merged, or a
		// human added it directly. Idempotent no-op.
		return "", provider.NewError(provider.KindNotFound, "add pipeline tag", "tag already present", nil)
	}

	url, err := client.ProposePipelineTagChange(ctx, action.ResourceID, patched, action.TagValue)
	if err != nil {
		return "", err
	}
	return "merge request opened: " + url, nil
}

// performRunnerTagAction re-fetches the runner's current tag list and
// applies the union with the desired tag - the live re-fetch is the
// revalidation, and an already-present tag is reported as idempotent
// no-op rather than a redundant API call.
func performRunnerTagAction(ctx context.Context, client Executor, action domain.PlannedAction) error {
	current, err := client.GetRunnerTags(ctx, action.ResourceID)
	if err != nil {
		return fmt.Errorf("fetch current runner tags: %w", err)
	}
	for _, t := range current {
		if t == action.TagValue {
			return provider.NewError(provider.KindNotFound, "add runner tag", "tag already present", nil)
		}
	}
	return client.UpdateRunnerTags(ctx, action.ResourceID, append(current, action.TagValue))
}
