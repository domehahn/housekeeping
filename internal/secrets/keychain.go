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

// KeyringStore adds the mutations used only by explicit auth login/logout
// commands. Normal credential resolution depends solely on the narrower
// read-only Keyring interface above.
type KeyringStore interface {
	Keyring
	Set(service, account, value string) error
	Delete(service, account string) error
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
		var err error
		account, err = ResolveAccount("", r.CurrentUser)
		if err != nil {
			return "", err
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

func (nativeKeyring) Set(service, account, value string) error {
	return keyring.Set(service, account, value)
}

func (nativeKeyring) Delete(service, account string) error {
	return keyring.Delete(service, account)
}

// NewNativeKeyringStore returns the production OS credential-store adapter.
func NewNativeKeyringStore() KeyringStore { return nativeKeyring{} }

// IsNotFound reports the native library's portable missing-entry error.
func IsNotFound(err error) bool { return errors.Is(err, keyring.ErrNotFound) }

// ResolveAccount applies the same current-OS-user default used by resolution.
func ResolveAccount(account string, currentUser CurrentUser) (string, error) {
	if account != "" {
		return account, nil
	}
	if currentUser == nil {
		currentUser = operatingSystemUser
	}
	account, err := currentUser()
	if err != nil {
		return "", fmt.Errorf("determine current operating-system user: %w", err)
	}
	if account == "" {
		return "", fmt.Errorf("current operating-system user has an empty username")
	}
	return account, nil
}

func operatingSystemUser() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.Username, nil
}
