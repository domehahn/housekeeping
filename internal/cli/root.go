// Package cli wires cobra commands to the application layer. It is
// responsible for flag parsing, configuration resolution, output
// rendering, and exit codes - never for business logic, which lives in
// internal/app and internal/policy.
package cli

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

// Execute builds and runs the root command, returning the process exit
// code. It is the sole entry point called from cmd/scm-cleaner/main.go.
func Execute(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root, e := newRootCmd()
	root.SetArgs(args)

	if err := root.ExecuteContext(ctx); err != nil {
		return handleErr(e, root, err)
	}
	return ExitSuccess
}

func handleErr(e *env, cmd *cobra.Command, err error) int {
	var ee *ExitError
	if errors.As(err, &ee) {
		cmd.PrintErrln("Error:", ee.Error())
		return ee.Code
	}
	cmd.PrintErrln("Error:", err.Error())
	return ExitGeneralError
}

func newRootCmd() (*cobra.Command, *env) {
	e := &env{}
	flags := &e.flags

	root := &cobra.Command{
		Use:           "scm-cleaner",
		Short:         "Policy-driven SCM housekeeping and GitLab CI runner-tag management",
		Long:          rootLongDescription,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			e.ctx = cmd.Context()
			return e.loadConfig()
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&flags.configPath, "config", defaultConfigPath(), "path to YAML configuration file")
	pf.StringVar(&flags.output, "output", "", "output format: table|json|yaml (default table)")
	pf.StringVar(&flags.group, "group", "", "GitLab group path to operate on")
	pf.BoolVar(&flags.recursive, "recursive", false, "include subgroups")
	pf.StringVar(&flags.gitlabURL, "gitlab-url", "", "GitLab base URL, overrides config")
	pf.StringVar(&flags.tokenEnv, "token-env", "", "environment variable holding the GitLab token, overrides config")
	pf.BoolVar(&flags.insecure, "insecure-skip-tls-verify", false, "DANGEROUS: disable TLS certificate verification")
	pf.IntVar(&flags.workers, "workers", 0, "bounded concurrency for read operations, overrides config")
	pf.StringVar(&flags.auditLogPath, "audit-log", "", "append destructive-operation records to this JSON Lines file")

	root.AddCommand(
		newVersionCmd(),
		newProviderCmd(e),
		newProjectsCmd(e),
		newUsersCmd(e),
		newPipelinesCmd(e),
		newRunnersCmd(e),
		newExecuteCmd(e),
		newConfigCmd(e),
		newDoctorCmd(e),
	)
	return root, e
}

func defaultConfigPath() string {
	if _, err := os.Stat("scm-cleaner.yaml"); err == nil {
		return "scm-cleaner.yaml"
	}
	return ""
}

const rootLongDescription = `scm-cleaner discovers, evaluates, plans, and (only when explicitly
approved) executes cleanup of stale projects/users and CI runner-tag rollouts
on source-code-management platforms.

Currently implemented: GitLab (self-managed and GitLab.com).

Destructive operations always require an explicit, reviewable plan and an
explicit --apply flag. Dry run is the default everywhere.`
