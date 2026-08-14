package project

import (
	"fmt"
	"regexp"

	"github.com/domehahn/housekeeping/internal/domain"
)

// Protection implements domain.ProjectProtectionRule. A protected project
// is never included in a plan, regardless of how strongly it matches an
// inactivity or name policy.
type Protection struct {
	Paths   map[string]bool
	Regexes []*regexp.Regexp
}

// NewProtection compiles a Protection from raw configuration values.
func NewProtection(paths, regexes []string) (Protection, error) {
	p := Protection{Paths: map[string]bool{}}
	for _, path := range paths {
		p.Paths[path] = true
	}
	for _, r := range regexes {
		re, err := regexp.Compile(r)
		if err != nil {
			return Protection{}, fmt.Errorf("policy: compile protection regex %q: %w", r, err)
		}
		p.Regexes = append(p.Regexes, re)
	}
	return p, nil
}

func (p Protection) IsProtected(proj domain.Project) (bool, domain.Reason) {
	if p.Paths[proj.FullPath] {
		return true, fmt.Sprintf("protected: path %q is in the protected paths list", proj.FullPath)
	}
	for _, re := range p.Regexes {
		if re.MatchString(proj.FullPath) {
			return true, fmt.Sprintf("protected: path %q matches protected pattern %q", proj.FullPath, re.String())
		}
	}
	return false, ""
}
