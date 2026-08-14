package domain

import "time"

// PlanVersion is the current on-disk plan schema version. Execution refuses
// to run a plan whose version it does not understand - see
// docs/adr/0002-plan-before-execute.md.
const PlanVersion = 1

// Plan is the durable, reviewable artifact produced by a `plan` command and
// consumed by `execute`. It intentionally captures enough context
// (provider, instance, scope) to detect a plan being run against the wrong
// target.
type Plan struct {
	Version   int       `json:"version"`
	Provider  string    `json:"provider"`
	Instance  string    `json:"instance"`
	Scope     PlanScope `json:"scope"`
	CreatedAt time.Time `json:"createdAt"`

	// Actions are the concrete operations discovered during planning.
	Actions []PlannedAction `json:"actions"`

	// Hash is a SHA-256 fingerprint over the canonical plan content
	// (excluding this field), used to detect accidental or malicious
	// modification between `plan` and `execute`.
	Hash string `json:"hash,omitempty"`
}

// PlanScope is the serialized form of the Scope an action set was
// discovered in.
type PlanScope struct {
	Type      ScopeType `json:"type"`
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Recursive bool      `json:"recursive"`
}
