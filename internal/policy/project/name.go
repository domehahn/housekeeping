package project

import (
	"context"
	"fmt"
	"regexp"

	"github.com/domehahn/housekeeping/internal/domain"
)

// NamePolicy matches projects whose short Path (the project slug, not its
// full namespace path - use Protection for full-path matching such as
// "company/platform/production") satisfies configured include/exclude
// regex rules. Exclude always takes precedence over include, matching the
// requirement that excludes must win.
type NamePolicy struct {
	Include []*regexp.Regexp
	Exclude []*regexp.Regexp
}

// NewNamePolicy compiles include/exclude patterns.
func NewNamePolicy(include, exclude []string) (NamePolicy, error) {
	inc, err := compileAll(include)
	if err != nil {
		return NamePolicy{}, fmt.Errorf("policy: compile include pattern: %w", err)
	}
	exc, err := compileAll(exclude)
	if err != nil {
		return NamePolicy{}, fmt.Errorf("policy: compile exclude pattern: %w", err)
	}
	return NamePolicy{Include: inc, Exclude: exc}, nil
}

func compileAll(patterns []string) ([]*regexp.Regexp, error) {
	var out []*regexp.Regexp
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", p, err)
		}
		out = append(out, re)
	}
	return out, nil
}

func (p NamePolicy) Name() string { return "project-name" }

func (p NamePolicy) Evaluate(_ context.Context, proj domain.Project) domain.Evaluation {
	for _, re := range p.Exclude {
		if re.MatchString(proj.Path) {
			return domain.Evaluation{
				Match:   false,
				Reasons: []string{fmt.Sprintf("path %q matches exclude pattern %q (excludes take precedence)", proj.Path, re.String())},
			}
		}
	}
	if len(p.Include) == 0 {
		return domain.Evaluation{Match: true, Reasons: []string{"no include patterns configured: all non-excluded paths match"}}
	}
	for _, re := range p.Include {
		if re.MatchString(proj.Path) {
			return domain.Evaluation{
				Match:   true,
				Reasons: []string{fmt.Sprintf("path %q matches include pattern %q", proj.Path, re.String())},
			}
		}
	}
	return domain.Evaluation{Match: false, Reasons: []string{fmt.Sprintf("path %q matches no include pattern", proj.Path)}}
}
