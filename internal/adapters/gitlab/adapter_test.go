package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/provider"
)

// newTestAdapter starts an httptest.Server driven by handler and returns an
// Adapter pointed at it. No test in this file talks to a real GitLab
// instance.
func newTestAdapter(t *testing.T, handler http.HandlerFunc) *Adapter {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	a, err := New(Options{BaseURL: srv.URL, Token: "test-token", Workers: 2, RetryMax: 1})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		in           string
		wantAPI      string
		wantInstance string
		wantErr      bool
	}{
		{"https://gitlab.example.com", "https://gitlab.example.com/api/v4", "https://gitlab.example.com", false},
		{"https://gitlab.example.com/", "https://gitlab.example.com/api/v4", "https://gitlab.example.com", false},
		{"https://gitlab.example.com/api/v4", "https://gitlab.example.com/api/v4", "https://gitlab.example.com", false},
		{"https://gitlab.example.com/api/v4/", "https://gitlab.example.com/api/v4", "https://gitlab.example.com", false},
		{"gitlab.example.com", "", "", true},
		{"", "", "", true},
	}
	for _, tc := range tests {
		api, instance, err := normalizeBaseURL(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("normalizeBaseURL(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if err == nil && (api != tc.wantAPI || instance != tc.wantInstance) {
			t.Errorf("normalizeBaseURL(%q) = (%q, %q), want (%q, %q)", tc.in, api, instance, tc.wantAPI, tc.wantInstance)
		}
	}
}

func TestAuthentication_TokenHeaderSent(t *testing.T) {
	var gotToken string
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		writeJSON(t, w, map[string]any{"id": 1, "username": "bot", "is_admin": false})
	})

	if _, err := a.CurrentUser(context.Background()); err != nil {
		t.Fatalf("CurrentUser: %v", err)
	}
	if gotToken != "test-token" {
		t.Errorf("expected token header to be sent, got %q", gotToken)
	}
}

func TestGetGroupMember_MapsDirectRole(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/groups/10/members/42" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		writeJSON(t, w, map[string]any{
			"id": 42, "username": "alice", "name": "Alice", "state": "active", "access_level": 50,
		})
	})
	member, err := a.GetGroupMember(context.Background(), "10", "42")
	if err != nil {
		t.Fatalf("GetGroupMember: %v", err)
	}
	if member.GroupID != "10" || member.MembershipOrigin != domain.MembershipDirect || member.AccessLevel != domain.AccessLevelOwner {
		t.Fatalf("unexpected mapped membership: %+v", member)
	}
}

func TestErrorClassification(t *testing.T) {
	tests := []struct {
		status   int
		wantKind provider.Kind
	}{
		{http.StatusUnauthorized, provider.KindAuthentication},
		{http.StatusForbidden, provider.KindAuthorization},
		{http.StatusNotFound, provider.KindNotFound},
		{http.StatusInternalServerError, provider.KindTemporary},
	}
	for _, tc := range tests {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"message":"boom"}`))
			})
			_, err := a.GetProject(context.Background(), "1")
			if err == nil {
				t.Fatal("expected error")
			}
			var pErr *provider.Error
			if !errors.As(err, &pErr) {
				t.Fatalf("expected *provider.Error, got %T: %v", err, err)
			}
			if pErr.Kind != tc.wantKind {
				t.Errorf("Kind = %v, want %v", pErr.Kind, tc.wantKind)
			}
		})
	}
}

func TestErrorClassification_RateLimit(t *testing.T) {
	// 429 is retried by the underlying SDK's retryablehttp transport, so
	// exercising it end-to-end would require several round trips before
	// the retry budget is exhausted and the final classified error is
	// returned. We verify the classifier itself directly instead.
	if kindForStatus(http.StatusTooManyRequests) != provider.KindRateLimit {
		t.Error("expected 429 to classify as KindRateLimit")
	}
}

func TestListProjects_Pagination(t *testing.T) {
	pages := [][]map[string]any{
		{{"id": 1, "name": "a", "path": "a", "path_with_namespace": "g/a"}},
		{{"id": 2, "name": "b", "path": "b", "path_with_namespace": "g/b"}},
		{{"id": 3, "name": "c", "path": "c", "path_with_namespace": "g/c"}},
	}
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		idx := page - 1
		if idx+1 < len(pages) {
			w.Header().Set("X-Next-Page", strconv.Itoa(page+1))
		}
		writeJSON(t, w, pages[idx])
	})

	scope := domain.Scope{ID: "42", Path: "g", GroupIDs: []string{"42"}}
	projects, err := a.ListProjects(context.Background(), scope)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 3 {
		t.Fatalf("expected 3 projects across 3 pages, got %d: %+v", len(projects), projects)
	}
	if projects[2].FullPath != "g/c" {
		t.Errorf("unexpected project on last page: %+v", projects[2])
	}
}

func TestListProjects_ActivityAlwaysKnown(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []map[string]any{
			{"id": 1, "name": "a", "path": "a", "path_with_namespace": "g/a", "last_activity_at": nil},
		})
	})
	scope := domain.Scope{ID: "1", GroupIDs: []string{"1"}}
	projects, err := a.ListProjects(context.Background(), scope)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if !projects[0].LastActivityAt.IsKnown() {
		t.Error("project last_activity_at must always be Known (not admin-gated) even when nil")
	}
	if projects[0].LastActivityAt.At != nil {
		t.Error("expected nil last_activity_at to map to a known-nil timestamp")
	}
}

func TestListSubGroups_RecursiveWalkAndDedup(t *testing.T) {
	// Tree: root(1) -> [child(2), child(3)]; child(2) -> [grandchild(4)]
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/groups/g":
			writeJSON(t, w, map[string]any{"id": 1, "name": "root", "path": "g", "full_path": "g"})
		case "/api/v4/groups/1/subgroups":
			writeJSON(t, w, []map[string]any{
				{"id": 2, "name": "child2", "path": "child2", "full_path": "g/child2", "parent_id": 1},
				{"id": 3, "name": "child3", "path": "child3", "full_path": "g/child3", "parent_id": 1},
			})
		case "/api/v4/groups/2/subgroups":
			writeJSON(t, w, []map[string]any{
				{"id": 4, "name": "grandchild", "path": "gc", "full_path": "g/child2/gc", "parent_id": 2},
			})
		default:
			writeJSON(t, w, []map[string]any{})
		}
	})

	scope, groups, err := a.ResolveGroupScope(context.Background(), "g", true)
	if err != nil {
		t.Fatalf("ResolveGroupScope: %v", err)
	}
	if len(groups) != 4 {
		t.Fatalf("expected root + 3 descendants = 4 groups, got %d: %+v", len(groups), groups)
	}
	if len(scope.GroupIDs) != 4 {
		t.Errorf("expected scope.GroupIDs to contain all 4 group IDs, got %v", scope.GroupIDs)
	}
}

func TestListGroupMembers_DirectOnlyAndDedup(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/groups/1/members":
			writeJSON(t, w, []map[string]any{
				{"id": 10, "username": "alice", "name": "Alice", "state": "active", "access_level": 30},
			})
		case "/api/v4/groups/2/members":
			writeJSON(t, w, []map[string]any{
				{"id": 10, "username": "alice", "name": "Alice", "state": "active", "access_level": 40}, // same user, seen again
				{"id": 11, "username": "bob", "name": "Bob", "state": "active", "access_level": 20},
			})
		case "/api/v4/user":
			writeJSON(t, w, map[string]any{"id": 999, "username": "svc", "is_admin": false})
		default:
			writeJSON(t, w, []map[string]any{})
		}
	})

	scope := domain.Scope{ID: "1", GroupIDs: []string{"1", "2"}}
	members, err := a.ListGroupMembers(context.Background(), scope)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected 2 unique members (alice deduped), got %d: %+v", len(members), members)
	}
	for _, m := range members {
		if m.Username == "alice" && m.GroupID != "1" {
			t.Errorf("expected alice's membership to be attributed to the first group encountered (1), got %s", m.GroupID)
		}
	}
	// Non-admin token: activity fields must remain unknown, never silently
	// treated as inactive.
	for _, m := range members {
		if m.LastLoginAt.IsKnown() || m.LastActivityAt.IsKnown() {
			t.Errorf("expected unknown activity for non-admin token, got %+v", m)
		}
	}
}

func TestListGroupMembers_AdminEnrichesActivity(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/groups/1/members":
			writeJSON(t, w, []map[string]any{
				{"id": 10, "username": "alice", "name": "Alice", "state": "active", "access_level": 30},
			})
		case "/api/v4/user":
			writeJSON(t, w, map[string]any{"id": 999, "username": "admin", "is_admin": true})
		case "/api/v4/users/10":
			writeJSON(t, w, map[string]any{
				"id": 10, "username": "alice", "state": "active",
				"last_sign_in_at":  "2026-01-01T00:00:00Z",
				"last_activity_on": "2026-01-02",
			})
		default:
			writeJSON(t, w, []map[string]any{})
		}
	})

	scope := domain.Scope{ID: "1", GroupIDs: []string{"1"}}
	members, err := a.ListGroupMembers(context.Background(), scope)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}
	if !members[0].LastLoginAt.IsKnown() || members[0].LastLoginAt.At == nil {
		t.Error("expected admin-enriched last login to be known")
	}
	if !members[0].LastActivityAt.IsKnown() || members[0].LastActivityAt.At == nil {
		t.Error("expected admin-enriched last activity to be known")
	}
}

func TestDeleteProject(t *testing.T) {
	var gotMethod, gotPath string
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusAccepted)
	})
	if err := a.DeleteProject(context.Background(), "123"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("expected DELETE, got %s", gotMethod)
	}
	if gotPath != "/api/v4/projects/123" {
		t.Errorf("unexpected path: %s", gotPath)
	}
}

func TestRemoveGroupMember(t *testing.T) {
	var gotMethod, gotPath string
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	if err := a.RemoveGroupMember(context.Background(), "1", "10"); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("expected DELETE, got %s", gotMethod)
	}
	if gotPath != "/api/v4/groups/1/members/10" {
		t.Errorf("unexpected path: %s", gotPath)
	}
}

func TestRemoveGroupMember_NotFoundClassifiedCorrectly(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"404 Not found"}`))
	})
	err := a.RemoveGroupMember(context.Background(), "1", "10")
	if err == nil {
		t.Fatal("expected error")
	}
	var pErr *provider.Error
	if !errors.As(err, &pErr) || pErr.Kind != provider.KindNotFound {
		t.Errorf("expected KindNotFound, got %v", err)
	}
}

func TestListBillableGroupMembers_Pagination(t *testing.T) {
	pages := [][]map[string]any{
		{{"id": 10, "username": "alice"}},
		{{"id": 11, "username": "bob"}},
	}
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/groups/100/billable_members" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		idx := page - 1
		if idx+1 < len(pages) {
			w.Header().Set("X-Next-Page", strconv.Itoa(page+1))
		}
		writeJSON(t, w, pages[idx])
	})

	billable, err := a.ListBillableGroupMembers(context.Background(), "100")
	if err != nil {
		t.Fatalf("ListBillableGroupMembers: %v", err)
	}
	if len(billable) != 2 || !billable["10"] || !billable["11"] {
		t.Errorf("expected billable set {10,11}, got %v", billable)
	}
}

func TestListBillableGroupMembers_RequiresOwner(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"403 Forbidden - You must have owner access"}`))
	})
	_, err := a.ListBillableGroupMembers(context.Background(), "100")
	if err == nil {
		t.Fatal("expected an error")
	}
	var pErr *provider.Error
	if !errors.As(err, &pErr) || pErr.Kind != provider.KindAuthorization {
		t.Errorf("expected KindAuthorization, got %v", err)
	}
}

func TestListUserMemberships_FiltersToNamespaceAndMaps(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/users/10/memberships" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("type"); got != "Namespace" {
			t.Errorf("expected type=Namespace filter, got %q", got)
		}
		writeJSON(t, w, []map[string]any{
			{"source_id": 5, "source_name": "other-group", "source_type": "Namespace", "access_level": 20},
		})
	})

	memberships, err := a.ListUserMemberships(context.Background(), "10")
	if err != nil {
		t.Fatalf("ListUserMemberships: %v", err)
	}
	if len(memberships) != 1 {
		t.Fatalf("expected 1 membership, got %d", len(memberships))
	}
	if memberships[0].SourceType != domain.MembershipSourceGroup || memberships[0].SourceID != "5" || memberships[0].AccessLevel != domain.AccessLevelReporter {
		t.Errorf("unexpected mapping: %+v", memberships[0])
	}
}

func TestListUserMemberships_RequiresAdmin(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"403 Forbidden"}`))
	})
	_, err := a.ListUserMemberships(context.Background(), "10")
	if err == nil {
		t.Fatal("expected an error")
	}
	var pErr *provider.Error
	if !errors.As(err, &pErr) || pErr.Kind != provider.KindAuthorization {
		t.Errorf("expected KindAuthorization, got %v", err)
	}
}
