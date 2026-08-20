package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newManifestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "manifest <run>",
		Short: "Show a manifest's resolved resource list (by manifest id or run id)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			c, err := openClient()
			if err != nil {
				return err
			}
			defer c.Close()

			man, err := c.GetManifest(context.Background(), target)
			if err != nil {
				return err
			}

			fmt.Printf("Manifest %s (run %s), created %s, %d entr%s\n\n",
				man.ManifestID, man.RunID, man.CreatedAt.Format(time.RFC3339), len(man.Entries), pluralEntries(len(man.Entries)))

			// The REF column only appears when something in this manifest
			// was actually mounted by tag — no empty column otherwise.
			showRef := false
			for _, e := range man.Entries {
				if e.Ref != "" {
					showRef = true
					break
				}
			}

			w := newTabWriter()
			if showRef {
				fmt.Fprintln(w, "POS\tURI\tREF\tSNAPSHOT\tCONTENT_HASH")
			} else {
				fmt.Fprintln(w, "POS\tURI\tSNAPSHOT\tCONTENT_HASH")
			}
			for _, e := range man.Entries {
				if showRef {
					ref := e.Ref
					if ref == "" {
						ref = "-"
					}
					fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", e.Position, e.URI, ref, e.SnapshotID, e.ContentHash)
					continue
				}
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", e.Position, e.URI, e.SnapshotID, e.ContentHash)
			}
			return w.Flush()
		},
	}
}
