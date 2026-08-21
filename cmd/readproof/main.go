package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/fbzz/readproof/internal/app"
	"github.com/fbzz/readproof/internal/client"
	"github.com/fbzz/readproof/internal/client/local"
	"github.com/fbzz/readproof/internal/client/remote"
	"github.com/fbzz/readproof/internal/telemetry"
	"github.com/fbzz/readproof/internal/version"
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
	shutdownTelemetry, err := telemetry.Init(ctx, "readproof")
	if err != nil {
		fmt.Fprintln(os.Stderr, "readproof:", err)
		return 1
	}
	defer shutdownTelemetry(ctx)

	if err := root.Execute(); err != nil {
		// Cobra is silenced (see newRootCmd), so this is the one place an
		// error reaches the user.
		fmt.Fprintln(os.Stderr, "readproof:", err)
		return 1
	}
	return 0
}

// newRootCmd builds the command tree. Split out of run() so tests can drive
// the real root command — including its error/usage behavior — without
// going through os.Exit.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "readproof",
		Short:   "Readproof — reliable, versioned, reproducible context for AI agents",
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
			warnIfAPIKeyOnArgv(cmd)
		},
	}
	root.SetVersionTemplate("readproof {{.Version}}\n")
	root.PersistentFlags().StringVar(&dataDir, "data-dir", "", "path to the local .readproof data directory (default: .readproof, or $READPROOF_HOME); ignored with --server")
	root.PersistentFlags().StringVar(&serverURL, "server", os.Getenv("READPROOF_SERVER_URL"), "readproofd server URL (e.g. http://localhost:8080); when unset, runs against the local embedded data directory")
	root.PersistentFlags().StringVar(&apiKey, "api-key", os.Getenv("READPROOF_API_KEY"), "API key to send to a --server that requires one (prefer READPROOF_API_KEY: a flag is visible in `ps`)")

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

// warnIfAPIKeyOnArgv says so, once per invocation, when the key was typed on
// the command line rather than read from READPROOF_API_KEY. argv is visible to
// every user on the host through `ps`, and this is the value that authenticates
// to readproofd. Cobra's Changed is exactly the right test: it is false when
// the value came from the flag's environment-derived default.
func warnIfAPIKeyOnArgv(cmd *cobra.Command) {
	if flag := cmd.Flags().Lookup("api-key"); flag != nil && flag.Changed && apiKey != "" {
		fmt.Fprintln(os.Stderr, "readproof: warning: --api-key on the command line is visible to every user on this host via `ps`; prefer the READPROOF_API_KEY environment variable")
	}
}

// openClient returns the local (embedded) client by default, or a remote
// client talking to readproofd when --server / READPROOF_SERVER_URL is set.
// Every CLI command is written against client.Client, so behavior is
// identical either way.
//
// Embedded, the App is opened with the default (unrestricted) source policy:
// a filesystem source may name any path, a "${VAR}" header any variable, an
// http source any address. That is deliberate — the files, the environment
// and the person typing the command are the same trust domain here, and a
// restriction would only stop a user reading their own documents. With
// --server none of this applies: the fetch happens on the server, so the
// server's policy (--filesystem-root, --header-env-allow,
// --allow-private-sources) is what governs. See SECURITY.md.
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
