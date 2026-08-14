package domain

import "time"

// ActivityStatus records whether a point-in-time fact (e.g. "last login")
// is a known value obtained from the provider, or unknown because the
// provider did not return it (insufficient permissions, unsupported API,
// field not populated, etc).
//
// This distinction is safety-critical: an unknown value must never be
// silently treated as "never happened" / "inactive". See
// docs/adr/0003-unknown-activity-safe-default.md.
type ActivityStatus string

const (
	// ActivityKnown means the provider returned a definite value. A nil
	// time.Time combined with ActivityKnown means "known to have never
	// happened" (e.g. account never signed in).
	ActivityKnown ActivityStatus = "known"

	// ActivityUnknown means the provider could not or did not supply the
	// value (e.g. missing permissions, field unsupported on this
	// instance/edition). The associated time.Time must be ignored.
	ActivityUnknown ActivityStatus = "unknown"
)

// Timestamp pairs an optional point in time with an explicit status so
// "no data" and "never happened" can never be confused.
type Timestamp struct {
	At     *time.Time
	Status ActivityStatus
}

// Known constructs a Timestamp with a known value (which may itself be nil,
// meaning "known to have never occurred").
func Known(at *time.Time) Timestamp {
	return Timestamp{At: at, Status: ActivityKnown}
}

// Unknown constructs a Timestamp whose value could not be determined.
func Unknown() Timestamp {
	return Timestamp{Status: ActivityUnknown}
}

// IsKnown reports whether the provider supplied a definite value.
func (t Timestamp) IsKnown() bool { return t.Status == ActivityKnown }

// DaysAgo returns the number of whole days between the timestamp and now,
// and true if the timestamp is known and non-nil. Callers must check the
// bool before using the count.
func (t Timestamp) DaysAgo(now time.Time) (days int, ok bool) {
	if !t.IsKnown() || t.At == nil {
		return 0, false
	}
	d := now.Sub(*t.At)
	return int(d.Hours() / 24), true
}
