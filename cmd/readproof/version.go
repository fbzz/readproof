package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fbzz/readproof/internal/version"
)

// newVersionCmd is the subcommand form of `readproof --version` (cobra
// generates the flag from root.Version). Both print the same line, because
// a bug report quoting either has to identify the same build.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the readproof version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			// Writes to cmd.OutOrStdout(), not os.Stdout, so tests can
			// capture it.
			fmt.Fprintf(cmd.OutOrStdout(), "readproof %s\n", version.String())
		},
	}
}
