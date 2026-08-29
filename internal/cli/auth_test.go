package cli

import (
	"errors"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"
)

type fakeKeyringStore struct {
	values map[string]string
	err    error
}

func (f *fakeKeyringStore) key(service, account string) string { return service + "/" + account }

func (f *fakeKeyringStore) Get(service, account string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	value, ok := f.values[f.key(service, account)]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func (f *fakeKeyringStore) Set(service, account, value string) error {
	if f.err != nil {
		return f.err
	}
	f.values[f.key(service, account)] = value
	return nil
}

func (f *fakeKeyringStore) Delete(service, account string) error {
	if f.err != nil {
		return f.err
	}
	key := f.key(service, account)
	if _, ok := f.values[key]; !ok {
		return keyring.ErrNotFound
	}
	delete(f.values, key)
	return nil
}

func TestAuthLoginStatusLogoutWithInjectedKeyring(t *testing.T) {
	store := &fakeKeyringStore{values: map[string]string{}}
	e := testEnv(nil)
	e.keyringStore = store
	e.currentOSUser = func() (string, error) { return "os-user", nil }
	e.secretPrompt = func(string) (string, error) { return "do-not-print-secret", nil }

	stdout, _, err := runCmd(newAuthCmd(e), []string{"login", "--service", "scm-cleaner"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout, "do-not-print-secret") || store.values["scm-cleaner/os-user"] != "do-not-print-secret" {
		t.Fatalf("login output/value = %q / %+v", stdout, store.values)
	}

	stdout, _, err = runCmd(newAuthCmd(e), []string{"status", "--service", "scm-cleaner"})
	if err != nil || !strings.Contains(stdout, "exists") || strings.Contains(stdout, "do-not-print-secret") {
		t.Fatalf("status output/error = %q / %v", stdout, err)
	}

	stdout, _, err = runCmd(newAuthCmd(e), []string{"logout", "--service", "scm-cleaner"})
	if err != nil || !strings.Contains(stdout, "removed") || len(store.values) != 0 {
		t.Fatalf("logout output/error/values = %q / %v / %+v", stdout, err, store.values)
	}
}

func TestAuthBackendErrorDoesNotLeakPromptedSecret(t *testing.T) {
	e := testEnv(nil)
	e.keyringStore = &fakeKeyringStore{values: map[string]string{}, err: errors.New("backend leaked do-not-print-secret")}
	e.currentOSUser = func() (string, error) { return "os-user", nil }
	e.secretPrompt = func(string) (string, error) { return "do-not-print-secret", nil }
	_, _, err := runCmd(newAuthCmd(e), []string{"login", "--service", "scm-cleaner"})
	if err == nil || strings.Contains(err.Error(), "do-not-print-secret") {
		t.Fatalf("error = %v", err)
	}
}
