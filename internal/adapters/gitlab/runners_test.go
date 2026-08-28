package gitlab

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/provider"
)

func TestListRunnersForProjects_DedupAndBlastRadius(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/projects/1/runners"):
			writeJSON(t, w, []map[string]any{{"id": 100, "description": "shared-runner"}})
		case strings.Contains(r.URL.Path, "/projects/2/runners"):
			writeJSON(t, w, []map[string]any{{"id": 100, "description": "shared-runner"}}) // same runner, seen again
		case strings.HasSuffix(r.URL.Path, "/runners/100"):
			writeJSON(t, w, map[string]any{
				"id": 100, "description": "shared-runner", "is_shared": true, "runner_type": "project_type",
				"tag_list": []string{"existing-tag"},
				"projects": []map[string]any{
					{"id": 1, "path_with_namespace": "group/in-scope-a"},
					{"id": 2, "path_with_namespace": "group/in-scope-b"},
					{"id": 3, "path_with_namespace": "other-group/out-of-scope"},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})

	scope := domain.Scope{Type: domain.ScopeTypeGroup, ID: "10", GroupIDs: []string{"10"}, Recursive: true}
	runners, err := a.ListRunnersForProjects(context.Background(), scope, []string{"1", "2"})
	if err != nil {
		t.Fatalf("ListRunnersForProjects: %v", err)
	}
	if len(runners) != 1 {
		t.Fatalf("expected 1 de-duplicated runner, got %d: %+v", len(runners), runners)
	}
	r := runners[0]
	if !r.Shared {
		t.Error("expected Shared=true")
	}
	if !r.ImpactKnown {
		t.Errorf("expected project runner impact to be known, reason=%q", r.ImpactReason)
	}
	if len(r.InScopeProjectPaths) != 2 {
		t.Errorf("expected 2 in-scope projects, got %v", r.InScopeProjectPaths)
	}
	if len(r.OutOfScopeProjectPaths) != 1 || r.OutOfScopeProjectPaths[0] != "other-group/out-of-scope" {
		t.Errorf("expected 1 out-of-scope project 'other-group/out-of-scope', got %v", r.OutOfScopeProjectPaths)
	}
}

func TestListRunnersForProjects_Pagination(t *testing.T) {
	pages := [][]map[string]any{
		{{"id": 100, "description": "runner-a"}},
		{{"id": 200, "description": "runner-b"}},
	}
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/projects/1/runners"):
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			if page == 0 {
				page = 1
			}
			idx := page - 1
			if idx+1 < len(pages) {
				w.Header().Set("X-Next-Page", strconv.Itoa(page+1))
			}
			writeJSON(t, w, pages[idx])
		case strings.Contains(r.URL.Path, "/runners/100"):
			writeJSON(t, w, map[string]any{"id": 100, "runner_type": "project_type", "tag_list": []string{}, "projects": []map[string]any{{"id": 1, "path_with_namespace": "group/a"}}})
		case strings.Contains(r.URL.Path, "/runners/200"):
			writeJSON(t, w, map[string]any{"id": 200, "runner_type": "project_type", "tag_list": []string{}, "projects": []map[string]any{{"id": 1, "path_with_namespace": "group/a"}}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})

	runners, err := a.ListRunnersForProjects(context.Background(), domain.Scope{}, []string{"1"})
	if err != nil {
		t.Fatalf("ListRunnersForProjects: %v", err)
	}
	if len(runners) != 2 {
		t.Fatalf("expected 2 runners across 2 pages, got %d: %+v", len(runners), runners)
	}
}

func TestGetRunnerTags(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"id": 5, "tag_list": []string{"a", "b"}})
	})
	tags, err := a.GetRunnerTags(context.Background(), "5")
	if err != nil {
		t.Fatalf("GetRunnerTags: %v", err)
	}
	if len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Errorf("unexpected tags: %v", tags)
	}
}

func TestUpdateRunnerTags(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody []byte
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if r.Method == http.MethodGet {
			writeJSON(t, w, map[string]any{"id": 5, "tag_list": []string{"a"}})
			return
		}
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = buf
		writeJSON(t, w, map[string]any{"id": 5, "tag_list": []string{"a", "b"}})
	})
	if err := a.UpdateRunnerTags(context.Background(), "5", []string{"a"}, []string{"a", "b"}); err != nil {
		t.Fatalf("UpdateRunnerTags: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("expected PUT, got %s", gotMethod)
	}
	if !strings.HasSuffix(gotPath, "/runners/5") {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if !strings.Contains(string(gotBody), "tag_list") {
		t.Errorf("expected request body to contain tag_list, got %s", gotBody)
	}
}

func TestUpdateRunnerTags_RejectsConcurrentTagChange(t *testing.T) {
	putCalled := false
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			putCalled = true
		}
		writeJSON(t, w, map[string]any{"id": 5, "tag_list": []string{"a", "concurrent"}})
	})
	err := a.UpdateRunnerTags(context.Background(), "5", []string{"a"}, []string{"a", "wanted"})
	var pErr *provider.Error
	if !errors.As(err, &pErr) || pErr.Kind != provider.KindConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
	if putCalled {
		t.Fatal("must not overwrite tags after a concurrent change")
	}
}

func TestAssessRunnerImpact_GroupRunnerRequiresOwningRecursiveScope(t *testing.T) {
	tests := []struct {
		name   string
		scope  domain.Scope
		runner domain.Runner
		known  bool
	}{
		{
			name:   "owning recursive scope is safe",
			scope:  domain.Scope{Recursive: true, GroupIDs: []string{"10", "11"}},
			runner: domain.Runner{RunnerType: "group_type", OwnerGroupIDs: []string{"11"}},
			known:  true,
		},
		{
			name:   "non-recursive scope is incomplete",
			scope:  domain.Scope{Recursive: false, GroupIDs: []string{"10"}},
			runner: domain.Runner{RunnerType: "group_type", OwnerGroupIDs: []string{"10"}},
		},
		{
			name:   "inherited ancestor is outside scope",
			scope:  domain.Scope{Recursive: true, GroupIDs: []string{"11"}},
			runner: domain.Runner{RunnerType: "group_type", OwnerGroupIDs: []string{"10"}},
		},
		{
			name:   "instance runner is unbounded",
			scope:  domain.Scope{Recursive: true, GroupIDs: []string{"10"}},
			runner: domain.Runner{RunnerType: "instance_type"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assessRunnerImpact(&tc.runner, tc.scope)
			if tc.runner.ImpactKnown != tc.known {
				t.Fatalf("ImpactKnown=%v, want %v (reason=%q)", tc.runner.ImpactKnown, tc.known, tc.runner.ImpactReason)
			}
			if !tc.known && tc.runner.ImpactReason == "" {
				t.Fatal("blocked runner must explain why its impact is unprovable")
			}
		})
	}
}
