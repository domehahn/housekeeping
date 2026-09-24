package domain

// Branch is a provider-independent repository branch.
type Branch struct {
	Name      string
	Protected bool
	Merged    bool
	Default   bool
	WebURL    string
}
