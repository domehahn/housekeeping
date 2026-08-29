package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/domehahn/housekeeping/internal/domain"
)

func samplePlan() domain.Plan {
	return domain.Plan{
		Version:   domain.PlanVersion,
		Provider:  "gitlab",
		Instance:  "https://gitlab.example.com",
		Scope:     domain.PlanScope{Type: domain.ScopeTypeGroup, ID: "1", Path: "engineering"},
		CreatedAt: time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC),
		Actions: []domain.PlannedAction{
			{ResourceType: domain.ResourceTypeUser, ResourceID: "42", ResourceName: "alice", GroupID: "1", AccessLevel: domain.AccessLevelDeveloper, Action: domain.ActionRemoveGroupMember, Reason: []string{"inactive"}, EvaluatedAt: time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)},
		},
	}
}

func TestLoadPlan_RejectsMissingHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	data, err := json.Marshal(samplePlan())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPlan(path); err == nil {
		t.Error("expected a missing plan hash to be rejected")
	}
}

func TestLoadPlan_RejectsActionResourceMismatch(t *testing.T) {
	p := samplePlan()
	p.Actions[0].Action = domain.ActionDeleteProject
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := SavePlan(path, p); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPlan(path); err == nil {
		t.Error("expected a project deletion action targeting a user to be rejected")
	}
}

func TestSaveAndLoadPlan_PipelineConfigAction(t *testing.T) {
	p := samplePlan()
	p.Actions = []domain.PlannedAction{{
		ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", ResourceName: "company/a",
		Action: domain.ActionAddPipelineTag, TagValue: "k8s-runner", Reason: []string{"missing"},
		EvaluatedAt: p.CreatedAt,
	}}
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := SavePlan(path, p); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	if _, err := LoadPlan(path); err != nil {
		t.Errorf("expected a valid pipeline_config action to load, got: %v", err)
	}
}

func TestSaveAndLoadPlan_MultiplePipelineTags(t *testing.T) {
	p := samplePlan()
	p.Actions = []domain.PlannedAction{{
		ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", ResourceName: "company/a",
		Action: domain.ActionAddPipelineTag, TagValues: []string{"AKS", "production"}, EvaluatedAt: p.CreatedAt,
	}}
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := SavePlan(path, p); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPlan(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(loaded.Actions[0].Tags(), []string{"AKS", "production"}) {
		t.Fatalf("loaded tags = %v", loaded.Actions[0].Tags())
	}
}

func TestLoadPlan_RejectsPipelineTagActionWithoutTagValue(t *testing.T) {
	p := samplePlan()
	p.Actions = []domain.PlannedAction{{
		ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", ResourceName: "company/a",
		Action: domain.ActionAddPipelineTag, EvaluatedAt: p.CreatedAt, // no TagValue
	}}
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := SavePlan(path, p); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	if _, err := LoadPlan(path); err == nil {
		t.Error("expected an add-pipeline-tag action with no tagValue to be rejected")
	}
}

func TestLoadPlan_RejectsWhitespacePipelineTag(t *testing.T) {
	p := samplePlan()
	p.Actions = []domain.PlannedAction{{
		ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", ResourceName: "company/a",
		Action: domain.ActionAddPipelineTag, TagValues: []string{"  "}, EvaluatedAt: p.CreatedAt,
	}}
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := SavePlan(path, p); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPlan(path); err == nil {
		t.Error("expected a whitespace-only pipeline tag to be rejected")
	}
}

func TestSaveAndLoadPlan_RunnerTagAction(t *testing.T) {
	p := samplePlan()
	p.Actions = []domain.PlannedAction{{
		ResourceType: domain.ResourceTypeRunner, ResourceID: "500", ResourceName: "shared-runner",
		Action: domain.ActionAddRunnerTag, TagValue: "k8s-runner", Reason: []string{"missing"},
		OutOfScopeProjectCount: 1, OutOfScopeProjectPaths: []string{"other/project"},
		EvaluatedAt: p.CreatedAt,
	}}
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := SavePlan(path, p); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	if _, err := LoadPlan(path); err != nil {
		t.Errorf("expected a valid runner action to load, got: %v", err)
	}
}

func TestLoadPlan_RejectsRunnerActionWithMismatchedOutOfScopeCount(t *testing.T) {
	p := samplePlan()
	p.Actions = []domain.PlannedAction{{
		ResourceType: domain.ResourceTypeRunner, ResourceID: "500", ResourceName: "shared-runner",
		Action: domain.ActionAddRunnerTag, TagValue: "k8s-runner",
		OutOfScopeProjectCount: 5, OutOfScopeProjectPaths: []string{"other/project"}, // 5 != 1
		EvaluatedAt: p.CreatedAt,
	}}
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := SavePlan(path, p); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	if _, err := LoadPlan(path); err == nil {
		t.Error("expected a mismatched outOfScopeProjectCount to be rejected")
	}
}

func TestSaveAndLoadPlan_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	original := samplePlan()

	if err := SavePlan(path, original); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	loaded, err := LoadPlan(path)
	if err != nil {
		t.Fatalf("LoadPlan: %v", err)
	}
	if loaded.Provider != original.Provider || loaded.Instance != original.Instance {
		t.Errorf("round-tripped plan does not match: %+v", loaded)
	}
	if loaded.Hash == "" {
		t.Error("expected a non-empty hash to have been written")
	}
}

func TestLoadPlan_RejectsTamperedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := SavePlan(path, samplePlan()); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := []byte(replaceOnce(string(data), `"alice"`, `"mallory"`))
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadPlan(path); err == nil {
		t.Error("expected an error loading a plan whose content was modified after the hash was computed")
	}
}

func TestLoadPlan_RejectsUnsupportedVersion(t *testing.T) {
	p := samplePlan()
	p.Version = 999
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := SavePlan(path, p); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	if _, err := LoadPlan(path); err == nil {
		t.Error("expected an error for an unsupported plan version")
	}
}

func TestVerifyAgainstInstance(t *testing.T) {
	p := samplePlan()

	if err := VerifyAgainstInstance(p, "gitlab", "https://gitlab.example.com"); err != nil {
		t.Errorf("unexpected error for matching provider/instance: %v", err)
	}
	if err := VerifyAgainstInstance(p, "gitlab", "https://gitlab-b.example.com"); err == nil {
		t.Error("expected an error: plan must not be executable against a different instance")
	}
	if err := VerifyAgainstInstance(p, "github", "https://gitlab.example.com"); err == nil {
		t.Error("expected an error: plan must not be executable against a different provider")
	}
}

func replaceOnce(s, old, new string) string {
	idx := indexOf(s, old)
	if idx < 0 {
		return s
	}
	return s[:idx] + new + s[idx+len(old):]
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
