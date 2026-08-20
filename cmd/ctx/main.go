package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"ctx/internal/app"
	"ctx/internal/client"
	"ctx/internal/client/local"
	"ctx/internal/client/remote"
	"ctx/internal/telemetry"
)

var (
	dataDir   string
	serverURL string
	apiKey    string
)

func main() {
	os.Exit(run())
}

// run's defer runs before main calls os.Exit — os.Exit itself skips
// deferred functions, so the telemetry flush has to happen inside here.
func run() int {
	root := &cobra.Command{
		Use:   "ctx",
		Short: "Ctx — reliable, versioned, reproducible context for AI agents",
	}
	root.PersistentFlags().StringVar(&dataDir, "data-dir", "", "path to the local .ctx data directory (default: .ctx, or $CTX_HOME); ignored with --server")
	root.PersistentFlags().StringVar(&serverURL, "server", os.Getenv("CTX_SERVER_URL"), "ctxd server URL (e.g. http://localhost:8080); when unset, runs against the local embedded data directory")
	root.PersistentFlags().StringVar(&apiKey, "api-key", os.Getenv("CTX_API_KEY"), "API key to send to a --server that requires one")

	root.AddCommand(
		newResourceCmd(),
		newGetCmd(),
		newInspectCmd(),
		newHistoryCmd(),
		newTagCmd(),
		newRunCmd(),
		newManifestCmd(),
		newDiffCmd(),
		newReplayCmd(),
		newEvidenceCmd(),
	)

	ctx := context.Background()
	shutdownTelemetry, err := telemetry.Init(ctx, "ctx")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	defer shutdownTelemetry(ctx)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

// openClient returns the local (embedded) client by default, or a remote
// client talking to ctxd when --server / CTX_SERVER_URL is set. Every CLI
// command is written against client.Client, so behavior is identical
// either way.
func openClient() (client.Client, error) {
	if serverURL != "" {
		return remote.New(serverURL, apiKey), nil
	}
	a, err := app.Open(dataDir)
	if err != nil {
		return nil, err
	}
	return local.New(a), nil
}
