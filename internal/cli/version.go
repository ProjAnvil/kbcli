package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newVersionCmd builds "kbcli version".
func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the kbcli version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "kbcli %s\n", version)
			return nil
		},
	}
}
