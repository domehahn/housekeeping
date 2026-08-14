// Package domain contains provider-independent business types and the
// vocabulary the rest of the application is built on. Nothing in this
// package may import a provider-specific SDK or know about GitLab, GitHub,
// or any other concrete platform.
package domain

import "time"

// Clock abstracts "now" so policy evaluation is deterministic in tests.
// Business logic must never call time.Now() directly.
type Clock interface {
	Now() time.Time
}

// RealClock is the production Clock backed by the system clock.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

// FixedClock is a Clock that always returns the same instant. It exists in
// the domain package (rather than only in _test.go files) because it is a
// legitimate, reusable collaborator for any caller that needs deterministic
// time - not just unit tests of this package.
type FixedClock struct {
	Instant time.Time
}

func (c FixedClock) Now() time.Time { return c.Instant }
