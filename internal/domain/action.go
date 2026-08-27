package domain

import "time"

// ResourceType identifies what kind of resource a planned action targets.
type ResourceType string

const (
	ResourceTypeProject        ResourceType = "project"
	ResourceTypeUser           ResourceType = "user"
	ResourceTypePipelineConfig ResourceType = "pipeline_config"
	ResourceTypeRunner         ResourceType = "runner"
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
	ActionAddPipelineTag    ActionType = "add-pipeline-tag"
	ActionAddRunnerTag      ActionType = "add-runner-tag"
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
	// AccessLevel captures the direct membership role observed while a user
	// removal was planned. Execution compares it with the live membership and
	// skips the action if the role changed in the meantime.
	AccessLevel AccessLevel `json:"accessLevel,omitempty"`

	// TagValue is the CI tag being added, for ActionAddPipelineTag and
	// ActionAddRunnerTag actions only.
	TagValue string `json:"tagValue,omitempty"`
	// OutOfScopeProjectCount is set only for ActionAddRunnerTag: the
	// number of projects using that runner outside the evaluated scope
	// (0 for a non-shared runner, or one only used within scope). This
	// drives the mandatory --confirm-out-of-scope-impact execution guard.
	OutOfScopeProjectCount int `json:"outOfScopeProjectCount,omitempty"`
	// OutOfScopeProjectPaths lists the actual out-of-scope projects (not
	// just the count), so an operator can see exactly what would be
	// affected before confirming.
	OutOfScopeProjectPaths []string `json:"outOfScopeProjectPaths,omitempty"`

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
