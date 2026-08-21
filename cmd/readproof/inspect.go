package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/fbzz/readproof/internal/redact"
	"github.com/fbzz/readproof/internal/resource"
	"github.com/fbzz/readproof/internal/source"
	"github.com/fbzz/readproof/internal/tag"
)

func newInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <uri>[@<tag>]",
		Short: "Show detailed status for a context resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			uri, ref, err := resource.SplitRef(args[0])
			if err != nil {
				return err
			}
			c, err := openClient()
			if err != nil {
				return err
			}
			defer c.Close()

			ctx := context.Background()
			res, err := c.GetResource(ctx, uri)
			if err != nil {
				return err
			}
			history, err := c.History(ctx, uri)
			if err != nil {
				return err
			}
			tags, err := c.ListTags(ctx, uri)
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
				// Applied even in embedded mode where this value was never
				// sent over the wire: sensitive header values should never
				// be echoed by `readproof inspect`, regardless of client mode.
				headers := redact.Headers(res.SourceConfig.HTTP.Headers)
				if len(headers) > 0 {
					fmt.Println("  headers:")
					keys := make([]string, 0, len(headers))
					for k := range headers {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					for _, k := range keys {
						fmt.Printf("    %s: %s\n", k, headers[k])
					}
				}
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

			if len(tags) == 0 {
				fmt.Println("Tags: none")
			} else {
				fmt.Println("Tags:")
				for _, t := range tags {
					fmt.Printf("  %s -> %s (updated %s)\n", t.Name, t.SnapshotID, t.UpdatedAt.Format(time.RFC3339))
				}
			}
			fmt.Println()

			// An explicit @<tag> makes inspect report what that tag would
			// deliver, which is not necessarily the current snapshot.
			if ref != "" {
				var tagged *tag.Tag
				for i := range tags {
					if tags[i].Name == ref {
						tagged = &tags[i]
						break
					}
				}
				if tagged == nil {
					return fmt.Errorf("%s has no tag %q", uri, ref)
				}
				snap, err := c.GetSnapshot(ctx, tagged.SnapshotID)
				if err != nil {
					return err
				}
				fmt.Printf("Tag @%s resolves to:\n", ref)
				fmt.Printf("  snapshot_id:     %s\n", snap.SnapshotID)
				fmt.Printf("  observed_at:     %s\n", snap.ObservedAt.Format(time.RFC3339))
				fmt.Printf("  source_revision: %s\n", snap.SourceRevision)
				fmt.Printf("  content_hash:    %s\n", snap.ContentHash)
				fmt.Printf("  bytes:           %d\n", snap.Bytes)
				fmt.Println()
			}

			if res.CurrentSnapshotID == "" {
				fmt.Println("Current snapshot: none (never resolved — run `readproof get` first)")
			} else {
				snap, err := c.GetSnapshot(ctx, res.CurrentSnapshotID)
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
			fmt.Printf("Snapshot history: %d snapshot(s) (see `readproof history %s`)\n", len(history), uri)
			return nil
		},
	}
}
