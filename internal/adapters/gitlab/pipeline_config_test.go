package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/domehahn/housekeeping/internal/domain"
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
			writeJSON(t, w, map[string]any{"name": proposalBranchName([]string{"k8s-runner"}, patched)})
		case strings.Contains(r.URL.Path, "/repository/files/") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("build-job:\n  script: [x]\n"))
		case strings.Contains(r.URL.Path, "/repository/files/") && r.Method == http.MethodPut:
			gotFileReq = true
			writeJSON(t, w, map[string]any{"file_path": ".gitlab-ci.yml", "branch": proposalBranchName([]string{"k8s-runner"}, patched)})
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.Method == http.MethodGet:
			writeJSON(t, w, []map[string]any{})
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.Method == http.MethodPost:
			gotMRReq = true
			writeJSON(t, w, map[string]any{"iid": 42, "web_url": "https://gitlab.example.com/group/proj/-/merge_requests/42"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	url, err := a.ProposePipelineTagChange(context.Background(), "1", patched, []string{"k8s-runner"})
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
			writeJSON(t, w, map[string]any{"name": proposalBranchName([]string{"k8s-runner"}, patched)})
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

	url, err := a.ProposePipelineTagChange(context.Background(), "1", patched, []string{"k8s-runner"})
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

func TestGetMergedPipelineConfig(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/projects/1/ci/lint") {
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
		writeJSON(t, w, map[string]any{
			"valid":       true,
			"merged_yaml": "default:\n  tags: [AKS]\n",
			"includes":    []map[string]any{{"type": "local", "location": "jobs.yml", "context_project": "company/a", "context_sha": "abc"}},
		})
	})
	content, includes, err := a.GetMergedPipelineConfig(context.Background(), "1")
	if err != nil || !strings.Contains(string(content), "AKS") || len(includes) != 1 || includes[0].Location != "jobs.yml" {
		t.Fatalf("GetMergedPipelineConfig() = %q, %+v, %v", content, includes, err)
	}
}

func TestListPipelineTagProposalsFiltersBranchPrefix(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("order_by") != "updated_at" || r.URL.Query().Get("sort") != "desc" {
			t.Errorf("proposal ordering query = %q", r.URL.RawQuery)
		}
		writeJSON(t, w, []map[string]any{
			{"title": "collision", "description": proposalTagMarker([]string{"aks"}), "state": "opened", "source_branch": "scm-cleaner/add-tag-aks-newest", "web_url": "https://example/mr/collision"},
			{"title": "scm-cleaner", "description": proposalTagMarker([]string{"AKS"}), "state": "merged", "source_branch": "scm-cleaner/add-tag-aks-abcdef", "web_url": "https://example/mr/1"},
			{"title": "other", "state": "opened", "source_branch": "feature/other", "web_url": "https://example/mr/2"},
		})
	})
	proposals, err := a.ListPipelineTagProposals(context.Background(), "1", []string{"AKS"})
	if err != nil || len(proposals) != 1 || proposals[0].State != "merged" {
		t.Fatalf("ListPipelineTagProposals() = %+v, %v", proposals, err)
	}
}

func TestProposePipelineTagRename_HappyPathOpensNewMR(t *testing.T) {
	renames := []domain.TagRename{{Old: "AKS", New: "aks"}}
	patched := []byte("default:\n  tags:\n    - aks\n")
	var gotBranchReq, gotFileReq, gotMRReq bool
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/projects/1"):
			writeJSON(t, w, map[string]any{"id": 1, "default_branch": "main"})
		case strings.Contains(r.URL.Path, "/repository/branches/") && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"404 Branch Not Found"}`))
		case strings.Contains(r.URL.Path, "/repository/branches") && r.Method == http.MethodPost:
			gotBranchReq = true
			writeJSON(t, w, map[string]any{"name": renameBranchName(renames, patched)})
		case strings.Contains(r.URL.Path, "/repository/files/") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("default:\n  tags:\n    - AKS\n"))
		case strings.Contains(r.URL.Path, "/repository/files/") && r.Method == http.MethodPut:
			gotFileReq = true
			writeJSON(t, w, map[string]any{"file_path": ".gitlab-ci.yml", "branch": renameBranchName(renames, patched)})
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.Method == http.MethodGet:
			writeJSON(t, w, []map[string]any{})
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.Method == http.MethodPost:
			gotMRReq = true
			writeJSON(t, w, map[string]any{"iid": 43, "web_url": "https://gitlab.example.com/group/proj/-/merge_requests/43"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	url, closed, err := a.ProposePipelineTagRename(context.Background(), "1", patched, renames)
	if err != nil {
		t.Fatalf("ProposePipelineTagRename: %v", err)
	}
	if !gotBranchReq || !gotFileReq || !gotMRReq {
		t.Fatalf("expected branch+file+MR requests, got branch=%v file=%v mr=%v", gotBranchReq, gotFileReq, gotMRReq)
	}
	if url != "https://gitlab.example.com/group/proj/-/merge_requests/43" {
		t.Errorf("unexpected MR url: %s", url)
	}
	if len(closed) != 0 {
		t.Errorf("expected no closed proposals when none exist, got %v", closed)
	}
}

func TestProposePipelineTagRename_ClosesSupersededOpenProposal(t *testing.T) {
	renames := []domain.TagRename{{Old: "AKS", New: "aks"}}
	patched := []byte("default:\n  tags:\n    - aks\n")
	var closedIID string
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/projects/1"):
			writeJSON(t, w, map[string]any{"id": 1, "default_branch": "main"})
		case strings.Contains(r.URL.Path, "/repository/branches/") && r.Method == http.MethodGet:
			writeJSON(t, w, map[string]any{"name": renameBranchName(renames, patched)})
		case strings.Contains(r.URL.Path, "/repository/files/") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write(patched)
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.URL.Query().Get("state") == "all":
			// ListPipelineTagProposals lookup for the old tag "AKS".
			writeJSON(t, w, []map[string]any{
				{"iid": 7, "title": "scm-cleaner: add CI tags AKS", "description": proposalTagMarker([]string{"AKS"}), "state": "opened", "source_branch": "scm-cleaner/add-tag-aks-old", "web_url": "https://example/mr/7"},
			})
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.Method == http.MethodGet:
			// findOpenProposal for the new rename branch: none yet.
			writeJSON(t, w, []map[string]any{})
		case strings.Contains(r.URL.Path, "/merge_requests/7") && r.Method == http.MethodPut:
			closedIID = "7"
			writeJSON(t, w, map[string]any{"iid": 7, "state": "closed"})
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.Method == http.MethodPost:
			writeJSON(t, w, map[string]any{"iid": 43, "web_url": "https://gitlab.example.com/group/proj/-/merge_requests/43"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	_, closed, err := a.ProposePipelineTagRename(context.Background(), "1", patched, renames)
	if err != nil {
		t.Fatalf("ProposePipelineTagRename: %v", err)
	}
	if closedIID != "7" {
		t.Error("expected the old, open scm-cleaner proposal (iid 7) to be closed")
	}
	if len(closed) != 1 || closed[0] != "https://example/mr/7" {
		t.Errorf("expected closedProposalURLs to report the closed MR, got %v", closed)
	}
}

func TestProposePipelineTagRename_LeavesMergedProposalAlone(t *testing.T) {
	renames := []domain.TagRename{{Old: "AKS", New: "aks"}}
	patched := []byte("default:\n  tags:\n    - aks\n")
	putCalled := false
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/projects/1"):
			writeJSON(t, w, map[string]any{"id": 1, "default_branch": "main"})
		case strings.Contains(r.URL.Path, "/repository/branches/") && r.Method == http.MethodGet:
			writeJSON(t, w, map[string]any{"name": renameBranchName(renames, patched)})
		case strings.Contains(r.URL.Path, "/repository/files/") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write(patched)
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.URL.Query().Get("state") == "all":
			writeJSON(t, w, []map[string]any{
				{"iid": 7, "title": "scm-cleaner: add CI tags AKS", "description": proposalTagMarker([]string{"AKS"}), "state": "merged", "source_branch": "scm-cleaner/add-tag-aks-old", "web_url": "https://example/mr/7"},
			})
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.Method == http.MethodGet:
			writeJSON(t, w, []map[string]any{})
		case strings.Contains(r.URL.Path, "/merge_requests/7") && r.Method == http.MethodPut:
			putCalled = true
			writeJSON(t, w, map[string]any{"iid": 7, "state": "closed"})
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.Method == http.MethodPost:
			writeJSON(t, w, map[string]any{"iid": 43, "web_url": "https://gitlab.example.com/group/proj/-/merge_requests/43"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	_, closed, err := a.ProposePipelineTagRename(context.Background(), "1", patched, renames)
	if err != nil {
		t.Fatalf("ProposePipelineTagRename: %v", err)
	}
	if putCalled {
		t.Error("expected an already-merged proposal to never be closed")
	}
	if len(closed) != 0 {
		t.Errorf("expected no closed proposals, got %v", closed)
	}
}

func TestProposePipelineTagRename_CloseFailureDoesNotFailRename(t *testing.T) {
	renames := []domain.TagRename{{Old: "AKS", New: "aks"}}
	patched := []byte("default:\n  tags:\n    - aks\n")
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/projects/1"):
			writeJSON(t, w, map[string]any{"id": 1, "default_branch": "main"})
		case strings.Contains(r.URL.Path, "/repository/branches/") && r.Method == http.MethodGet:
			writeJSON(t, w, map[string]any{"name": renameBranchName(renames, patched)})
		case strings.Contains(r.URL.Path, "/repository/files/") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write(patched)
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.URL.Query().Get("state") == "all":
			writeJSON(t, w, []map[string]any{
				{"iid": 7, "title": "scm-cleaner: add CI tags AKS", "description": proposalTagMarker([]string{"AKS"}), "state": "opened", "source_branch": "scm-cleaner/add-tag-aks-old", "web_url": "https://example/mr/7"},
			})
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.Method == http.MethodGet:
			writeJSON(t, w, []map[string]any{})
		case strings.Contains(r.URL.Path, "/merge_requests/7") && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.Method == http.MethodPost:
			writeJSON(t, w, map[string]any{"iid": 43, "web_url": "https://gitlab.example.com/group/proj/-/merge_requests/43"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	url, closed, err := a.ProposePipelineTagRename(context.Background(), "1", patched, renames)
	if err != nil {
		t.Fatalf("expected a close failure to never fail the rename itself, got: %v", err)
	}
	if url != "https://gitlab.example.com/group/proj/-/merge_requests/43" {
		t.Errorf("expected the new MR to still be opened, got url=%q", url)
	}
	if len(closed) != 0 {
		t.Errorf("expected no closed proposals reported when closing failed, got %v", closed)
	}
}

func TestClosePipelineTagProposals_ClosesOnlyMatchingOpenProposal(t *testing.T) {
	var closedIID string
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.URL.Query().Get("state") == "all":
			writeJSON(t, w, []map[string]any{
				{"iid": 7, "title": "scm-cleaner: add CI tags AKS", "description": proposalTagMarker([]string{"AKS"}), "state": "opened", "source_branch": "scm-cleaner/add-tag-aks-old", "web_url": "https://example/mr/7"},
				{"iid": 9, "title": "scm-cleaner: add CI tags EKS", "description": proposalTagMarker([]string{"EKS"}), "state": "opened", "source_branch": "scm-cleaner/add-tag-eks-old", "web_url": "https://example/mr/9"},
			})
		case strings.Contains(r.URL.Path, "/merge_requests/7") && r.Method == http.MethodPut:
			closedIID = "7"
			writeJSON(t, w, map[string]any{"iid": 7, "state": "closed"})
		case strings.Contains(r.URL.Path, "/merge_requests/9") && r.Method == http.MethodPut:
			t.Fatal("expected the unrelated EKS proposal to never be closed")
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	closed, err := a.ClosePipelineTagProposals(context.Background(), "1", []string{"AKS"})
	if err != nil {
		t.Fatalf("ClosePipelineTagProposals: %v", err)
	}
	if closedIID != "7" {
		t.Error("expected the AKS proposal (iid 7) to be closed")
	}
	if len(closed) != 1 || closed[0] != "https://example/mr/7" {
		t.Errorf("unexpected closed URLs: %v", closed)
	}
}

func TestClosePipelineTagProposals_NoMatchReturnsEmpty(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []map[string]any{})
	})
	closed, err := a.ClosePipelineTagProposals(context.Background(), "1", []string{"AKS"})
	if err != nil || len(closed) != 0 {
		t.Fatalf("ClosePipelineTagProposals() = %v, %v; want empty, nil", closed, err)
	}
}

func TestMergeIfNoApprovalRequired_MergesWhenZeroApprovalsRequired(t *testing.T) {
	var gotAutoMerge bool
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/approvals") && r.Method == http.MethodGet:
			writeJSON(t, w, map[string]any{"approvals_required": 0})
		case strings.Contains(r.URL.Path, "/merge_requests/42/merge") && r.Method == http.MethodPut:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if autoMerge, _ := body["auto_merge"].(bool); !autoMerge {
				t.Errorf("expected auto_merge=true in request body, got %v", body)
			}
			gotAutoMerge = true
			writeJSON(t, w, map[string]any{"iid": 42, "state": "merged"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	merged, requiresApproval, err := a.MergeIfNoApprovalRequired(context.Background(), "1", "https://gitlab.example.com/group/proj/-/merge_requests/42")
	if err != nil {
		t.Fatalf("MergeIfNoApprovalRequired: %v", err)
	}
	if !merged || requiresApproval {
		t.Errorf("expected merged=true, requiresApproval=false, got merged=%v requiresApproval=%v", merged, requiresApproval)
	}
	if !gotAutoMerge {
		t.Error("expected the merge endpoint to be called")
	}
}

func TestMergeIfNoApprovalRequired_LeavesOpenWhenApprovalRequired(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/approvals") && r.Method == http.MethodGet:
			writeJSON(t, w, map[string]any{"approvals_required": 1})
		default:
			t.Fatalf("unexpected request: %s %s (approval is required, must never attempt to merge)", r.Method, r.URL.Path)
		}
	})

	merged, requiresApproval, err := a.MergeIfNoApprovalRequired(context.Background(), "1", "https://gitlab.example.com/group/proj/-/merge_requests/42")
	if err != nil {
		t.Fatalf("MergeIfNoApprovalRequired: %v", err)
	}
	if merged || !requiresApproval {
		t.Errorf("expected merged=false, requiresApproval=true, got merged=%v requiresApproval=%v", merged, requiresApproval)
	}
}

func TestMergeRequestIIDFromURL(t *testing.T) {
	tests := map[string]struct {
		want    int64
		wantErr bool
	}{
		"https://gitlab.example.com/group/proj/-/merge_requests/42":    {want: 42},
		"https://gitlab.example.com/group/proj/-/merge_requests/7?x=1": {want: 7},
		"https://gitlab.example.com/group/proj/-/merge_requests":       {wantErr: true},
		"not a url at all": {wantErr: true},
	}
	for url, tc := range tests {
		got, err := mergeRequestIIDFromURL(url)
		if tc.wantErr {
			if err == nil {
				t.Errorf("mergeRequestIIDFromURL(%q): expected an error", url)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("mergeRequestIIDFromURL(%q) = %d, %v; want %d, nil", url, got, err, tc.want)
		}
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
