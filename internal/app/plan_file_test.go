package app

import (
	"os"
	"path/filepath"
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
			{ResourceType: domain.ResourceTypeUser, ResourceID: "42", ResourceName: "alice", GroupID: "1", Action: domain.ActionRemoveGroupMember, Reason: []string{"inactive"}},
		},
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
