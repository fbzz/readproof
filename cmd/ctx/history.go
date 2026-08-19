package main

import (
	"context"
	"fmt"
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
			a, err := openApp()
			if err != nil {
				return err
			}
			defer a.Close()

			history, err := a.Snapshots.ListByResource(context.Background(), uri)
			if err != nil {
				return err
			}
			w := newTabWriter()
			fmt.Fprintln(w, "SNAPSHOT\tOBSERVED\tREVISION")
			for _, s := range history {
				fmt.Fprintf(w, "%s\t%s\t%s\n", s.SnapshotID, s.ObservedAt.Format(time.RFC3339), s.SourceRevision)
			}
			return w.Flush()
		},
	}
}
