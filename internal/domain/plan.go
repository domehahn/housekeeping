package domain

import "time"

// PlanVersion is the current on-disk plan schema version. Execution refuses
// to run a plan whose version it does not understand - see
// docs/adr/0002-plan-before-execute.md.
const PlanVersion = 4

// MinimumSupportedPlanVersion preserves execution of existing single-tag
// plans while new plans use the multi-tag schema.
const MinimumSupportedPlanVersion = 2

// PlanProvenance records provenance details about how a plan file was generated.
type PlanProvenance struct {
	ToolVersion  string `json:"toolVersion,omitempty"`
	ToolCommit   string `json:"toolCommit,omitempty"`
	ConfigDigest string `json:"configDigest,omitempty"`
	PolicyDigest string `json:"policyDigest,omitempty"`
}

// PlanSignature holds cryptographic signature metadata verifying plan authenticity.
type PlanSignature struct {
	Algorithm string `json:"algorithm,omitempty"` // e.g., "ed25519"
	KeyID     string `json:"keyId,omitempty"`
	Value     string `json:"value,omitempty"`
}

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
	ExpiresAt time.Time `json:"expiresAt,omitempty"`

	// Discovery records the population count evaluated per resource type.
	Discovery map[ResourceType]int `json:"discovery,omitempty"`

	// Actions are the concrete operations discovered during planning.
	Actions []PlannedAction `json:"actions"`

	// Provenance records build/version/config metadata.
	Provenance *PlanProvenance `json:"provenance,omitempty"`

	// Signature holds an optional cryptographic signature.
	Signature *PlanSignature `json:"signature,omitempty"`

	// Hash is a SHA-256 fingerprint over the canonical plan content
	// (excluding this field and Signature), used to detect accidental or malicious
	// modification between `plan` and `execute`.
	Hash string `json:"hash,omitempty"`
}

// IsExpired reports whether the plan has passed its explicit expiration timestamp.
func (p Plan) IsExpired(now time.Time) bool {
	if p.ExpiresAt.IsZero() {
		return false
	}
	return now.After(p.ExpiresAt)
}

// PlanScope is the serialized form of the Scope an action set was
// discovered in.
type PlanScope struct {
	Type      ScopeType `json:"type"`
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Recursive bool      `json:"recursive"`
}

// Scope converts PlanScope into a domain.Scope.
func (s PlanScope) Scope() Scope {
	return Scope{Type: s.Type, ID: s.ID, Path: s.Path, Recursive: s.Recursive}
}
