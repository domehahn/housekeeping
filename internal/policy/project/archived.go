package project

import (
	"context"

	"github.com/domehahn/housekeeping/internal/domain"
)

// ArchivedPolicy matches projects that are already archived. Useful for
// cleanup rules that target the "archived and forgotten" backlog
// separately from activity-based inactivity.
type ArchivedPolicy struct{}

func (ArchivedPolicy) Name() string { return "archived-project" }

func (ArchivedPolicy) Evaluate(_ context.Context, proj domain.Project) domain.Evaluation {
	if proj.Archived {
		return domain.Evaluation{Match: true, Reasons: []string{"project is archived"}}
	}
	return domain.Evaluation{Match: false, Reasons: []string{"project is not archived"}}
}
