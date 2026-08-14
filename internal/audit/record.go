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
	Timestamp    time.Time              `json:"timestamp"`
	Provider     string                 `json:"provider"`
	Instance     string                 `json:"instance"`
	ResourceType domain.ResourceType    `json:"resourceType"`
	ResourceID   string                 `json:"resourceId"`
	ResourceName string                 `json:"resourceName"`
	Scope        string                 `json:"scope"`
	Action       domain.ActionType      `json:"action"`
	Result       domain.ExecutionResult `json:"result"`
	Detail       string                 `json:"detail,omitempty"`
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
		Result:       outcome.Result,
		Detail:       outcome.Detail,
	}
}
