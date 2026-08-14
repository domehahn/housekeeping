package app

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	provider.GroupMemberRemover
	provider.UserBlocker
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
	if opts.Revalidate {
		if skip, detail := revalidate(ctx, client, action); skip {
			return domain.ActionOutcome{Action: action, Result: domain.ResultSkippedRevalidate, Detail: detail}
		}
	}

	if !opts.Apply {
		return domain.ActionOutcome{Action: action, Result: domain.ResultDryRun, Detail: "simulated (pass --apply to execute)"}
	}

	err := performAction(ctx, client, action)
	if err == nil {
		return domain.ActionOutcome{Action: action, Result: domain.ResultSuccess}
	}

	var pErr *provider.Error
	if errors.As(err, &pErr) && pErr.Kind == provider.KindNotFound {
		// The resource (or membership) is already gone - treat as a
		// successful no-op rather than a failure, per the idempotency
		// requirement.
		return domain.ActionOutcome{Action: action, Result: domain.ResultSkippedAlreadyDone, Detail: "resource no longer exists"}
	}

	return domain.ActionOutcome{Action: action, Result: domain.ResultFailed, Detail: err.Error()}
}

// revalidate re-fetches the resource and reports whether the action should
// be skipped because the fact that justified it is no longer true, or
// because the resource could no longer be confirmed at all (a failed
// revalidation is treated as "skip", not "proceed anyway" - see the
// project's "safety over convenience" principle).
func revalidate(ctx context.Context, client Executor, action domain.PlannedAction) (skip bool, detail string) {
	switch action.ResourceType {
	case domain.ResourceTypeProject:
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
		return false, ""

	case domain.ResourceTypeUser:
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
		return false, ""

	default:
		return false, ""
	}
}

// activityChangedSince reports whether ts represents a known, non-nil point
// in time that falls after "since" - i.e. the resource had activity that
// post-dates when the plan was created.
func activityChangedSince(ts domain.Timestamp, since time.Time) bool {
	return ts.IsKnown() && ts.At != nil && ts.At.After(since)
}

func performAction(ctx context.Context, client Executor, action domain.PlannedAction) error {
	switch action.Action {
	case domain.ActionDeleteProject:
		return client.DeleteProject(ctx, action.ResourceID)
	case domain.ActionArchiveProject:
		return client.ArchiveProject(ctx, action.ResourceID)
	case domain.ActionRemoveGroupMember:
		if action.GroupID == "" {
			return fmt.Errorf("remove-from-group action for user %s is missing a group ID", action.ResourceID)
		}
		return client.RemoveGroupMember(ctx, action.GroupID, action.ResourceID)
	case domain.ActionBlockUser:
		return client.BlockUser(ctx, action.ResourceID)
	case domain.ActionReport:
		return nil // report-only actions never mutate anything
	default:
		return fmt.Errorf("unsupported action type %q", action.Action)
	}
}
