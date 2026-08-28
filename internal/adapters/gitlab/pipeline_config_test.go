package gitlab

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestGetPipelineConfig_ReturnsContentWhenFileExists(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/projects/1"):
			writeJSON(t, w, map[string]any{"id": 1, "default_branch": "main"})
		case strings.Contains(r.URL.Path, "/repository/files/"):
			if got := r.URL.Query().Get("ref"); got != "main" {
				t.Errorf("expected ref=main, got %q", got)
			}
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("build-job:\n  script: [\"x\"]\n"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})

	content, exists, err := a.GetPipelineConfig(context.Background(), "1")
	if err != nil {
		t.Fatalf("GetPipelineConfig: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true")
	}
	if !strings.Contains(string(content), "build-job") {
		t.Errorf("unexpected content: %s", content)
	}
}

func TestGetPipelineConfig_NoFileReturnsExistsFalseNotError(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/projects/1"):
			writeJSON(t, w, map[string]any{"id": 1, "default_branch": "main"})
		case strings.Contains(r.URL.Path, "/repository/files/"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"404 File Not Found"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})

	content, exists, err := a.GetPipelineConfig(context.Background(), "1")
	if err != nil {
		t.Fatalf("expected no error for a missing CI file, got: %v", err)
	}
	if exists {
		t.Error("expected exists=false")
	}
	if content != nil {
		t.Errorf("expected nil content, got %q", content)
	}
}

func TestGetPipelineConfig_EmptyRepositoryReturnsExistsFalse(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"id": 1, "default_branch": ""})
	})

	_, exists, err := a.GetPipelineConfig(context.Background(), "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected exists=false for an empty repository (no default branch)")
	}
}

func TestProposePipelineTagChange_HappyPath(t *testing.T) {
	var gotBranchReq, gotFileReq, gotMRReq bool
	patched := []byte("default:\n  tags:\n    - k8s-runner\n")
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/projects/1"):
			writeJSON(t, w, map[string]any{"id": 1, "default_branch": "main"})
		case strings.Contains(r.URL.Path, "/repository/branches/") && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"404 Branch Not Found"}`))
		case strings.Contains(r.URL.Path, "/repository/branches") && r.Method == http.MethodPost:
			gotBranchReq = true
			writeJSON(t, w, map[string]any{"name": proposalBranchName("k8s-runner", patched)})
		case strings.Contains(r.URL.Path, "/repository/files/") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("build-job:\n  script: [x]\n"))
		case strings.Contains(r.URL.Path, "/repository/files/") && r.Method == http.MethodPut:
			gotFileReq = true
			writeJSON(t, w, map[string]any{"file_path": ".gitlab-ci.yml", "branch": proposalBranchName("k8s-runner", patched)})
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.Method == http.MethodGet:
			writeJSON(t, w, []map[string]any{})
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.Method == http.MethodPost:
			gotMRReq = true
			writeJSON(t, w, map[string]any{"iid": 42, "web_url": "https://gitlab.example.com/group/proj/-/merge_requests/42"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	url, err := a.ProposePipelineTagChange(context.Background(), "1", patched, "k8s-runner")
	if err != nil {
		t.Fatalf("ProposePipelineTagChange: %v", err)
	}
	if !gotBranchReq || !gotFileReq || !gotMRReq {
		t.Fatalf("expected branch+file+MR requests, got branch=%v file=%v mr=%v", gotBranchReq, gotFileReq, gotMRReq)
	}
	if url != "https://gitlab.example.com/group/proj/-/merge_requests/42" {
		t.Errorf("unexpected MR url: %s", url)
	}
}

func TestProposePipelineTagChange_RetryReusesExistingBranchAndMergeRequest(t *testing.T) {
	patched := []byte("default:\n  tags: [k8s-runner]\n")
	putOrPostCalled := false
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/projects/1"):
			writeJSON(t, w, map[string]any{"id": 1, "default_branch": "main"})
		case strings.Contains(r.URL.Path, "/repository/branches/"):
			writeJSON(t, w, map[string]any{"name": proposalBranchName("k8s-runner", patched)})
		case strings.Contains(r.URL.Path, "/repository/files/") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write(patched)
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.Method == http.MethodGet:
			writeJSON(t, w, []map[string]any{{"iid": 42, "web_url": "https://gitlab.example.com/group/proj/-/merge_requests/42"}})
		default:
			putOrPostCalled = true
			writeJSON(t, w, map[string]any{})
		}
	})

	url, err := a.ProposePipelineTagChange(context.Background(), "1", patched, "k8s-runner")
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if url != "https://gitlab.example.com/group/proj/-/merge_requests/42" {
		t.Fatalf("unexpected existing MR URL %q", url)
	}
	if putOrPostCalled {
		t.Fatal("retry must not update content or create a duplicate merge request")
	}
}

func TestSlugifyTag(t *testing.T) {
	tests := map[string]string{
		"k8s-runner":  "k8s-runner",
		"K8s Runner!": "k8s-runner",
		"  spaced  ":  "spaced",
		"":            "tag",
		"___":         "tag",
	}
	for in, want := range tests {
		if got := slugifyTag(in); got != want {
			t.Errorf("slugifyTag(%q) = %q, want %q", in, got, want)
		}
	}
}
