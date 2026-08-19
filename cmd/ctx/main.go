package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"ctx/internal/app"
	"ctx/internal/client"
	"ctx/internal/client/local"
	"ctx/internal/client/remote"
)

var (
	dataDir   string
	serverURL string
)

func main() {
	root := &cobra.Command{
		Use:   "ctx",
		Short: "Ctx — reliable, versioned, reproducible context for AI agents",
	}
	root.PersistentFlags().StringVar(&dataDir, "data-dir", "", "path to the local .ctx data directory (default: .ctx, or $CTX_HOME); ignored with --server")
	root.PersistentFlags().StringVar(&serverURL, "server", os.Getenv("CTX_SERVER_URL"), "ctxd server URL (e.g. http://localhost:8080); when unset, runs against the local embedded data directory")

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

// openClient returns the local (embedded) client by default, or a remote
// client talking to ctxd when --server / CTX_SERVER_URL is set. Every CLI
// command is written against client.Client, so behavior is identical
// either way.
func openClient() (client.Client, error) {
	if serverURL != "" {
		return remote.New(serverURL), nil
	}
	a, err := app.Open(dataDir)
	if err != nil {
		return nil, err
	}
	return local.New(a), nil
}
