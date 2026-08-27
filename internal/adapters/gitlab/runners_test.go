package gitlab

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
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
				"id": 100, "description": "shared-runner", "is_shared": true,
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

	runners, err := a.ListRunnersForProjects(context.Background(), []string{"1", "2"})
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
			writeJSON(t, w, map[string]any{"id": 100, "tag_list": []string{}})
		case strings.Contains(r.URL.Path, "/runners/200"):
			writeJSON(t, w, map[string]any{"id": 200, "tag_list": []string{}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})

	runners, err := a.ListRunnersForProjects(context.Background(), []string{"1"})
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
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = buf
		writeJSON(t, w, map[string]any{"id": 5, "tag_list": []string{"a", "b"}})
	})
	if err := a.UpdateRunnerTags(context.Background(), "5", []string{"a", "b"}); err != nil {
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
