package domain

// Runner is a provider-independent representation of a CI runner available
// to one or more projects. It exists solely to support the CI tag management
// feature (see internal/ciyaml and the `runners` CLI commands) - it is not
// part of ordinary project/user discovery.
type Runner struct {
	ID          string
	Description string
	TagList     []string
	RunnerType  string

	// Shared indicates the runner is not dedicated to a single project -
	// it may be available to projects outside whichever scope is currently
	// being evaluated. This is the provider's own determination (GitLab's
	// is_shared), never inferred.
	Shared bool

	// InScopeProjectPaths lists explicit project assignments among the
	// projects actually evaluated.
	InScopeProjectPaths []string

	// OutOfScopeProjectPaths lists explicit project assignments outside the
	// evaluated scope. Implicit group/instance reach is represented by
	// ImpactKnown/ImpactReason instead of being guessed from this list.
	OutOfScopeProjectPaths []string

	// ImpactKnown is true only when the provider can prove the complete
	// effect of changing this runner from the evaluated scope. Instance
	// runners and inherited group runners deliberately fail closed.
	ImpactKnown   bool
	ImpactReason  string
	OwnerGroupIDs []string
}

// HasTag reports whether tag is already present in the runner's tag list.
func (r Runner) HasTag(tag string) bool {
	for _, t := range r.TagList {
		if t == tag {
			return true
		}
	}
	return false
}
