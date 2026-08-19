package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"ctx/internal/source"
)

func newInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <uri>",
		Short: "Show detailed status for a context resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			uri := args[0]
			a, err := openApp()
			if err != nil {
				return err
			}
			defer a.Close()

			ctx := context.Background()
			res, err := a.Resources.Get(ctx, uri)
			if err != nil {
				return err
			}
			history, err := a.Snapshots.ListByResource(ctx, uri)
			if err != nil {
				return err
			}

			fmt.Printf("Resource:  %s\n", res.URI)
			fmt.Printf("Namespace: %s\n", res.Namespace)
			fmt.Printf("Path:      %s\n\n", res.Path)

			fmt.Println("Source:")
			fmt.Printf("  type: %s\n", res.SourceConfig.Kind)
			switch res.SourceConfig.Kind {
			case source.KindFilesystem:
				fmt.Printf("  path: %s\n", res.SourceConfig.Filesystem.Path)
			case source.KindGitHub:
				gh := res.SourceConfig.GitHub
				fmt.Printf("  repo: %s/%s\n", gh.Owner, gh.Repo)
				fmt.Printf("  path: %s\n", gh.Path)
				fmt.Printf("  ref:  %s\n", gh.Ref)
			case source.KindHTTP:
				fmt.Printf("  url: %s\n", res.SourceConfig.HTTP.URL)
			}
			fmt.Println()

			fmt.Println("Policy:")
			fmt.Printf("  strategy: %s\n", res.Policy.Strategy)
			if res.Policy.MaxAge > 0 {
				fmt.Printf("  max_age:  %s\n", res.Policy.MaxAge)
			} else {
				fmt.Printf("  max_age:  n/a\n")
			}
			fmt.Println()

			if res.CurrentSnapshotID == "" {
				fmt.Println("Current snapshot: none (never resolved — run `ctx get` first)")
			} else {
				snap, err := a.Snapshots.Get(ctx, res.CurrentSnapshotID)
				if err != nil {
					return err
				}
				fmt.Println("Current snapshot:")
				fmt.Printf("  snapshot_id:     %s\n", snap.SnapshotID)
				fmt.Printf("  observed_at:     %s\n", snap.ObservedAt.Format(time.RFC3339))
				fmt.Printf("  source_revision: %s\n", snap.SourceRevision)
				fmt.Printf("  content_hash:    %s\n", snap.ContentHash)
				fmt.Printf("  bytes:           %d\n", snap.Bytes)
			}
			fmt.Println()
			fmt.Printf("Snapshot history: %d snapshot(s) (see `ctx history %s`)\n", len(history), uri)
			return nil
		},
	}
}
