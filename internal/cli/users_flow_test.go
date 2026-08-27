package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/domehahn/housekeeping/internal/domain"
)

func TestUsersList_HappyPath(t *testing.T) {
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.members = []domain.User{{ID: "1", Username: "alice"}}
	e := withGroup(testEnv(c), "company", false)

	stdout, _, err := runCmd(newUsersCmd(e), []string{"list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "alice") {
		t.Errorf("expected output to list the discovered user, got:\n%s", stdout)
	}
}

func TestUsersEvaluate_MatchesInactiveUser(t *testing.T) {
	old := time.Now().Add(-100 * 24 * time.Hour)
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.members = []domain.User{
		{ID: "1", Username: "alice", LastLoginAt: domain.Known(&old), LastActivityAt: domain.Known(&old)},
	}
	e := withGroup(testEnv(c), "company", false)

	stdout, _, err := runCmd(newUsersCmd(e), []string{"evaluate", "--inactive-for", "30d"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "alice") {
		t.Errorf("expected alice to be reported as matched, got:\n%s", stdout)
	}
}

func TestUsersEvaluate_ProtectedUsernameNeverMatches(t *testing.T) {
	old := time.Now().Add(-100 * 24 * time.Hour)
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.members = []domain.User{
		{ID: "1", Username: "root", LastLoginAt: domain.Known(&old), LastActivityAt: domain.Known(&old)},
	}
	e := withGroup(testEnv(c), "company", false)
	e.cfg.Users.Protection.Usernames = []string{"root"}

	stdout, _, err := runCmd(newUsersCmd(e), []string{"evaluate", "--inactive-for", "30d"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "protected 1") {
		t.Errorf("expected root to be reported as protected, got:\n%s", stdout)
	}
}

func TestUsersPlan_RemoveFromGroup(t *testing.T) {
	old := time.Now().Add(-100 * 24 * time.Hour)
	c := newFakeClient()
	c.scope = domain.Scope{ID: "1", Path: "company"}
	c.members = []domain.User{
		{ID: "1", Username: "alice", GroupID: "1", LastLoginAt: domain.Known(&old), LastActivityAt: domain.Known(&old)},
	}
	c.info = fakeInfo()
	e := withGroup(testEnv(c), "company", false)

	planPath := t.TempDir() + "/plan.json"
	stdout, _, err := runCmd(newUsersCmd(e), []string{
		"plan", "--inactive-for", "30d", "--action", "remove-from-group", "--output-plan", planPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "Plan written to") {
		t.Errorf("expected confirmation that the plan was written, got:\n%s", stdout)
	}
}
