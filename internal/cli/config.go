package cli

import (
	"fmt"

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
				ref, err := e.cfg.Provider.GitLab.SecretReference()
				if err != nil {
					return exitErr(ExitInvalidConfiguration, err)
				}
				if e.secretResolver == nil {
					return exitErr(ExitInvalidConfiguration, fmt.Errorf("secret resolver is not configured"))
				}
				if _, err := e.secretResolver.Resolve(cmd.Context(), ref); err != nil {
					return exitErr(ExitInvalidConfiguration, err)
				}
			}
			cmd.Println("Configuration is valid.")
			return nil
		},
	}
}
