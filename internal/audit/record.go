// Package audit provides an append-only, JSON-Lines audit trail for
// destructive operations, independent from the general-purpose structured
// application log. It never records secrets.
package audit

import (
	"time"

	"github.com/domehahn/housekeeping/internal/domain"
)

// Record is a single audited event: one attempted action and its outcome.
type Record struct {
	RunID                string                 `json:"runId,omitempty"`
	EventID              string                 `json:"eventId,omitempty"`
	Sequence             int                    `json:"sequence,omitempty"`
	Timestamp            time.Time              `json:"timestamp"`
	Provider             string                 `json:"provider"`
	Instance             string                 `json:"instance"`
	AuthenticatedActor   string                 `json:"authenticatedActor,omitempty"`
	AuthenticatedActorID string                 `json:"authenticatedActorId,omitempty"`
	Scope                string                 `json:"scope"`
	PlanHash             string                 `json:"planHash,omitempty"`
	PlanVersion          int                    `json:"planVersion,omitempty"`
	PlanCreatedAt        time.Time              `json:"planCreatedAt,omitempty"`
	ConfigDigest         string                 `json:"configDigest,omitempty"`
	ToolVersion          string                 `json:"toolVersion,omitempty"`
	ToolCommit           string                 `json:"toolCommit,omitempty"`
	ResourceType         domain.ResourceType    `json:"resourceType"`
	ResourceID           string                 `json:"resourceId"`
	ResourceName         string                 `json:"resourceName"`
	Action               domain.ActionType      `json:"action"`
	Phase                string                 `json:"phase,omitempty"` // "intent" | "result"
	Result               domain.ExecutionResult `json:"result,omitempty"`
	Detail               string                 `json:"detail,omitempty"`
	DryRun               bool                   `json:"dryRun"`
	PreviousHash         string                 `json:"previousHash,omitempty"`
	Hash                 string                 `json:"hash,omitempty"`
}

// FromOutcome builds an audit Record from an executed action outcome.
func FromOutcome(now time.Time, providerName, instance, scope string, outcome domain.ActionOutcome) Record {
	return Record{
		Timestamp:    now,
		Provider:     providerName,
		Instance:     instance,
		ResourceType: outcome.Action.ResourceType,
		ResourceID:   outcome.Action.ResourceID,
		ResourceName: outcome.Action.ResourceName,
		Scope:        scope,
		Action:       outcome.Action.Action,
		Phase:        "result",
		Result:       outcome.Result,
		Detail:       outcome.Detail,
	}
}
