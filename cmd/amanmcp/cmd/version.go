package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Aman-CERP/amanmcp/pkg/version"
)

// newVersionCmd creates the version command.
func newVersionCmd() *cobra.Command {
	var jsonOutput bool
	var shortOutput bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long:  `Print version information including git commit, build date, and Go version.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Short output takes precedence
			if shortOutput {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), version.Short())
				return err
			}

			// JSON output
			if jsonOutput {
				info := version.GetInfo()
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(info)
			}

			// Default: full string output
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version.String())
			return err
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output version info as JSON")
	cmd.Flags().BoolVar(&shortOutput, "short", false, "Output only the version number")

	return cmd
}
