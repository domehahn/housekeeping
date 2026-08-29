package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/domehahn/housekeeping/internal/secrets"
)

type authKeychainFlags struct {
	source  string
	service string
	account string
}

func newAuthCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage scm-cleaner credentials in the native OS keychain",
		Long:  "auth stores, checks, or removes credentials only after an explicit subcommand. Token values are never accepted as CLI flags or printed.",
	}
	cmd.AddCommand(newAuthLoginCmd(e), newAuthStatusCmd(e), newAuthLogoutCmd(e))
	return cmd
}

func newAuthLoginCmd(e *env) *cobra.Command {
	flags := &authKeychainFlags{}
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Prompt securely and store a credential in the native OS keychain",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, account, err := resolveAuthKeychainReference(e, flags)
			if err != nil {
				return exitErr(ExitInvalidConfiguration, err)
			}
			prompt := e.secretPrompt
			if prompt == nil {
				prompt = readSecretFromTerminal
			}
			value, err := prompt("GitLab token: ")
			if err != nil {
				return exitErr(ExitGeneralError, err)
			}
			if value == "" {
				return exitErr(ExitInvalidConfiguration, fmt.Errorf("token must not be empty"))
			}
			if e.keyringStore == nil {
				return exitErr(ExitGeneralError, fmt.Errorf("native keychain is not configured"))
			}
			if err := e.keyringStore.Set(service, account, value); err != nil {
				return exitErr(ExitGeneralError, fmt.Errorf("store credential for service %q and account %q: keychain backend failed", service, account))
			}
			cmd.Printf("Credential stored for service %q and account %q.\n", service, account)
			return nil
		},
	}
	addAuthKeychainFlags(cmd, flags)
	return cmd
}

func newAuthStatusCmd(e *env) *cobra.Command {
	flags := &authKeychainFlags{}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check whether a native keychain credential exists without printing it",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, account, err := resolveAuthKeychainReference(e, flags)
			if err != nil {
				return exitErr(ExitInvalidConfiguration, err)
			}
			if e.keyringStore == nil {
				return exitErr(ExitGeneralError, fmt.Errorf("native keychain is not configured"))
			}
			value, err := e.keyringStore.Get(service, account)
			if err != nil {
				if secrets.IsNotFound(err) {
					return exitErr(ExitAuthenticationError, fmt.Errorf("no credential found for service %q and account %q", service, account))
				}
				return exitErr(ExitGeneralError, fmt.Errorf("check credential for service %q and account %q: keychain backend failed", service, account))
			}
			if value == "" {
				return exitErr(ExitAuthenticationError, fmt.Errorf("credential for service %q and account %q is empty", service, account))
			}
			cmd.Printf("Credential exists for service %q and account %q.\n", service, account)
			return nil
		},
	}
	addAuthKeychainFlags(cmd, flags)
	return cmd
}

func newAuthLogoutCmd(e *env) *cobra.Command {
	flags := &authKeychainFlags{}
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove a credential from the native OS keychain",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, account, err := resolveAuthKeychainReference(e, flags)
			if err != nil {
				return exitErr(ExitInvalidConfiguration, err)
			}
			if e.keyringStore == nil {
				return exitErr(ExitGeneralError, fmt.Errorf("native keychain is not configured"))
			}
			if err := e.keyringStore.Delete(service, account); err != nil {
				if secrets.IsNotFound(err) {
					cmd.Printf("No credential exists for service %q and account %q.\n", service, account)
					return nil
				}
				return exitErr(ExitGeneralError, fmt.Errorf("remove credential for service %q and account %q: keychain backend failed", service, account))
			}
			cmd.Printf("Credential removed for service %q and account %q.\n", service, account)
			return nil
		},
	}
	addAuthKeychainFlags(cmd, flags)
	return cmd
}

func addAuthKeychainFlags(cmd *cobra.Command, flags *authKeychainFlags) {
	cmd.Flags().StringVar(&flags.source, "source", "keychain", "credential destination/source; only keychain is supported")
	cmd.Flags().StringVar(&flags.service, "service", "", "native keychain service; defaults to configured token.service")
	cmd.Flags().StringVar(&flags.account, "account", "", "native keychain account; defaults to configured token.account or current OS user")
}

func resolveAuthKeychainReference(e *env, flags *authKeychainFlags) (string, string, error) {
	if flags.source != "keychain" {
		return "", "", fmt.Errorf("--source must be keychain")
	}
	service, account := flags.service, flags.account
	configured := e.cfg.Provider.GitLab.Token
	if configured.Source == secrets.SourceKeychain {
		if service == "" {
			service = configured.Service
		}
		if account == "" {
			account = configured.Account
		}
	}
	if service == "" {
		return "", "", fmt.Errorf("--service is required when no keychain service is configured")
	}
	resolvedAccount, err := secrets.ResolveAccount(account, e.currentOSUser)
	if err != nil {
		return "", "", err
	}
	return service, resolvedAccount, nil
}

func readSecretFromTerminal(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("secure token prompt requires an interactive terminal")
	}
	_, _ = fmt.Fprint(os.Stderr, prompt)
	value, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read token: %w", err)
	}
	return string(value), nil
}
