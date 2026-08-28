package secrets

// NewDefaultResolver builds the production resolver registry. Construction is
// explicit so tests and callers do not depend on mutable global state.
func NewDefaultResolver() (Resolver, error) {
	return NewRegistry(map[Source]Resolver{
		SourceEnv:      EnvResolver{},
		SourceKeychain: KeychainResolver{},
	})
}
