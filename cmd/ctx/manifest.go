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

			w := newTabWriter()
			fmt.Fprintln(w, "POS\tURI\tSNAPSHOT\tCONTENT_HASH")
			for _, e := range man.Entries {
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", e.Position, e.URI, e.SnapshotID, e.ContentHash)
			}
			return w.Flush()
		},
	}
}
