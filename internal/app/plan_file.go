package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/domehahn/housekeeping/internal/domain"
)

// SavePlan writes a plan to disk as indented JSON, computing and embedding
// its integrity hash first.
func SavePlan(path string, plan domain.Plan) error {
	plan.Hash = ""
	plan.Hash = computePlanHash(plan)

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write plan file %s: %w", path, err)
	}
	return nil
}

// LoadPlan reads and validates a plan file: JSON well-formedness, a
// supported version, and an intact integrity hash. It performs no network
// I/O and does not by itself check the plan against a live provider
// instance - see VerifyAgainstInstance for that.
func LoadPlan(path string) (domain.Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("read plan file %s: %w", path, err)
	}

	var plan domain.Plan
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&plan); err != nil {
		return domain.Plan{}, fmt.Errorf("parse plan file %s: %w", path, err)
	}

	if plan.Version != domain.PlanVersion {
		return domain.Plan{}, fmt.Errorf("unsupported plan version %d (this build supports version %d)", plan.Version, domain.PlanVersion)
	}
	if plan.Provider == "" || plan.Instance == "" {
		return domain.Plan{}, fmt.Errorf("plan file is missing provider/instance metadata")
	}
	for i, a := range plan.Actions {
		if a.ResourceID == "" {
			return domain.Plan{}, fmt.Errorf("plan action %d is missing a resource ID", i)
		}
	}

	if plan.Hash != "" {
		claimed := plan.Hash
		check := plan
		check.Hash = ""
		if computePlanHash(check) != claimed {
			return domain.Plan{}, fmt.Errorf("plan integrity check failed: file has been modified since it was created")
		}
	}

	return plan, nil
}

// VerifyAgainstInstance guards against a plan created for one provider
// instance being executed against another - see docs/adr and the "Plan
// Integrity" requirement.
func VerifyAgainstInstance(plan domain.Plan, providerName, instance string) error {
	if plan.Provider != providerName {
		return fmt.Errorf("plan was created for provider %q but current provider is %q", plan.Provider, providerName)
	}
	if plan.Instance != instance {
		return fmt.Errorf("plan was created for instance %q but current instance is %q", plan.Instance, instance)
	}
	return nil
}

// computePlanHash returns a SHA-256 fingerprint over the canonical
// (indent-free, key-sorted-by-struct-order) JSON encoding of the plan with
// its Hash field cleared. This is a tamper/accidental-change detector, not
// a cryptographic signature - it protects against silent edits to a plan
// file between `plan` and `execute`, not against a determined attacker who
// can also recompute the hash.
func computePlanHash(plan domain.Plan) string {
	plan.Hash = ""
	data, err := json.Marshal(plan)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
