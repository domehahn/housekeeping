// Package gitlab_test contains opt-in integration tests that exercise the
// real GitLab adapter against a live GitLab instance. They are skipped
// unless GITLAB_INTEGRATION_TEST=true, GITLAB_URL, and GITLAB_TOKEN are all
// set - `go test ./...` in CI or on a developer machine never touches the
// network by default.
//
// Destructive tests (project deletion, group member removal) additionally
// require GITLAB_INTEGRATION_ALLOW_DESTRUCTIVE=true and a dedicated,
// disposable GITLAB_INTEGRATION_GROUP; they must never run against a
// production instance or group.
package gitlab_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/domehahn/housekeeping/internal/adapters/gitlab"
)

func requireIntegrationEnv(t *testing.T) (baseURL, token string) {
	t.Helper()
	if os.Getenv("GITLAB_INTEGRATION_TEST") != "true" {
		t.Skip("skipping: set GITLAB_INTEGRATION_TEST=true to run integration tests against a real GitLab instance")
	}
	baseURL = os.Getenv("GITLAB_URL")
	token = os.Getenv("GITLAB_TOKEN")
	if baseURL == "" || token == "" {
		t.Fatal("GITLAB_INTEGRATION_TEST=true requires GITLAB_URL and GITLAB_TOKEN to also be set")
	}
	return baseURL, token
}

// TestIntegration_ReadOnly exercises read-only adapter operations
// (Info, Capabilities, CurrentUser) against a real instance. Safe to run
// in any environment with a valid token - it never mutates anything.
func TestIntegration_ReadOnly(t *testing.T) {
	baseURL, token := requireIntegrationEnv(t)

	adapter, err := gitlab.New(gitlab.Options{BaseURL: baseURL, Token: token, Workers: 5})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	info, err := adapter.Info(ctx)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.AuthenticatedAs == "" {
		t.Error("expected a non-empty authenticated identity")
	}

	if _, err := adapter.Capabilities(ctx); err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
}

// TestIntegration_ScopeAndDiscovery lists groups/projects/members for a
// group named by GITLAB_INTEGRATION_GROUP, read-only.
func TestIntegration_ScopeAndDiscovery(t *testing.T) {
	baseURL, token := requireIntegrationEnv(t)
	group := os.Getenv("GITLAB_INTEGRATION_GROUP")
	if group == "" {
		t.Skip("skipping: set GITLAB_INTEGRATION_GROUP to a disposable test group to run discovery tests")
	}

	adapter, err := gitlab.New(gitlab.Options{BaseURL: baseURL, Token: token, Workers: 5})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	scope, groups, err := adapter.ResolveGroupScope(ctx, group, true)
	if err != nil {
		t.Fatalf("ResolveGroupScope: %v", err)
	}
	if len(groups) == 0 {
		t.Error("expected at least the root group to be returned")
	}

	if _, err := adapter.ListProjects(ctx, scope); err != nil {
		t.Errorf("ListProjects: %v", err)
	}
	if _, err := adapter.ListGroupMembers(ctx, scope); err != nil {
		t.Errorf("ListGroupMembers: %v", err)
	}
}

// TestIntegration_DestructiveGuard documents (and enforces) that
// destructive integration tests do not run without an explicit,
// additional opt-in on top of GITLAB_INTEGRATION_TEST. There is
// deliberately no destructive integration test implemented here - a
// scenario that deletes real projects or removes real group members
// belongs in a manually-run, disposable environment, not in a suite
// anyone can trigger via `go test ./...`.
func TestIntegration_DestructiveGuard(t *testing.T) {
	requireIntegrationEnv(t)
	if os.Getenv("GITLAB_INTEGRATION_ALLOW_DESTRUCTIVE") != "true" {
		t.Skip("skipping: destructive integration tests require GITLAB_INTEGRATION_ALLOW_DESTRUCTIVE=true (not implemented in this suite - see test file comment)")
	}
	t.Skip("no destructive integration scenarios are implemented; run cleanup flows manually against a disposable instance instead")
}
