package config

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"30d", 30 * 24 * time.Hour, false},
		{"90d", 90 * 24 * time.Hour, false},
		{"12h", 12 * time.Hour, false},
		{"6w", 6 * 7 * 24 * time.Hour, false},
		{"3mo", 3 * 30 * 24 * time.Hour, false},
		{"1y", 365 * 24 * time.Hour, false},
		{"30m", 30 * time.Minute, false}, // bare "m" stays minutes, not months
		{"1.5d", time.Duration(1.5 * 24 * float64(time.Hour)), false},
		{"", 0, true},
		{"-1d", 0, true},
		{"30x", 0, true},
		{"notaduration", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseDuration(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ParseDuration(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Errorf("ParseDuration(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
