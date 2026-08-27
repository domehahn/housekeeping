package domain

// Runner is a provider-independent representation of a CI runner used by
// one or more projects. It exists solely to support the CI tag management
// feature (see internal/ciyaml and the `runners` CLI commands) - it is not
// part of ordinary project/user discovery.
type Runner struct {
	ID          string
	Description string
	TagList     []string

	// Shared indicates the runner is not dedicated to a single project -
	// it may be used by projects outside whichever scope is currently
	// being evaluated. This is the provider's own determination (GitLab's
	// is_shared), never inferred.
	Shared bool

	// InScopeProjectPaths lists the full paths of every project, among
	// the ones actually evaluated, that use this runner.
	InScopeProjectPaths []string

	// OutOfScopeProjectPaths lists the full paths of every OTHER project
	// (not part of the evaluated scope) that also uses this runner - the
	// "blast radius" of changing its tags. Empty for a non-shared runner
	// or one only used by in-scope projects.
	OutOfScopeProjectPaths []string
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
