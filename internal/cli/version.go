package cli

import (
	"runtime"

	"github.com/spf13/cobra"

	"github.com/domehahn/housekeeping/internal/output"
	"github.com/domehahn/housekeeping/pkg/version"
)

func newVersionCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			info := version.Get()
			info.GoVersion = runtime.Version()

			f, err := output.ParseFormat(format)
			if err != nil {
				return exitErr(ExitInvalidConfiguration, err)
			}
			if f == output.FormatTable {
				cmd.Println(info.String())
				return nil
			}
			return output.Render(cmd.OutOrStdout(), f, output.Table{}, info)
		},
	}
	cmd.Flags().StringVar(&format, "output", "table", "output format: table|json|yaml")
	return cmd
}
