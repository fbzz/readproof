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
	"ctx/internal/version"
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
	root := newRootCmd()

	ctx := context.Background()
	shutdownTelemetry, err := telemetry.Init(ctx, "ctx")
	if err != nil {
		fmt.Fprintln(os.Stderr, "ctx:", err)
		return 1
	}
	defer shutdownTelemetry(ctx)

	if err := root.Execute(); err != nil {
		// Cobra is silenced (see newRootCmd), so this is the one place an
		// error reaches the user.
		fmt.Fprintln(os.Stderr, "ctx:", err)
		return 1
	}
	return 0
}

// newRootCmd builds the command tree. Split out of run() so tests can drive
// the real root command — including its error/usage behavior — without
// going through os.Exit.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "ctx",
		Short:   "Ctx — reliable, versioned, reproducible context for AI agents",
		Version: version.String(),

		// A failed replay, an unknown tag, or an unreachable server is a
		// runtime failure, not CLI misuse: dumping the full usage block
		// there buries the one line that matters. SilenceUsage is set in
		// PersistentPreRun instead of here because cobra runs that hook
		// *after* flag parsing and argument validation — so genuine misuse
		// (unknown flag, missing argument) fails before the hook and still
		// prints usage.
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			cmd.SilenceUsage = true
		},
	}
	root.SetVersionTemplate("ctx {{.Version}}\n")
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
		newMCPCmd(),
		newVersionCmd(),
	)
	return root
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
