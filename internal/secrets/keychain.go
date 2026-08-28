package secrets

import (
	"context"
	"errors"
	"fmt"
	"os/user"

	keyring "github.com/zalando/go-keyring"
)

// Keyring is the smallest native-keyring API needed for secret resolution.
type Keyring interface {
	Get(service, account string) (string, error)
}

// CurrentUser resolves the current operating-system account name.
type CurrentUser func() (string, error)

// KeychainResolver resolves credentials from the operating system's native
// credential store. Tests inject a fake Keyring and never touch the real one.
type KeychainResolver struct {
	Keyring     Keyring
	CurrentUser CurrentUser
}

// Resolve gets a credential by service and account. An omitted account
// defaults to the current operating-system user.
func (r KeychainResolver) Resolve(ctx context.Context, ref Reference) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("keychain lookup canceled: %w", err)
	}
	if ref.Source != SourceKeychain {
		return "", fmt.Errorf("keychain resolver received source %q", ref.Source)
	}
	if ref.Service == "" {
		return "", fmt.Errorf("keychain service is required")
	}

	account := ref.Account
	if account == "" {
		currentUser := r.CurrentUser
		if currentUser == nil {
			currentUser = operatingSystemUser
		}
		var err error
		account, err = currentUser()
		if err != nil {
			return "", fmt.Errorf("determine current operating-system user: %w", err)
		}
		if account == "" {
			return "", fmt.Errorf("current operating-system user has an empty username")
		}
	}

	store := r.Keyring
	if store == nil {
		store = nativeKeyring{}
	}
	value, err := store.Get(ref.Service, account)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", fmt.Errorf("credential not found for service %q and account %q", ref.Service, account)
		}
		return "", fmt.Errorf("read credential for service %q and account %q: keychain backend failed", ref.Service, account)
	}
	if value == "" {
		return "", fmt.Errorf("credential for service %q and account %q is empty", ref.Service, account)
	}
	return value, nil
}

type nativeKeyring struct{}

func (nativeKeyring) Get(service, account string) (string, error) {
	return keyring.Get(service, account)
}

func operatingSystemUser() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.Username, nil
}
