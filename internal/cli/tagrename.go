package cli

import (
	"fmt"
	"strings"

	"github.com/domehahn/housekeeping/internal/domain"
)

// parseTagRenames parses "OLD:NEW" pairs (as passed via --replace-tag) into
// domain.TagRename values, rejecting a malformed pair, an empty half, or
// Old == New. Shared between "pipelines" and "runners".
func parseTagRenames(raw []string) ([]domain.TagRename, error) {
	renames := make([]domain.TagRename, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, entry := range raw {
		old, next, found := strings.Cut(entry, ":")
		old, next = strings.TrimSpace(old), strings.TrimSpace(next)
		if !found || old == "" || next == "" {
			return nil, fmt.Errorf("--replace-tag %q must be of the form OLD:NEW", entry)
		}
		if old == next {
			return nil, fmt.Errorf("--replace-tag %q: old and new tag must differ", entry)
		}
		if seen[old] {
			return nil, fmt.Errorf("--replace-tag: duplicate old tag %q", old)
		}
		seen[old] = true
		renames = append(renames, domain.TagRename{Old: old, New: next})
	}
	return renames, nil
}

// requireExactlyOneTagMode enforces that exactly one of --tag/--replace-tag
// was supplied - a run either adds tags or renames tags, never a mix, so
// each plan's diff and Merge Request stays unambiguous.
func requireExactlyOneTagMode(tags, replaceTags []string) error {
	if len(tags) > 0 && len(replaceTags) > 0 {
		return fmt.Errorf("--tag and --replace-tag are mutually exclusive")
	}
	if len(tags) == 0 && len(replaceTags) == 0 {
		return fmt.Errorf("either --tag or --replace-tag is required")
	}
	return nil
}
