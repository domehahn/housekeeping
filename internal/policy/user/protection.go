package user

import (
	"fmt"
	"regexp"

	"github.com/domehahn/housekeeping/internal/domain"
)

// Protection implements domain.UserProtectionRule. Protection always takes
// precedence over any matching policy - a protected user is never included
// in a plan, regardless of how strongly it matches an inactivity policy.
type Protection struct {
	// Usernames is an exact-match denylist (e.g. "root").
	Usernames map[string]bool
	// Regexes match against username (e.g. "^svc-", "^bot-").
	Regexes []*regexp.Regexp
	// AccessLevels protects users holding one of these membership levels
	// (e.g. always protect owners).
	AccessLevels map[domain.AccessLevel]bool
	// CurrentUserID, when set, protects the authenticated caller so a
	// misconfigured run cannot lock itself out or remove its own token
	// owner.
	CurrentUserID string
}

// NewProtection compiles a Protection from raw configuration values.
// Regexes are validated by config.Validate before this is called, but
// invalid patterns here still return an error defensively.
func NewProtection(usernames, regexes []string, accessLevels []string, currentUserID string) (Protection, error) {
	p := Protection{
		Usernames:     map[string]bool{},
		AccessLevels:  map[domain.AccessLevel]bool{},
		CurrentUserID: currentUserID,
	}
	for _, u := range usernames {
		p.Usernames[u] = true
	}
	for _, r := range regexes {
		re, err := regexp.Compile(r)
		if err != nil {
			return Protection{}, fmt.Errorf("policy: compile protection regex %q: %w", r, err)
		}
		p.Regexes = append(p.Regexes, re)
	}
	for _, lvl := range accessLevels {
		p.AccessLevels[domain.AccessLevel(lvl)] = true
	}
	return p, nil
}

func (p Protection) IsProtected(u domain.User) (bool, domain.Reason) {
	if p.CurrentUserID != "" && u.ID == p.CurrentUserID {
		return true, "protected: is the currently authenticated user"
	}
	if p.Usernames[u.Username] {
		return true, fmt.Sprintf("protected: username %q is in the protected usernames list", u.Username)
	}
	for _, re := range p.Regexes {
		if re.MatchString(u.Username) {
			return true, fmt.Sprintf("protected: username %q matches protected pattern %q", u.Username, re.String())
		}
	}
	if p.AccessLevels[u.AccessLevel] {
		return true, fmt.Sprintf("protected: access level %q is protected", u.AccessLevel)
	}
	return false, ""
}
