package project

import (
	"context"
	"testing"

	"github.com/domehahn/housekeeping/internal/domain"
)

func TestNamePolicy_ExcludeTakesPrecedenceOverInclude(t *testing.T) {
	p, err := NewNamePolicy(
		[]string{"^sandbox-.*", "^test-.*"},
		[]string{"^production$", "^infrastructure$"},
	)
	if err != nil {
		t.Fatalf("NewNamePolicy: %v", err)
	}

	tests := []struct {
		path      string
		wantMatch bool
	}{
		{"sandbox-foo", true},
		{"test-bar", true},
		{"production", false},     // matches exclude
		{"infrastructure", false}, // matches exclude
		{"other", false},          // matches no include pattern
	}
	for _, tc := range tests {
		proj := domain.Project{Path: tc.path}
		got := p.Evaluate(context.Background(), proj).Match
		if got != tc.wantMatch {
			t.Errorf("%s: Match = %v, want %v", tc.path, got, tc.wantMatch)
		}
	}
}

func TestNamePolicy_NoIncludeMeansAllNonExcludedMatch(t *testing.T) {
	p, err := NewNamePolicy(nil, []string{"^protected$"})
	if err != nil {
		t.Fatalf("NewNamePolicy: %v", err)
	}
	if !p.Evaluate(context.Background(), domain.Project{Path: "anything"}).Match {
		t.Error("expected match: no include patterns means everything not excluded matches")
	}
	if p.Evaluate(context.Background(), domain.Project{Path: "protected"}).Match {
		t.Error("expected no match: exclude must still apply")
	}
}

func TestProtection_ProjectPathsAndRegex(t *testing.T) {
	p, err := NewProtection(
		[]string{"company/platform/production"},
		[]string{".*/terraform-state$"},
	)
	if err != nil {
		t.Fatalf("NewProtection: %v", err)
	}

	tests := []struct {
		path string
		want bool
	}{
		{"company/platform/production", true},
		{"company/infra/terraform-state", true},
		{"company/sandbox/foo", false},
	}
	for _, tc := range tests {
		proj := domain.Project{FullPath: tc.path}
		got, _ := p.IsProtected(proj)
		if got != tc.want {
			t.Errorf("%s: IsProtected = %v, want %v", tc.path, got, tc.want)
		}
	}
}
