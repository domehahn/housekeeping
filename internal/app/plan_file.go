package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

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
	data, err := os.ReadFile(path) //nolint:gosec // path is the operator's own plan-file CLI argument, not attacker-controlled input
	if err != nil {
		return domain.Plan{}, fmt.Errorf("read plan file %s: %w", path, err)
	}

	var plan domain.Plan
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&plan); err != nil {
		return domain.Plan{}, fmt.Errorf("parse plan file %s: %w", path, err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return domain.Plan{}, fmt.Errorf("parse plan file %s: trailing JSON content is not allowed", path)
	}

	if plan.Version < domain.MinimumSupportedPlanVersion || plan.Version > domain.PlanVersion {
		return domain.Plan{}, fmt.Errorf("unsupported plan version %d (this build supports versions %d-%d)", plan.Version, domain.MinimumSupportedPlanVersion, domain.PlanVersion)
	}
	if plan.Provider == "" || plan.Instance == "" || plan.Scope.ID == "" || plan.Scope.Path == "" {
		return domain.Plan{}, fmt.Errorf("plan file is missing provider/instance/scope metadata")
	}
	for i, a := range plan.Actions {
		if err := validatePlannedAction(a); err != nil {
			return domain.Plan{}, fmt.Errorf("plan action %d: %w", i, err)
		}
	}

	if plan.Hash == "" {
		return domain.Plan{}, fmt.Errorf("plan integrity check failed: hash is required")
	}
	claimed := plan.Hash
	check := plan
	check.Hash = ""
	if computePlanHash(check) != claimed {
		return domain.Plan{}, fmt.Errorf("plan integrity check failed: file has been modified since it was created")
	}

	return plan, nil
}

// validatePlannedAction checks one action's shape against what its
// resource type/action combination requires. Each resource type has its
// own small helper so this stays a dispatch table rather than one large
// function.
func validatePlannedAction(a domain.PlannedAction) error {
	if a.ResourceID == "" || a.ResourceName == "" || a.EvaluatedAt.IsZero() {
		return fmt.Errorf("resource ID, resource name, and evaluatedAt are required")
	}
	switch a.ResourceType {
	case domain.ResourceTypeProject:
		return validateProjectAction(a)
	case domain.ResourceTypeUser:
		return validateUserAction(a)
	case domain.ResourceTypePipelineConfig:
		return validatePipelineConfigAction(a)
	case domain.ResourceTypeRunner:
		return validateRunnerAction(a)
	default:
		return fmt.Errorf("unsupported resource type %q", a.ResourceType)
	}
}

func validateProjectAction(a domain.PlannedAction) error {
	if a.GroupID != "" || a.AccessLevel != "" {
		return fmt.Errorf("project action must not contain user membership metadata")
	}
	switch a.Action {
	case domain.ActionReport, domain.ActionDeleteProject, domain.ActionArchiveProject:
		return nil
	default:
		return fmt.Errorf("action %q is invalid for resource type %q", a.Action, a.ResourceType)
	}
}

func validateUserAction(a domain.PlannedAction) error {
	switch a.Action {
	case domain.ActionReport, domain.ActionBlockUser:
		return nil
	case domain.ActionRemoveGroupMember:
		if a.GroupID == "" || a.AccessLevel == "" || a.AccessLevel == domain.AccessLevelUnknown {
			return fmt.Errorf("remove-from-group requires groupId and a known accessLevel")
		}
		return nil
	default:
		return fmt.Errorf("action %q is invalid for resource type %q", a.Action, a.ResourceType)
	}
}

func validatePipelineConfigAction(a domain.PlannedAction) error {
	if a.GroupID != "" || a.AccessLevel != "" {
		return fmt.Errorf("pipeline_config action must not contain user membership metadata")
	}
	switch a.Action {
	case domain.ActionReport:
		return nil
	case domain.ActionAddPipelineTag:
		if err := validateActionTags(a); err != nil {
			return fmt.Errorf("add-pipeline-tag: %w", err)
		}
		return nil
	case domain.ActionReplacePipelineTag:
		if err := validateActionTagRenames(a); err != nil {
			return fmt.Errorf("replace-pipeline-tag: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("action %q is invalid for resource type %q", a.Action, a.ResourceType)
	}
}

func validateRunnerAction(a domain.PlannedAction) error {
	if a.GroupID != "" || a.AccessLevel != "" {
		return fmt.Errorf("runner action must not contain user membership metadata")
	}
	switch a.Action {
	case domain.ActionReport:
		return nil
	case domain.ActionAddRunnerTag:
		if err := validateActionTags(a); err != nil {
			return fmt.Errorf("add-runner-tag: %w", err)
		}
		if a.OutOfScopeProjectCount != len(a.OutOfScopeProjectPaths) {
			return fmt.Errorf("outOfScopeProjectCount must match the number of outOfScopeProjectPaths")
		}
		return nil
	case domain.ActionReplaceRunnerTag:
		if err := validateActionTagRenames(a); err != nil {
			return fmt.Errorf("replace-runner-tag: %w", err)
		}
		if a.OutOfScopeProjectCount != len(a.OutOfScopeProjectPaths) {
			return fmt.Errorf("outOfScopeProjectCount must match the number of outOfScopeProjectPaths")
		}
		return nil
	default:
		return fmt.Errorf("action %q is invalid for resource type %q", a.Action, a.ResourceType)
	}
}

func validateActionTags(a domain.PlannedAction) error {
	tags := a.Tags()
	if len(tags) == 0 {
		return fmt.Errorf("at least one tagValue/tagValues entry is required")
	}
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if strings.TrimSpace(tag) == "" {
			return fmt.Errorf("tags must not be empty or whitespace")
		}
		if strings.TrimSpace(tag) != tag {
			return fmt.Errorf("tag %q must not contain leading or trailing whitespace", tag)
		}
		if seen[tag] {
			return fmt.Errorf("duplicate tag %q", tag)
		}
		seen[tag] = true
	}
	if a.TagValue != "" && len(a.TagValues) > 0 {
		return fmt.Errorf("legacy tagValue and tagValues are mutually exclusive")
	}
	return nil
}

func validateActionTagRenames(a domain.PlannedAction) error {
	if len(a.TagRenames) == 0 {
		return fmt.Errorf("at least one tagRenames entry is required")
	}
	seen := make(map[string]bool, len(a.TagRenames))
	for _, r := range a.TagRenames {
		old := strings.TrimSpace(r.Old)
		next := strings.TrimSpace(r.New)
		if old == "" || next == "" {
			return fmt.Errorf("tagRenames old/new must not be empty or whitespace")
		}
		if old != r.Old || next != r.New {
			return fmt.Errorf("tagRenames old/new must not contain leading or trailing whitespace")
		}
		if old == next {
			return fmt.Errorf("tagRenames old and new must differ (got %q)", old)
		}
		if seen[old] {
			return fmt.Errorf("duplicate tagRenames old value %q", old)
		}
		seen[old] = true
	}
	return nil
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
