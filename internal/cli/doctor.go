package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/domehahn/housekeeping/internal/output"
)

// newDoctorCmd runs a sequence of read-only checks and prints a
// pass/fail summary. It never performs a destructive operation.
func newDoctorCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose configuration, connectivity, authentication, permissions, and safety readiness",
		RunE: func(cmd *cobra.Command, args []string) error {
			rows := [][]string{}
			hasError := false
			add := func(check, status string) { rows = append(rows, []string{check, status}) }

			if err := e.cfg.Validate(); err != nil {
				hasError = true
				add("Configuration", "FAILED: "+err.Error())
				_ = output.Render(cmd.OutOrStdout(), e.format, output.Table{Headers: []string{"Check", "Status"}, Rows: rows}, rows)
				return exitErr(ExitInvalidConfiguration, fmt.Errorf("configuration validation failed"))
			}
			add("Configuration", "OK")
			add("Config Digest", e.cfg.ComputeConfigDigest())
			add("Provider", e.cfg.Provider.Type)

			// TLS Mode Check
			baseURL := strings.ToLower(e.cfg.Provider.GitLab.BaseURL)
			if strings.HasPrefix(baseURL, "https://") {
				add("TLS Security", "OK (HTTPS enforced)")
			} else if strings.HasPrefix(baseURL, "http://localhost") || strings.HasPrefix(baseURL, "http://127.0.0.1") {
				add("TLS Security", "WARNING (HTTP used for local endpoint)")
			} else if baseURL != "" {
				add("TLS Security", "FAILED (insecure plain HTTP configured for remote instance)")
				hasError = true
			} else {
				add("TLS Security", "SKIPPED (no base URL)")
			}

			// Audit Sink Check
			auditStatus := []string{}
			if e.cfg.Audit.RequiredForApply {
				auditStatus = append(auditStatus, "required_for_apply=true")
			} else {
				auditStatus = append(auditStatus, "required_for_apply=false")
			}
			if e.cfg.Audit.Path != "" || e.flags.auditLogPath != "" {
				path := e.flags.auditLogPath
				if path == "" {
					path = e.cfg.Audit.Path
				}
				auditStatus = append(auditStatus, "path="+path)
			} else {
				auditStatus = append(auditStatus, "path=stdout/unset")
			}
			add("Audit Sink", strings.Join(auditStatus, ", "))

			// Safety Limits Check
			add("Safety Limits", fmt.Sprintf("MaxActions[P:%d, U:%d, Pipe:%d, Run:%d], MaxPercentage[P:%d%%, U:%d%%, Pipe:%d%%, Run:%d%%]",
				e.cfg.Safety.MaxActions.Projects, e.cfg.Safety.MaxActions.Users, e.cfg.Safety.MaxActions.PipelineTags, e.cfg.Safety.MaxActions.RunnerTags,
				e.cfg.Safety.MaxPercentage.Projects, e.cfg.Safety.MaxPercentage.Users, e.cfg.Safety.MaxPercentage.PipelineTags, e.cfg.Safety.MaxPercentage.RunnerTags))

			client, err := e.requireClient()
			if err != nil {
				add("Authentication", "FAILED: "+err.Error())
				_ = output.Render(cmd.OutOrStdout(), e.format, output.Table{Headers: []string{"Check", "Status"}, Rows: rows}, rows)
				return exitErr(ExitInvalidConfiguration, fmt.Errorf("authentication check failed"))
			}

			ctx := cmd.Context()
			info, err := client.Info(ctx)
			if err != nil {
				add("API connectivity", "FAILED: "+err.Error())
				_ = output.Render(cmd.OutOrStdout(), e.format, output.Table{Headers: []string{"Check", "Status"}, Rows: rows}, rows)
				return exitErr(ExitGeneralError, fmt.Errorf("API connectivity failed"))
			}
			add("API connectivity", "OK")
			add("Authentication", fmt.Sprintf("OK (as %s)", info.AuthenticatedAs))

			if e.cfg.Scope.Group != "" {
				scope, _, err := client.ResolveGroupScope(ctx, e.cfg.Scope.Group, e.cfg.Scope.Recursive)
				if err != nil {
					hasError = true
					add("Group", "NOT FOUND: "+err.Error())
				} else {
					add("Group", fmt.Sprintf("FOUND (%s)", scope.Path))
					if _, err := client.ListProjects(ctx, scope); err != nil {
						hasError = true
						add("Permissions (projects)", "FAILED: "+err.Error())
					} else {
						add("Permissions (projects)", "OK")
					}
					if _, err := client.ListGroupMembers(ctx, scope); err != nil {
						hasError = true
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
			if err := output.Render(cmd.OutOrStdout(), e.format, table, rows); err != nil {
				return err
			}
			if hasError {
				return exitErr(ExitGeneralError, fmt.Errorf("doctor detected configuration or permission issues"))
			}
			return nil
		},
	}
}
