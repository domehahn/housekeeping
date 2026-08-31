package cli

import (
	"bytes"
	"context"

	"github.com/spf13/cobra"

	"github.com/domehahn/housekeeping/internal/config"
	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/output"
	"github.com/domehahn/housekeeping/internal/provider"
	"github.com/domehahn/housekeeping/internal/secrets"
)

// fakeClient is an in-memory stand-in for the GitLab adapter, implementing
// the full provider.Client port set, used to test CLI command wiring
// (flag parsing, error paths, output rendering, exit codes) without any
// network access. Every leaf command already gets its provider.Client via
// env.requireClient(), which returns env.client directly when it is
// already set - so presetting it in a test env bypasses provider
// construction/config validation entirely.
type fakeClient struct {
	scope    domain.Scope
	groups   []domain.Group
	scopeErr error

	projects    []domain.Project
	projectsErr error

	members    []domain.User
	membersErr error

	info         provider.Info
	infoErr      error
	capabilities provider.Capabilities

	currentUser    domain.User
	currentUserErr error

	billable    map[string]bool
	billableErr error

	memberships map[string][]domain.Membership

	ciFiles      map[string][]byte
	proposeURL   string
	proposeErr   error
	proposedTags map[string][]string
	mergedCI     map[string][]byte
	ciIncludes   map[string][]domain.PipelineInclude
	proposals    map[string][]domain.PipelineProposal

	renameURL          string
	renameErr          error
	closedProposalURLs []string
	renamedTags        map[string][]domain.TagRename
	closeOnlyURLs      []string
	closeOnlyErr       error
	closeOnlyCalls     map[string][]string

	runners    []domain.Runner
	runnersErr error
	runnerTags map[string][]string

	deletedProjects   map[string]bool
	removedMembers    map[string]bool
	failDeleteProject string // if set, DeleteProject for this ID returns a generic (non-404) error
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		billable:        map[string]bool{},
		memberships:     map[string][]domain.Membership{},
		ciFiles:         map[string][]byte{},
		proposedTags:    map[string][]string{},
		mergedCI:        map[string][]byte{},
		ciIncludes:      map[string][]domain.PipelineInclude{},
		proposals:       map[string][]domain.PipelineProposal{},
		renamedTags:     map[string][]domain.TagRename{},
		closeOnlyCalls:  map[string][]string{},
		runnerTags:      map[string][]string{},
		deletedProjects: map[string]bool{},
		removedMembers:  map[string]bool{},
	}
}

func (f *fakeClient) ResolveGroupScope(context.Context, string, bool) (domain.Scope, []domain.Group, error) {
	return f.scope, f.groups, f.scopeErr
}
func (f *fakeClient) ListProjects(context.Context, domain.Scope) ([]domain.Project, error) {
	return f.projects, f.projectsErr
}
func (f *fakeClient) GetProject(_ context.Context, id string) (domain.Project, error) {
	for _, p := range f.projects {
		if p.ID == id {
			return p, nil
		}
	}
	return domain.Project{}, provider.NewError(provider.KindNotFound, "get project", "not found", nil)
}
func (f *fakeClient) DeleteProject(_ context.Context, id string) error {
	if id == f.failDeleteProject {
		return provider.NewError(provider.KindTemporary, "delete project", "boom", nil)
	}
	f.deletedProjects[id] = true
	return nil
}
func (f *fakeClient) ArchiveProject(context.Context, string) error { return nil }
func (f *fakeClient) ListGroupMembers(context.Context, domain.Scope) ([]domain.User, error) {
	return f.members, f.membersErr
}
func (f *fakeClient) GetGroupMember(_ context.Context, groupID, userID string) (domain.User, error) {
	for _, u := range f.members {
		if u.ID == userID {
			u.GroupID = groupID
			u.MembershipOrigin = domain.MembershipDirect
			return u, nil
		}
	}
	return domain.User{}, provider.NewError(provider.KindNotFound, "get member", "not found", nil)
}
func (f *fakeClient) RemoveGroupMember(_ context.Context, groupID, userID string) error {
	f.removedMembers[groupID+"/"+userID] = true
	return nil
}
func (f *fakeClient) GetUser(_ context.Context, id string) (domain.User, error) {
	for _, u := range f.members {
		if u.ID == id {
			return u, nil
		}
	}
	return domain.User{}, provider.NewError(provider.KindNotFound, "get user", "not found", nil)
}
func (f *fakeClient) BlockUser(context.Context, string) error { return nil }
func (f *fakeClient) ListBillableGroupMembers(context.Context, string) (map[string]bool, error) {
	return f.billable, f.billableErr
}
func (f *fakeClient) ListUserMemberships(_ context.Context, userID string) ([]domain.Membership, error) {
	return f.memberships[userID], nil
}
func (f *fakeClient) GetPipelineConfig(_ context.Context, projectID string) ([]byte, bool, error) {
	content, ok := f.ciFiles[projectID]
	return content, ok, nil
}
func (f *fakeClient) ProposePipelineTagChange(_ context.Context, projectID string, _ []byte, tags []string) (string, error) {
	if f.proposeErr != nil {
		return "", f.proposeErr
	}
	f.proposedTags[projectID] = append([]string{}, tags...)
	return f.proposeURL, nil
}
func (f *fakeClient) ProposePipelineTagRename(_ context.Context, projectID string, _ []byte, renames []domain.TagRename) (string, []string, error) {
	if f.renameErr != nil {
		return "", nil, f.renameErr
	}
	f.renamedTags[projectID] = append([]domain.TagRename{}, renames...)
	return f.renameURL, f.closedProposalURLs, nil
}
func (f *fakeClient) ClosePipelineTagProposals(_ context.Context, projectID string, oldTags []string) ([]string, error) {
	if f.closeOnlyErr != nil {
		return nil, f.closeOnlyErr
	}
	f.closeOnlyCalls[projectID] = append([]string{}, oldTags...)
	return f.closeOnlyURLs, nil
}
func (f *fakeClient) GetMergedPipelineConfig(_ context.Context, projectID string) ([]byte, []domain.PipelineInclude, error) {
	return f.mergedCI[projectID], f.ciIncludes[projectID], nil
}
func (f *fakeClient) ListPipelineTagProposals(_ context.Context, projectID string, _ []string) ([]domain.PipelineProposal, error) {
	return f.proposals[projectID], nil
}
func (f *fakeClient) ListRunnersForProjects(context.Context, domain.Scope, []string) ([]domain.Runner, error) {
	return f.runners, f.runnersErr
}
func (f *fakeClient) GetRunnerForProjects(_ context.Context, runnerID string, _ domain.Scope, _ []string) (domain.Runner, error) {
	for _, runner := range f.runners {
		if runner.ID == runnerID {
			return runner, nil
		}
	}
	return domain.Runner{}, provider.NewError(provider.KindNotFound, "get runner", "not found", nil)
}
func (f *fakeClient) GetRunnerTags(_ context.Context, runnerID string) ([]string, error) {
	return f.runnerTags[runnerID], nil
}
func (f *fakeClient) UpdateRunnerTags(_ context.Context, runnerID string, _ []string, tags []string) error {
	f.runnerTags[runnerID] = tags
	return nil
}
func (f *fakeClient) CurrentUser(context.Context) (domain.User, error) {
	return f.currentUser, f.currentUserErr
}
func (f *fakeClient) Capabilities(context.Context) (provider.Capabilities, error) {
	return f.capabilities, nil
}
func (f *fakeClient) Info(context.Context) (provider.Info, error) { return f.info, f.infoErr }

var _ provider.Client = (*fakeClient)(nil)

// testEnv builds an *env with a preset client (bypassing provider
// construction/config validation - see requireClient) and a fixed clock,
// ready to hand to a command constructor.
func testEnv(client provider.Client) *env {
	resolver, err := secrets.NewDefaultResolver()
	if err != nil {
		panic(err)
	}
	return &env{
		cfg:            config.Default(),
		client:         client,
		format:         output.FormatTable,
		clock:          domain.RealClock{},
		secretResolver: resolver,
	}
}

// runCmd executes cmd with args, capturing stdout/stderr, without going
// through the root command's persistent flags or PersistentPreRunE - each
// command constructor (newProjectsCmd(e), etc.) is tested as its own
// subtree, matching how it's actually wired into the real root command.
func runCmd(cmd *cobra.Command, args []string) (stdout, stderr string, err error) {
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}
