// Package user contains provider-independent policies that decide whether
// a domain.User matches a cleanup criterion. Policies never perform I/O and
// never know which SCM provider produced the User.
package user

import "fmt"

// UnknownBehavior controls how a policy treats a Timestamp whose status is
// domain.ActivityUnknown - i.e. the provider could not supply the value.
//
// The safe default is Skip: unknown data must never be silently treated as
// a match. See docs/adr/0003-unknown-activity-safe-default.md.
type UnknownBehavior string

const (
	// UnknownSkip never matches on unknown data (the safe default).
	UnknownSkip UnknownBehavior = "skip"
	// UnknownWarn behaves like Skip but the caller is expected to
	// surface a warning reason so operators notice missing data.
	UnknownWarn UnknownBehavior = "warn"
	// UnknownMatch treats unknown data as if it satisfied the threshold.
	// This is dangerous (an API/permission gap could then trigger
	// destructive actions) and must be explicitly configured.
	UnknownMatch UnknownBehavior = "match"
)

// ParseUnknownBehavior validates a config string, defaulting to Skip.
func ParseUnknownBehavior(s string) (UnknownBehavior, error) {
	switch UnknownBehavior(s) {
	case "", UnknownSkip:
		return UnknownSkip, nil
	case UnknownWarn:
		return UnknownWarn, nil
	case UnknownMatch:
		return UnknownMatch, nil
	default:
		return "", fmt.Errorf("policy: unknown_activity must be one of skip|warn|match, got %q", s)
	}
}
