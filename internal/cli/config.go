package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newConfigCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Configuration utilities"}
	cmd.AddCommand(newConfigValidateCmd(e))
	return cmd
}

func newConfigValidateCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the configuration file (YAML syntax, unknown fields, semantic checks)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// e.cfg was already loaded (and, critically, already validated
			// by config.Load / cfg.Validate as part of PersistentPreRunE's
			// loadConfig -> config.Load path) - but loadConfig only calls
			// Validate implicitly via config.Load for the file itself. Run
			// it again explicitly here so flag-only configurations (no
			// file) are checked too, and so this command's whole purpose
			// is visible in one place.
			if err := e.cfg.Validate(); err != nil {
				return exitErr(ExitInvalidConfiguration, err)
			}
			if e.cfg.Provider.Type == "gitlab" {
				if _, err := lookupTokenEnv(e.cfg.Provider.GitLab.TokenEnv); err != nil {
					return exitErr(ExitInvalidConfiguration, err)
				}
			}
			cmd.Println("Configuration is valid.")
			return nil
		},
	}
}

func lookupTokenEnv(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("provider.gitlab.token_env is not set")
	}
	if _, ok := os.LookupEnv(name); !ok {
		return "", fmt.Errorf("environment variable %s (named by provider.gitlab.token_env) is not set", name)
	}
	return name, nil
}
