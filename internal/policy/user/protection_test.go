package user

import (
	"testing"

	"github.com/domehahn/housekeeping/internal/domain"
)

func TestProtection(t *testing.T) {
	p, err := NewProtection(
		[]string{"root"},
		[]string{"^svc-", "^bot-"},
		[]string{"owner"},
		"999",
	)
	if err != nil {
		t.Fatalf("NewProtection: %v", err)
	}

	tests := []struct {
		name string
		user domain.User
		want bool
	}{
		{"exact username match", domain.User{ID: "1", Username: "root"}, true},
		{"regex svc- prefix", domain.User{ID: "2", Username: "svc-deploy"}, true},
		{"regex bot- prefix", domain.User{ID: "3", Username: "bot-ci"}, true},
		{"owner access level", domain.User{ID: "4", Username: "alice", AccessLevel: domain.AccessLevelOwner}, true},
		{"current user id", domain.User{ID: "999", Username: "cleanup-bot"}, true},
		{"unprotected regular user", domain.User{ID: "5", Username: "bob", AccessLevel: domain.AccessLevelDeveloper}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := p.IsProtected(tc.user)
			if got != tc.want {
				t.Errorf("IsProtected(%+v) = %v, want %v (reason: %q)", tc.user, got, tc.want, reason)
			}
			if got && reason == "" {
				t.Error("expected a non-empty reason when protected")
			}
		})
	}
}
