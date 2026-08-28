package providerfactory

// Descriptor describes a provider type's static, configuration-independent
// facts: whether an adapter exists for it at all, and what configuration
// it needs before a live connection can be attempted. Unlike
// `provider info`/`provider capabilities` (which query a real, connected
// instance and therefore require a working base_url/token), this
// information is fixed at compile time and needs no configuration, no
// network access, and no credentials to display - it exists specifically
// so `scm-cleaner provider list` works for a first-time user who has not
// configured anything yet.
type Descriptor struct {
	Type           string
	Status         string // "implemented" | "planned"
	Description    string
	RequiredConfig []string
}

// Registry returns the static descriptor for every provider type this
// build knows about. Only "gitlab" is Status: "implemented" - see
// docs/architecture.md "Provider Extension" and the README's "Adding
// another Provider" section for how a second entry would be added here.
// Providers are never listed as "implemented" without a real adapter
// behind them (no fake/placeholder entries).
func Registry() []Descriptor {
	return []Descriptor{
		{
			Type:        "gitlab",
			Status:      "implemented",
			Description: "GitLab (self-managed and GitLab.com)",
			RequiredConfig: []string{
				"provider.gitlab.base_url (or --gitlab-url)",
				"provider.gitlab.token (env or keychain reference), or legacy provider.gitlab.token_env/--token-env",
			},
		},
	}
}

// Find returns the descriptor for a given provider type, and whether one
// was found.
func Find(providerType string) (Descriptor, bool) {
	for _, d := range Registry() {
		if d.Type == providerType {
			return d, true
		}
	}
	return Descriptor{}, false
}
