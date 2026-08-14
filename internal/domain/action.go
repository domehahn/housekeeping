package domain

import "time"

// ResourceType identifies what kind of resource a planned action targets.
type ResourceType string

const (
	ResourceTypeProject ResourceType = "project"
	ResourceTypeUser    ResourceType = "user"
)

// ActionType is a specific, provider-independent operation that can be
// planned against a resource. Detection (policy evaluation) and action
// (what to do about a match) are deliberately separate: a policy only ever
// produces an Evaluation, never an ActionType.
type ActionType string

const (
	ActionReport            ActionType = "report"
	ActionDeleteProject     ActionType = "delete-project"
	ActionArchiveProject    ActionType = "archive-project"
	ActionRemoveGroupMember ActionType = "remove-from-group"
	ActionBlockUser         ActionType = "block"
	ActionDeleteUser        ActionType = "delete-user"
)

// PlannedAction is a single, concrete, resource-identified operation that
// execution may later carry out. It always carries a stable resource ID
// (never only a name) plus enough human-readable context to audit and
// review it safely.
type PlannedAction struct {
	ResourceType ResourceType `json:"resourceType"`
	ResourceID   string       `json:"resourceId"`
	ResourceName string       `json:"resourceName"`

	// GroupID is required for user actions that operate on a specific
	// group membership (e.g. remove-from-group); empty for project actions.
	GroupID string `json:"groupId,omitempty"`

	Action ActionType `json:"action"`
	Reason []string   `json:"reason"`

	// EvaluatedAt records when the fact underlying this action was
	// observed, so execution can decide whether to revalidate.
	EvaluatedAt time.Time `json:"evaluatedAt"`
}

// ExecutionResult is the outcome of attempting a single PlannedAction.
type ExecutionResult string

const (
	ResultSuccess            ExecutionResult = "success"
	ResultSkippedRevalidate  ExecutionResult = "skipped_revalidate"
	ResultSkippedAlreadyDone ExecutionResult = "skipped_already_done"
	ResultFailed             ExecutionResult = "failed"
	ResultDryRun             ExecutionResult = "dry_run"
)

// ActionOutcome pairs a PlannedAction with what actually happened when
// execution attempted it.
type ActionOutcome struct {
	Action PlannedAction   `json:"action"`
	Result ExecutionResult `json:"result"`
	Detail string          `json:"detail,omitempty"`
}
