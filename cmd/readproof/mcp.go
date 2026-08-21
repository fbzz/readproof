package main

import (
	"context"
	"log/slog"
	"os"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	ctxmcp "github.com/fbzz/readproof/internal/mcp"
)

func newMCPCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve Readproof to an AI agent over the Model Context Protocol (stdio)",
		Long: "Serve Readproof to an AI agent over the Model Context Protocol.\n\n" +
			"Speaks MCP on stdin/stdout, so it is launched by the MCP client (Claude\n" +
			"Code, Claude Desktop, Cursor, …) rather than run by hand. Registered\n" +
			"resources are exposed as readable readproof:// resources; resolve, tags, runs,\n" +
			"manifests, diff, replay, and evidence export are exposed as tools.\n\n" +
			"Like every other command it honors --data-dir (embedded) and --server /\n" +
			"--api-key (against a running readproofd), so the same MCP surface works over a\n" +
			"local data directory or a shared deployment. See docs/mcp.md for client\n" +
			"configuration snippets.",
		Args: cobra.NoArgs,
		// Cobra prints usage on a RunE error; for a server that failed to
		// start, the error alone is the useful output.
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := openClient()
			if err != nil {
				return err
			}
			defer c.Close()

			// stdout is the JSON-RPC channel: a single stray byte written
			// there corrupts the session, so every diagnostic goes to
			// stderr, and logging stays off unless asked for.
			level := slog.LevelError
			if verbose {
				level = slog.LevelInfo
			}
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

			srv := ctxmcp.NewServer(c, ctxmcp.Options{Logger: logger})
			return srv.Run(context.Background(), &mcpsdk.StdioTransport{})
		},
	}
	cmd.Flags().BoolVar(&verbose, "verbose", false, "log MCP session activity to stderr")
	return cmd
}
