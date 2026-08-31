package app

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/domehahn/housekeeping/internal/domain"
	projectpolicy "github.com/domehahn/housekeeping/internal/policy/project"
	"github.com/domehahn/housekeeping/internal/provider"
)

// fakeExecutor is an in-memory stand-in for the GitLab adapter, used to
// test executor orchestration logic (dry run, revalidation, idempotency,
// partial failure) without any network access.
type fakeExecutor struct {
	projects map[string]domain.Project
	users    map[string]domain.User

	deletedProjects  map[string]bool
	archivedProjects map[string]bool
	removedMembers   map[string]bool
	blockedUsers     map[string]bool

	failDeleteProject string // if set, DeleteProject for this ID returns a generic error
	currentUserID     string
	currentUserErr    error

	ciFiles           map[string][]byte   // projectID -> content; absent key means no CI file
	proposedMRs       map[string][]string // projectID -> tags, recording every ProposePipelineTagChange call
	proposeMRURL      string
	proposeMRErr      error
	runnerTags        map[string][]string // runnerID -> current tags
	updatedRunnerTags map[string][]string
	runners           map[string]domain.Runner

	renamedMRs         map[string][]domain.TagRename // projectID -> renames, recording every ProposePipelineTagRename call
	renameMRURL        string
	renameMRErr        error
	closedProposalURLs []string

	closeOnlyURLs  []string
	closeOnlyErr   error
	closeOnlyCalls map[string][]string // projectID -> oldTags, recording every ClosePipelineTagProposals call

	autoMergeResult   bool // MergeIfNoApprovalRequired's "merged" return
	autoMergeApproval bool // MergeIfNoApprovalRequired's "requiresApproval" return
	autoMergeErr      error
	autoMergeCalls    []string // merge request URLs passed to MergeIfNoApprovalRequired
}

func newFakeExecutor() *fakeExecutor {
	return &fakeExecutor{
		projects:          map[string]domain.Project{},
		users:             map[string]domain.User{},
		deletedProjects:   map[string]bool{},
		archivedProjects:  map[string]bool{},
		removedMembers:    map[string]bool{},
		blockedUsers:      map[string]bool{},
		currentUserID:     "current",
		ciFiles:           map[string][]byte{},
		proposedMRs:       map[string][]string{},
		runnerTags:        map[string][]string{},
		updatedRunnerTags: map[string][]string{},
		runners:           map[string]domain.Runner{},
		renamedMRs:        map[string][]domain.TagRename{},
		closeOnlyCalls:    map[string][]string{},
	}
}

func (f *fakeExecutor) GetPipelineConfig(_ context.Context, projectID string) ([]byte, bool, error) {
	content, ok := f.ciFiles[projectID]
	return content, ok, nil
}

func (f *fakeExecutor) ProposePipelineTagChange(_ context.Context, projectID string, _ []byte, tags []string) (string, error) {
	if f.proposeMRErr != nil {
		return "", f.proposeMRErr
	}
	f.proposedMRs[projectID] = append([]string{}, tags...)
	if f.proposeMRURL != "" {
		return f.proposeMRURL, nil
	}
	return "https://gitlab.example.com/mr/1", nil
}

func (f *fakeExecutor) ProposePipelineTagRename(_ context.Context, projectID string, _ []byte, renames []domain.TagRename) (string, []string, error) {
	if f.renameMRErr != nil {
		return "", nil, f.renameMRErr
	}
	f.renamedMRs[projectID] = append([]domain.TagRename{}, renames...)
	url := f.renameMRURL
	if url == "" {
		url = "https://gitlab.example.com/mr/2"
	}
	return url, f.closedProposalURLs, nil
}

func (f *fakeExecutor) ClosePipelineTagProposals(_ context.Context, projectID string, oldTags []string) ([]string, error) {
	if f.closeOnlyErr != nil {
		return nil, f.closeOnlyErr
	}
	f.closeOnlyCalls[projectID] = append([]string{}, oldTags...)
	return f.closeOnlyURLs, nil
}

func (f *fakeExecutor) MergeIfNoApprovalRequired(_ context.Context, _ string, mergeRequestURL string) (bool, bool, error) {
	f.autoMergeCalls = append(f.autoMergeCalls, mergeRequestURL)
	if f.autoMergeErr != nil {
		return false, false, f.autoMergeErr
	}
	return f.autoMergeResult, f.autoMergeApproval, nil
}

func (f *fakeExecutor) GetRunnerTags(_ context.Context, runnerID string) ([]string, error) {
	return f.runnerTags[runnerID], nil
}

func (f *fakeExecutor) UpdateRunnerTags(_ context.Context, runnerID string, _ []string, tags []string) error {
	f.updatedRunnerTags[runnerID] = tags
	f.runnerTags[runnerID] = tags
	return nil
}

func (f *fakeExecutor) ResolveGroupScope(_ context.Context, path string, recursive bool) (domain.Scope, []domain.Group, error) {
	return domain.Scope{Type: domain.ScopeTypeGroup, ID: "10", Path: path, Recursive: recursive, GroupIDs: []string{"10"}}, nil, nil
}

func (f *fakeExecutor) ListProjects(context.Context, domain.Scope) ([]domain.Project, error) {
	projects := make([]domain.Project, 0, len(f.projects))
	for _, p := range f.projects {
		projects = append(projects, p)
	}
	return projects, nil
}

func (f *fakeExecutor) ListRunnersForProjects(context.Context, domain.Scope, []string) ([]domain.Runner, error) {
	runners := make([]domain.Runner, 0, len(f.runners))
	for _, runner := range f.runners {
		runners = append(runners, runner)
	}
	return runners, nil
}

func (f *fakeExecutor) GetRunnerForProjects(_ context.Context, runnerID string, _ domain.Scope, _ []string) (domain.Runner, error) {
	runner, ok := f.runners[runnerID]
	if !ok {
		return domain.Runner{}, provider.NewError(provider.KindNotFound, "get runner", "not found", nil)
	}
	return runner, nil
}

func (f *fakeExecutor) GetProject(_ context.Context, id string) (domain.Project, error) {
	p, ok := f.projects[id]
	if !ok {
		return domain.Project{}, provider.NewError(provider.KindNotFound, "get project", "not found", nil)
	}
	return p, nil
}

func (f *fakeExecutor) DeleteProject(_ context.Context, id string) error {
	if id == f.failDeleteProject {
		return provider.NewError(provider.KindTemporary, "delete project", "boom", nil)
	}
	if _, ok := f.projects[id]; !ok {
		return provider.NewError(provider.KindNotFound, "delete project", "not found", nil)
	}
	f.deletedProjects[id] = true
	return nil
}

func (f *fakeExecutor) ArchiveProject(_ context.Context, id string) error {
	f.archivedProjects[id] = true
	return nil
}

func (f *fakeExecutor) GetUser(_ context.Context, id string) (domain.User, error) {
	u, ok := f.users[id]
	if !ok {
		return domain.User{}, provider.NewError(provider.KindNotFound, "get user", "not found", nil)
	}
	return u, nil
}

func (f *fakeExecutor) CurrentUser(_ context.Context) (domain.User, error) {
	if f.currentUserErr != nil {
		return domain.User{}, f.currentUserErr
	}
	return domain.User{ID: f.currentUserID, Username: "cleanup-bot"}, nil
}

func TestExecute_RevalidationSkipsCurrentUser(t *testing.T) {
	f := newFakeExecutor()
	f.currentUserID = "1"
	f.users["1"] = domain.User{ID: "1", Username: "cleanup-bot"}
	p := domain.Plan{Actions: []domain.PlannedAction{{
		ResourceType: domain.ResourceTypeUser, ResourceID: "1", ResourceName: "cleanup-bot",
		Action: domain.ActionBlockUser, EvaluatedAt: time.Now(),
	}}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: false})
	if summary.Outcomes[0].Result != domain.ResultSkippedRevalidate || f.blockedUsers["1"] {
		t.Fatalf("current user must be protected, got %+v", summary.Outcomes[0])
	}
}

func TestExecute_RevalidationSkipsChangedMembershipRole(t *testing.T) {
	f := newFakeExecutor()
	f.users["1"] = domain.User{ID: "1", Username: "alice", AccessLevel: domain.AccessLevelOwner}
	p := domain.Plan{Actions: []domain.PlannedAction{{
		ResourceType: domain.ResourceTypeUser, ResourceID: "1", ResourceName: "alice", GroupID: "10",
		AccessLevel: domain.AccessLevelDeveloper, Action: domain.ActionRemoveGroupMember, EvaluatedAt: time.Now(),
	}}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})
	if summary.Outcomes[0].Result != domain.ResultSkippedRevalidate || f.removedMembers["10/1"] {
		t.Fatalf("changed membership role must skip removal, got %+v", summary.Outcomes[0])
	}
}

func (f *fakeExecutor) GetGroupMember(_ context.Context, groupID, userID string) (domain.User, error) {
	u, ok := f.users[userID]
	if !ok {
		return domain.User{}, provider.NewError(provider.KindNotFound, "get member", "not found", nil)
	}
	u.GroupID = groupID
	u.MembershipOrigin = domain.MembershipDirect
	return u, nil
}

func (f *fakeExecutor) RemoveGroupMember(_ context.Context, groupID, userID string) error {
	if _, ok := f.users[userID]; !ok {
		return provider.NewError(provider.KindNotFound, "remove member", "not found", nil)
	}
	f.removedMembers[groupID+"/"+userID] = true
	return nil
}

func (f *fakeExecutor) BlockUser(_ context.Context, id string) error {
	f.blockedUsers[id] = true
	return nil
}

var _ Executor = (*fakeExecutor)(nil)

func TestExecute_DryRunNeverMutates(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1", LastActivityAt: domain.Known(nil)}

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeProject, ResourceID: "1", Action: domain.ActionDeleteProject, EvaluatedAt: time.Now()},
	}}

	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: false, Revalidate: true})
	if f.deletedProjects["1"] {
		t.Error("dry run must never delete a project")
	}
	if summary.Outcomes[0].Result != domain.ResultDryRun {
		t.Errorf("expected ResultDryRun, got %v", summary.Outcomes[0].Result)
	}
}

func TestExecute_ApplyDeletesProject(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1", LastActivityAt: domain.Known(nil)}

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeProject, ResourceID: "1", Action: domain.ActionDeleteProject, EvaluatedAt: time.Now()},
	}}

	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})
	if !f.deletedProjects["1"] {
		t.Error("expected project to be deleted")
	}
	if summary.Outcomes[0].Result != domain.ResultSuccess {
		t.Errorf("expected ResultSuccess, got %v: %s", summary.Outcomes[0].Result, summary.Outcomes[0].Detail)
	}
}

func TestExecute_RevalidationSkipsProjectThatBecameActive(t *testing.T) {
	f := newFakeExecutor()
	plannedAt := time.Now().Add(-time.Hour)
	recentActivity := time.Now()
	f.projects["1"] = domain.Project{ID: "1", LastActivityAt: domain.Known(&recentActivity)}

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeProject, ResourceID: "1", Action: domain.ActionDeleteProject, EvaluatedAt: plannedAt},
	}}

	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})
	if f.deletedProjects["1"] {
		t.Error("must not delete a project that has had activity since the plan was created")
	}
	if summary.Outcomes[0].Result != domain.ResultSkippedRevalidate {
		t.Errorf("expected ResultSkippedRevalidate, got %v", summary.Outcomes[0].Result)
	}
}

func TestExecute_AlreadyDeletedProjectIsIdempotent(t *testing.T) {
	f := newFakeExecutor() // project "1" does not exist -> 404 on both revalidate and delete

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeProject, ResourceID: "1", Action: domain.ActionDeleteProject, EvaluatedAt: time.Now()},
	}}

	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})
	if summary.Outcomes[0].Result != domain.ResultSkippedAlreadyDone {
		t.Errorf("expected ResultSkippedAlreadyDone, got %v: %s", summary.Outcomes[0].Result, summary.Outcomes[0].Detail)
	}
}

func TestExecute_PartialFailureDoesNotStopOtherActions(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1", LastActivityAt: domain.Known(nil)}
	f.projects["2"] = domain.Project{ID: "2", LastActivityAt: domain.Known(nil)}
	f.failDeleteProject = "1"

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeProject, ResourceID: "1", Action: domain.ActionDeleteProject, EvaluatedAt: time.Now()},
		{ResourceType: domain.ResourceTypeProject, ResourceID: "2", Action: domain.ActionDeleteProject, EvaluatedAt: time.Now()},
	}}

	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true, FailFast: false})
	if summary.CountByResult(domain.ResultFailed) != 1 {
		t.Errorf("expected 1 failure, got %d", summary.CountByResult(domain.ResultFailed))
	}
	if !f.deletedProjects["2"] {
		t.Error("expected project 2 to still be processed after project 1 failed (fail_fast=false)")
	}
	if !summary.Partial() {
		t.Error("expected Partial() to report true")
	}
}

func TestExecute_FailFastStopsAfterFirstFailure(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1", LastActivityAt: domain.Known(nil)}
	f.projects["2"] = domain.Project{ID: "2", LastActivityAt: domain.Known(nil)}
	f.failDeleteProject = "1"

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeProject, ResourceID: "1", Action: domain.ActionDeleteProject, EvaluatedAt: time.Now()},
		{ResourceType: domain.ResourceTypeProject, ResourceID: "2", Action: domain.ActionDeleteProject, EvaluatedAt: time.Now()},
	}}

	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true, FailFast: true})
	if len(summary.Outcomes) != 1 {
		t.Errorf("expected execution to stop after the first failure, got %d outcomes", len(summary.Outcomes))
	}
}

func TestExecute_UserRemoveFromGroupRequiresGroupID(t *testing.T) {
	f := newFakeExecutor()
	f.users["1"] = domain.User{ID: "1"}

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeUser, ResourceID: "1", Action: domain.ActionRemoveGroupMember, EvaluatedAt: time.Now()}, // no GroupID
	}}

	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})
	if summary.Outcomes[0].Result != domain.ResultFailed {
		t.Errorf("expected failure for a remove-from-group action with no group ID, got %v", summary.Outcomes[0].Result)
	}
}

func TestExecute_AddPipelineTag_OpensMergeRequest(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1"}
	f.ciFiles["1"] = []byte("build-job:\n  script: [\"echo hi\"]\n")
	f.proposeMRURL = "https://gitlab.example.com/group/proj/-/merge_requests/42"

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", Action: domain.ActionAddPipelineTag, TagValues: []string{"AKS", "production"}, EvaluatedAt: time.Now()},
	}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})

	if !slices.Equal(f.proposedMRs["1"], []string{"AKS", "production"}) {
		t.Errorf("expected one Merge Request with both tags, got %+v", f.proposedMRs)
	}
	if summary.Outcomes[0].Result != domain.ResultSuccess {
		t.Fatalf("expected ResultSuccess, got %v: %s", summary.Outcomes[0].Result, summary.Outcomes[0].Detail)
	}
	if summary.Outcomes[0].Detail == "" {
		t.Error("expected the outcome detail to contain the Merge Request URL")
	}
}

func TestExecute_AddPipelineTag_AlreadyPresentIsIdempotent(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1"}
	f.ciFiles["1"] = []byte("default:\n  tags:\n    - k8s-runner\n\nbuild-job:\n  script: [\"echo hi\"]\n")

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", Action: domain.ActionAddPipelineTag, TagValue: "k8s-runner", EvaluatedAt: time.Now()},
	}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})

	if len(f.proposedMRs) != 0 {
		t.Errorf("expected no Merge Request when the tag is already present, got %+v", f.proposedMRs)
	}
	if summary.Outcomes[0].Result != domain.ResultSkippedAlreadyDone {
		t.Errorf("expected ResultSkippedAlreadyDone, got %v: %s", summary.Outcomes[0].Result, summary.Outcomes[0].Detail)
	}
}

func TestExecute_AddPipelineTag_MissingCIFileIsIdempotent(t *testing.T) {
	f := newFakeExecutor() // no ciFiles entry for project "1"
	f.projects["1"] = domain.Project{ID: "1"}

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", Action: domain.ActionAddPipelineTag, TagValue: "k8s-runner", EvaluatedAt: time.Now()},
	}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})

	if summary.Outcomes[0].Result != domain.ResultSkippedAlreadyDone {
		t.Errorf("expected ResultSkippedAlreadyDone for a project whose .gitlab-ci.yml disappeared, got %v", summary.Outcomes[0].Result)
	}
}

func TestExecute_AddPipelineTag_DryRunNeverOpensMergeRequest(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1"}
	f.ciFiles["1"] = []byte("build-job:\n  script: [\"echo hi\"]\n")

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", Action: domain.ActionAddPipelineTag, TagValue: "k8s-runner", EvaluatedAt: time.Now()},
	}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: false, Revalidate: true})

	if len(f.proposedMRs) != 0 {
		t.Error("dry run must never open a Merge Request")
	}
	if summary.Outcomes[0].Result != domain.ResultDryRun {
		t.Errorf("expected ResultDryRun, got %v", summary.Outcomes[0].Result)
	}
}

func TestExecute_AddPipelineTag_RechecksProjectProtection(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1", FullPath: "group/protected"}
	f.ciFiles["1"] = []byte("build-job:\n  script: [\"echo hi\"]\n")
	protection, err := projectpolicy.NewProtection([]string{"group/protected"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := domain.Plan{Actions: []domain.PlannedAction{{
		ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", ResourceName: "group/protected",
		Action: domain.ActionAddPipelineTag, TagValue: "k8s-runner", EvaluatedAt: time.Now(),
	}}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true, ProjectProtection: protection})
	if summary.Outcomes[0].Result != domain.ResultSkippedRevalidate || len(f.proposedMRs) != 0 {
		t.Fatalf("live protection must block the proposal, got %+v", summary.Outcomes[0])
	}
}

func TestExecute_AddRunnerTag_UpdatesTagList(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1", FullPath: "group/project"}
	f.runners["5"] = domain.Runner{ID: "5", ImpactKnown: true, InScopeProjectPaths: []string{"group/project"}}
	f.runnerTags["5"] = []string{"existing-tag"}

	p := domain.Plan{Scope: domain.PlanScope{Path: "group", Recursive: true}, Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeRunner, ResourceID: "5", Action: domain.ActionAddRunnerTag, TagValue: "k8s-runner", EvaluatedAt: time.Now()},
	}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})

	got := f.updatedRunnerTags["5"]
	if len(got) != 2 || got[0] != "existing-tag" || got[1] != "k8s-runner" {
		t.Errorf("expected tags to become [existing-tag, k8s-runner], got %v", got)
	}
	if summary.Outcomes[0].Result != domain.ResultSuccess {
		t.Errorf("expected ResultSuccess, got %v: %s", summary.Outcomes[0].Result, summary.Outcomes[0].Detail)
	}
}

func TestExecute_AddRunnerTag_AlreadyPresentIsIdempotent(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1", FullPath: "group/project"}
	f.runners["5"] = domain.Runner{ID: "5", ImpactKnown: true, InScopeProjectPaths: []string{"group/project"}}
	f.runnerTags["5"] = []string{"k8s-runner"}

	p := domain.Plan{Scope: domain.PlanScope{Path: "group", Recursive: true}, Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeRunner, ResourceID: "5", Action: domain.ActionAddRunnerTag, TagValue: "k8s-runner", EvaluatedAt: time.Now()},
	}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})

	if len(f.updatedRunnerTags) != 0 {
		t.Errorf("expected no update when the tag is already present, got %+v", f.updatedRunnerTags)
	}
	if summary.Outcomes[0].Result != domain.ResultSkippedAlreadyDone {
		t.Errorf("expected ResultSkippedAlreadyDone, got %v: %s", summary.Outcomes[0].Result, summary.Outcomes[0].Detail)
	}
}

func TestExecute_AddPipelineTag_AutoMergesWhenNoApprovalRequired(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1"}
	f.ciFiles["1"] = []byte("build-job:\n  script: [\"echo hi\"]\n")
	f.proposeMRURL = "https://gitlab.example.com/group/proj/-/merge_requests/42"
	f.autoMergeResult = true

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", Action: domain.ActionAddPipelineTag, TagValue: "k8s-runner", EvaluatedAt: time.Now()},
	}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true, AutoMergeIfNoApprovalRequired: true})

	if !slices.Equal(f.autoMergeCalls, []string{f.proposeMRURL}) {
		t.Errorf("expected MergeIfNoApprovalRequired to be called with the new MR URL, got %v", f.autoMergeCalls)
	}
	if summary.Outcomes[0].Result != domain.ResultSuccess {
		t.Fatalf("expected ResultSuccess, got %v: %s", summary.Outcomes[0].Result, summary.Outcomes[0].Detail)
	}
	if !strings.Contains(summary.Outcomes[0].Detail, "merge requested") {
		t.Errorf("expected the detail to mention the merge request, got %q", summary.Outcomes[0].Detail)
	}
	if len(summary.NeedsApproval) != 0 {
		t.Errorf("expected no NeedsApproval entries, got %+v", summary.NeedsApproval)
	}
}

func TestExecute_AddPipelineTag_ReportsNeedsApprovalWithoutMerging(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1", FullPath: "group/project"}
	f.ciFiles["1"] = []byte("build-job:\n  script: [\"echo hi\"]\n")
	f.proposeMRURL = "https://gitlab.example.com/group/proj/-/merge_requests/42"
	f.autoMergeApproval = true

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", ResourceName: "group/project",
			Action: domain.ActionAddPipelineTag, TagValue: "k8s-runner", EvaluatedAt: time.Now()},
	}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true, AutoMergeIfNoApprovalRequired: true})

	if summary.Outcomes[0].Result != domain.ResultSuccess {
		t.Fatalf("expected ResultSuccess (the MR itself was opened successfully), got %v: %s", summary.Outcomes[0].Result, summary.Outcomes[0].Detail)
	}
	if !strings.Contains(summary.Outcomes[0].Detail, "requires approval") {
		t.Errorf("expected the detail to say approval is required, got %q", summary.Outcomes[0].Detail)
	}
	if len(summary.NeedsApproval) != 1 || summary.NeedsApproval[0].MergeRequestURL != f.proposeMRURL || summary.NeedsApproval[0].ResourceName != "group/project" {
		t.Fatalf("expected one NeedsApproval entry for the MR, got %+v", summary.NeedsApproval)
	}
}

func TestExecute_AddPipelineTag_AutoMergeOffByDefault(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1"}
	f.ciFiles["1"] = []byte("build-job:\n  script: [\"echo hi\"]\n")
	f.proposeMRURL = "https://gitlab.example.com/group/proj/-/merge_requests/42"

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", Action: domain.ActionAddPipelineTag, TagValue: "k8s-runner", EvaluatedAt: time.Now()},
	}}
	// AutoMergeIfNoApprovalRequired is left unset (false) - existing
	// behavior/plans must not change unless explicitly opted in.
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})

	if len(f.autoMergeCalls) != 0 {
		t.Errorf("expected MergeIfNoApprovalRequired to never be called without the opt-in flag, got %v", f.autoMergeCalls)
	}
	if summary.Outcomes[0].Detail != "merge request opened: "+f.proposeMRURL {
		t.Errorf("expected the plain, unmodified detail, got %q", summary.Outcomes[0].Detail)
	}
}

func TestExecute_ReplacePipelineTag_OpensMergeRequestAndReportsClosedProposals(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1"}
	f.ciFiles["1"] = []byte("default:\n  tags:\n    - AKS\n")
	f.renameMRURL = "https://gitlab.example.com/group/proj/-/merge_requests/43"
	f.closedProposalURLs = []string{"https://gitlab.example.com/group/proj/-/merge_requests/7"}

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", Action: domain.ActionReplacePipelineTag,
			TagRenames: []domain.TagRename{{Old: "AKS", New: "aks"}}, EvaluatedAt: time.Now()},
	}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})

	if !slices.Equal(f.renamedMRs["1"], []domain.TagRename{{Old: "AKS", New: "aks"}}) {
		t.Errorf("expected one rename Merge Request for AKS->aks, got %+v", f.renamedMRs)
	}
	if summary.Outcomes[0].Result != domain.ResultSuccess {
		t.Fatalf("expected ResultSuccess, got %v: %s", summary.Outcomes[0].Result, summary.Outcomes[0].Detail)
	}
	if !strings.Contains(summary.Outcomes[0].Detail, "merge_requests/43") || !strings.Contains(summary.Outcomes[0].Detail, "merge_requests/7") {
		t.Errorf("expected the outcome detail to name both the new MR and the closed one, got %q", summary.Outcomes[0].Detail)
	}
}

func TestExecute_ReplacePipelineTag_AlreadyFixedIsIdempotent(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1"}
	f.ciFiles["1"] = []byte("default:\n  tags:\n    - aks\n") // already correct

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", Action: domain.ActionReplacePipelineTag,
			TagRenames: []domain.TagRename{{Old: "AKS", New: "aks"}}, EvaluatedAt: time.Now()},
	}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})

	if len(f.renamedMRs) != 0 {
		t.Errorf("expected no Merge Request when the old tag is no longer present, got %+v", f.renamedMRs)
	}
	if summary.Outcomes[0].Result != domain.ResultSkippedAlreadyDone {
		t.Errorf("expected ResultSkippedAlreadyDone, got %v: %s", summary.Outcomes[0].Result, summary.Outcomes[0].Detail)
	}
}

func TestExecute_ReplacePipelineTag_AddsCorrectedTagWhenNeverMerged(t *testing.T) {
	// The original add-tag Merge Request for this project was never
	// merged: the file has neither "AKS" nor "aks" at all. The rename
	// must still add the corrected tag.
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1"}
	f.ciFiles["1"] = []byte("build-job:\n  script: [\"echo hi\"]\n")
	f.renameMRURL = "https://gitlab.example.com/group/proj/-/merge_requests/43"

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", Action: domain.ActionReplacePipelineTag,
			TagRenames: []domain.TagRename{{Old: "AKS", New: "aks"}}, EvaluatedAt: time.Now()},
	}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})

	if len(f.renamedMRs) != 1 {
		t.Fatalf("expected a Merge Request adding the corrected tag, got %+v", f.renamedMRs)
	}
	if summary.Outcomes[0].Result != domain.ResultSuccess {
		t.Fatalf("expected ResultSuccess, got %v: %s", summary.Outcomes[0].Result, summary.Outcomes[0].Detail)
	}
}

func TestExecute_ReplacePipelineTag_ClosesStaleProposalWithoutFileChange(t *testing.T) {
	// The file is already fully correct (or never needed merging), but a
	// stale, still-open Merge Request from an earlier, wrong-tag run
	// remains open - this is the exact case reported in production:
	// evaluate/plan flagged it, and execute must close it even though
	// there is no file diff to propose.
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1"}
	f.ciFiles["1"] = []byte("default:\n  tags:\n    - aks\n")
	f.closeOnlyURLs = []string{"https://gitlab.example.com/group/proj/-/merge_requests/7"}

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", Action: domain.ActionReplacePipelineTag,
			TagRenames: []domain.TagRename{{Old: "AKS", New: "aks"}}, EvaluatedAt: time.Now()},
	}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})

	if !slices.Equal(f.closeOnlyCalls["1"], []string{"AKS"}) {
		t.Errorf("expected ClosePipelineTagProposals to be called for the old tag, got %+v", f.closeOnlyCalls)
	}
	if len(f.renamedMRs) != 0 {
		t.Errorf("expected no new Merge Request when the file needs no change, got %+v", f.renamedMRs)
	}
	if summary.Outcomes[0].Result != domain.ResultSuccess {
		t.Fatalf("expected ResultSuccess, got %v: %s", summary.Outcomes[0].Result, summary.Outcomes[0].Detail)
	}
	if !strings.Contains(summary.Outcomes[0].Detail, "merge_requests/7") {
		t.Errorf("expected the outcome detail to name the closed proposal, got %q", summary.Outcomes[0].Detail)
	}
}

func TestExecute_ReplacePipelineTag_TrulyNothingToDoIsIdempotent(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1"}
	f.ciFiles["1"] = []byte("default:\n  tags:\n    - aks\n") // already correct, no stale proposal either

	p := domain.Plan{Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypePipelineConfig, ResourceID: "1", Action: domain.ActionReplacePipelineTag,
			TagRenames: []domain.TagRename{{Old: "AKS", New: "aks"}}, EvaluatedAt: time.Now()},
	}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})

	// A close attempt is made (harmlessly finding nothing to close), but
	// no Merge Request is opened and the outcome is a clean idempotent
	// no-op.
	if len(f.renamedMRs) != 0 {
		t.Errorf("expected no Merge Request, got %+v", f.renamedMRs)
	}
	if summary.Outcomes[0].Result != domain.ResultSkippedAlreadyDone {
		t.Errorf("expected ResultSkippedAlreadyDone, got %v: %s", summary.Outcomes[0].Result, summary.Outcomes[0].Detail)
	}
}

func TestExecute_ReplaceRunnerTag_SwapsTagList(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1", FullPath: "group/project"}
	f.runners["5"] = domain.Runner{ID: "5", ImpactKnown: true, InScopeProjectPaths: []string{"group/project"}}
	f.runnerTags["5"] = []string{"AKS", "other-tag"}

	p := domain.Plan{Scope: domain.PlanScope{Path: "group", Recursive: true}, Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeRunner, ResourceID: "5", Action: domain.ActionReplaceRunnerTag,
			TagRenames: []domain.TagRename{{Old: "AKS", New: "aks"}}, EvaluatedAt: time.Now()},
	}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})

	got := f.updatedRunnerTags["5"]
	if !slices.Contains(got, "aks") || slices.Contains(got, "AKS") || !slices.Contains(got, "other-tag") {
		t.Errorf("expected tags to become [other-tag, aks], got %v", got)
	}
	if summary.Outcomes[0].Result != domain.ResultSuccess {
		t.Errorf("expected ResultSuccess, got %v: %s", summary.Outcomes[0].Result, summary.Outcomes[0].Detail)
	}
}

func TestExecute_ReplaceRunnerTag_AlreadyFixedIsIdempotent(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1", FullPath: "group/project"}
	f.runners["5"] = domain.Runner{ID: "5", ImpactKnown: true, InScopeProjectPaths: []string{"group/project"}}
	f.runnerTags["5"] = []string{"aks"} // already correct

	p := domain.Plan{Scope: domain.PlanScope{Path: "group", Recursive: true}, Actions: []domain.PlannedAction{
		{ResourceType: domain.ResourceTypeRunner, ResourceID: "5", Action: domain.ActionReplaceRunnerTag,
			TagRenames: []domain.TagRename{{Old: "AKS", New: "aks"}}, EvaluatedAt: time.Now()},
	}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})

	if len(f.updatedRunnerTags) != 0 {
		t.Errorf("expected no update when the old tag is no longer present, got %+v", f.updatedRunnerTags)
	}
	if summary.Outcomes[0].Result != domain.ResultSkippedAlreadyDone {
		t.Errorf("expected ResultSkippedAlreadyDone, got %v: %s", summary.Outcomes[0].Result, summary.Outcomes[0].Detail)
	}
}

func TestExecute_ReplaceRunnerTag_RejectsChangedLiveImpact(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1", FullPath: "group/project"}
	f.runners["5"] = domain.Runner{
		ID: "5", ImpactKnown: true, InScopeProjectPaths: []string{"group/project"},
		OutOfScopeProjectPaths: []string{"other/new"},
	}
	f.runnerTags["5"] = []string{"AKS"}
	p := domain.Plan{Scope: domain.PlanScope{Path: "group", Recursive: true}, Actions: []domain.PlannedAction{{
		ResourceType: domain.ResourceTypeRunner, ResourceID: "5", Action: domain.ActionReplaceRunnerTag,
		TagRenames: []domain.TagRename{{Old: "AKS", New: "aks"}}, EvaluatedAt: time.Now(),
	}}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})
	if summary.Outcomes[0].Result != domain.ResultFailed || len(f.updatedRunnerTags) != 0 {
		t.Fatalf("changed runner reach must require a new plan, got %+v", summary.Outcomes[0])
	}
}

func TestExecute_AddRunnerTag_RejectsChangedLiveImpact(t *testing.T) {
	f := newFakeExecutor()
	f.projects["1"] = domain.Project{ID: "1", FullPath: "group/project"}
	f.runners["5"] = domain.Runner{
		ID: "5", ImpactKnown: true, InScopeProjectPaths: []string{"group/project"},
		OutOfScopeProjectPaths: []string{"other/new"},
	}
	f.runnerTags["5"] = []string{"existing"}
	p := domain.Plan{Scope: domain.PlanScope{Path: "group", Recursive: true}, Actions: []domain.PlannedAction{{
		ResourceType: domain.ResourceTypeRunner, ResourceID: "5", Action: domain.ActionAddRunnerTag,
		TagValue: "k8s-runner", EvaluatedAt: time.Now(),
	}}}
	summary := Execute(context.Background(), f, p, ExecuteOptions{Apply: true, Revalidate: true})
	if summary.Outcomes[0].Result != domain.ResultFailed || len(f.updatedRunnerTags) != 0 {
		t.Fatalf("changed runner reach must require a new plan, got %+v", summary.Outcomes[0])
	}
}
