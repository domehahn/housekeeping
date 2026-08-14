package cli

import (
	"github.com/spf13/cobra"

	"github.com/domehahn/housekeeping/internal/output"
)

func newProviderCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider",
		Short: "Inspect the configured provider",
	}
	cmd.AddCommand(newProviderInfoCmd(e), newProviderCapabilitiesCmd(e))
	return cmd
}

func newProviderInfoCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show provider, instance, and authenticated identity",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := e.requireClient()
			if err != nil {
				return err
			}
			info, err := client.Info(cmd.Context())
			if err != nil {
				return wrapProviderErr(err)
			}

			table := output.Table{
				Headers: []string{"Field", "Value"},
				Rows: [][]string{
					{"Provider", info.Provider},
					{"Instance", info.Instance},
					{"Server Version", orDash(info.ServerVersion)},
					{"Authenticated As", info.AuthenticatedAs},
					{"Is Admin", boolStr(info.IsAdmin)},
				},
			}
			return output.Render(cmd.OutOrStdout(), e.format, table, info)
		},
	}
}

func newProviderCapabilitiesCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "capabilities",
		Short: "Show which operations are available with the current credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := e.requireClient()
			if err != nil {
				return err
			}
			caps, err := client.Capabilities(cmd.Context())
			if err != nil {
				return wrapProviderErr(err)
			}

			table := output.Table{
				Headers: []string{"Capability", "Supported"},
				Rows: [][]string{
					{"List Projects", string(caps.ListProjects)},
					{"Delete Projects", string(caps.DeleteProjects)},
					{"Archive Projects", string(caps.ArchiveProjects)},
					{"List Groups", string(caps.ListGroups)},
					{"List Group Members", string(caps.ListGroupMembers)},
					{"Remove Group Member", string(caps.RemoveGroupMember)},
					{"Block Users", string(caps.BlockUsers)},
					{"Delete Users", string(caps.DeleteUsers)},
					{"User Last Login", string(caps.UserLastLogin)},
					{"User Last Activity", string(caps.UserLastActivity)},
					{"Project Last Activity", string(caps.ProjectLastActivity)},
					{"Billable Group Members", string(caps.BillableMembers)},
					{"Cross-Instance User Memberships", string(caps.UserMemberships)},
				},
			}
			return output.Render(cmd.OutOrStdout(), e.format, table, caps)
		},
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
