package app

import (
	"context"
	"testing"
	"time"

	"github.com/domehahn/housekeeping/internal/domain"
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
}

func newFakeExecutor() *fakeExecutor {
	return &fakeExecutor{
		projects:         map[string]domain.Project{},
		users:            map[string]domain.User{},
		deletedProjects:  map[string]bool{},
		archivedProjects: map[string]bool{},
		removedMembers:   map[string]bool{},
		blockedUsers:     map[string]bool{},
	}
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
