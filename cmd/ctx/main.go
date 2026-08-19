package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"ctx/internal/app"
)

var dataDir string

func main() {
	root := &cobra.Command{
		Use:   "ctx",
		Short: "Ctx — reliable, versioned, reproducible context for AI agents",
	}
	root.PersistentFlags().StringVar(&dataDir, "data-dir", "", "path to the local .ctx data directory (default: .ctx, or $CTX_HOME)")

	root.AddCommand(
		newResourceCmd(),
		newGetCmd(),
		newInspectCmd(),
		newHistoryCmd(),
		newRunCmd(),
		newManifestCmd(),
		newDiffCmd(),
		newReplayCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func openApp() (*app.App, error) {
	return app.Open(dataDir)
}
