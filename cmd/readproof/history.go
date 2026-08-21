package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history <uri>",
		Short: "Show snapshot history for a context resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			uri := args[0]
			c, err := openClient()
			if err != nil {
				return err
			}
			defer c.Close()

			ctx := context.Background()
			history, err := c.History(ctx, uri)
			if err != nil {
				return err
			}
			tags, err := c.ListTags(ctx, uri)
			if err != nil {
				return err
			}
			// A snapshot can carry several tags; ListTags is already sorted
			// by name, so the joined column is stable output.
			tagsBySnapshot := make(map[string][]string, len(tags))
			for _, t := range tags {
				tagsBySnapshot[t.SnapshotID] = append(tagsBySnapshot[t.SnapshotID], t.Name)
			}

			w := newTabWriter()
			fmt.Fprintln(w, "SNAPSHOT\tOBSERVED\tREVISION\tTAGS")
			for _, s := range history {
				names := "-"
				if n := tagsBySnapshot[s.SnapshotID]; len(n) > 0 {
					names = strings.Join(n, ",")
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.SnapshotID, s.ObservedAt.Format(time.RFC3339), s.SourceRevision, names)
			}
			return w.Flush()
		},
	}
}
