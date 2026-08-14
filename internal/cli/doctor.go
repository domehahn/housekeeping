package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/domehahn/housekeeping/internal/output"
)

// newDoctorCmd runs a sequence of read-only checks and prints a
// pass/fail summary. It never performs a destructive operation.
func newDoctorCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose configuration, connectivity, authentication, and permissions",
		RunE: func(cmd *cobra.Command, args []string) error {
			rows := [][]string{}
			add := func(check, status string) { rows = append(rows, []string{check, status}) }

			if err := e.cfg.Validate(); err != nil {
				add("Configuration", "FAILED: "+err.Error())
				return output.Render(cmd.OutOrStdout(), e.format, output.Table{Headers: []string{"Check", "Status"}, Rows: rows}, rows)
			}
			add("Configuration", "OK")
			add("Provider", e.cfg.Provider.Type)

			client, err := e.requireClient()
			if err != nil {
				add("Authentication", "FAILED: "+err.Error())
				return output.Render(cmd.OutOrStdout(), e.format, output.Table{Headers: []string{"Check", "Status"}, Rows: rows}, rows)
			}

			ctx := cmd.Context()
			info, err := client.Info(ctx)
			if err != nil {
				add("API connectivity", "FAILED: "+err.Error())
				return output.Render(cmd.OutOrStdout(), e.format, output.Table{Headers: []string{"Check", "Status"}, Rows: rows}, rows)
			}
			add("API connectivity", "OK")
			add("Authentication", fmt.Sprintf("OK (as %s)", info.AuthenticatedAs))

			if e.cfg.Scope.Group != "" {
				scope, _, err := client.ResolveGroupScope(ctx, e.cfg.Scope.Group, e.cfg.Scope.Recursive)
				if err != nil {
					add("Group", "NOT FOUND: "+err.Error())
				} else {
					add("Group", fmt.Sprintf("FOUND (%s)", scope.Path))
					if _, err := client.ListProjects(ctx, scope); err != nil {
						add("Permissions (projects)", "FAILED: "+err.Error())
					} else {
						add("Permissions (projects)", "OK")
					}
					if _, err := client.ListGroupMembers(ctx, scope); err != nil {
						add("Permissions (members)", "FAILED: "+err.Error())
					} else {
						add("Permissions (members)", "OK")
					}
				}
			} else {
				add("Group", "SKIPPED (no --group / scope.group configured)")
			}

			caps, err := client.Capabilities(ctx)
			if err == nil {
				add("Capabilities", fmt.Sprintf("last-login=%s last-activity=%s remove-member=%s delete-project=%s",
					caps.UserLastLogin, caps.UserLastActivity, caps.RemoveGroupMember, caps.DeleteProjects))
			}

			table := output.Table{Headers: []string{"Check", "Status"}, Rows: rows}
			return output.Render(cmd.OutOrStdout(), e.format, table, rows)
		},
	}
}
