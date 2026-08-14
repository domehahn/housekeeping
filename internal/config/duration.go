package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Day-scale units Go's time.ParseDuration does not support. Months and
// years are deliberately approximate (30 and 365 days respectively) and
// that approximation is documented rather than hidden - cleanup thresholds
// do not need calendar precision.
const (
	hoursPerDay   = 24
	hoursPerWeek  = hoursPerDay * 7
	hoursPerMonth = hoursPerDay * 30
	hoursPerYear  = hoursPerDay * 365
)

// ParseDuration parses a duration string using Go's native units (ns, us,
// ms, s, m, h) plus the day-scale suffixes d, w, m(onths), y. Because "m"
// already means minutes in Go, the day-scale month unit uses "mo" to stay
// unambiguous; a bare trailing "m" is always minutes.
//
// Examples: "30d", "90d", "12h", "6w", "3mo", "1y".
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("duration: empty value")
	}

	unit, numLen := splitUnit(s)
	if numLen == 0 {
		// No recognized day-scale suffix; fall back to Go's parser for
		// h/m/s/ms/us/ns combinations.
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("duration: invalid value %q: %w", s, err)
		}
		return d, nil
	}

	numPart := s[:numLen]
	value, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, fmt.Errorf("duration: invalid numeric value in %q: %w", s, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("duration: negative value not allowed in %q", s)
	}

	var hours float64
	switch unit {
	case "d":
		hours = value * hoursPerDay
	case "w":
		hours = value * hoursPerWeek
	case "mo":
		hours = value * hoursPerMonth
	case "y":
		hours = value * hoursPerYear
	default:
		return 0, fmt.Errorf("duration: unsupported unit %q in %q", unit, s)
	}

	return time.Duration(hours * float64(time.Hour)), nil
}

// splitUnit looks for one of the day-scale suffixes (checked longest-first
// so "mo" is not mistaken for a lone "m") at the end of s and returns the
// unit and the length of the numeric prefix. numLen is 0 if none matched.
func splitUnit(s string) (unit string, numLen int) {
	for _, u := range []string{"mo", "d", "w", "y"} {
		if strings.HasSuffix(s, u) {
			return u, len(s) - len(u)
		}
	}
	return "", 0
}
