package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newTagCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tag",
		Short: "Manage named, movable pointers to a resource's snapshots",
	}
	cmd.AddCommand(newTagSetCmd(), newTagListCmd(), newTagRemoveCmd())
	return cmd
}

func newTagSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <uri> <tag> <snapshot-id>",
		Short: "Point a tag at a snapshot (creating it, or moving an existing tag)",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			uri, name, snapshotID := args[0], args[1], args[2]
			c, err := openClient()
			if err != nil {
				return err
			}
			defer c.Close()

			t, err := c.SetTag(context.Background(), uri, name, snapshotID)
			if err != nil {
				return err
			}
			fmt.Printf("Tagged %s@%s -> %s\n", t.ResourceURI, t.Name, t.SnapshotID)
			fmt.Printf("  resolve it with: readproof get %s@%s\n", t.ResourceURI, t.Name)
			return nil
		},
	}
}

func newTagListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <uri>",
		Short: "List a resource's tags",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := openClient()
			if err != nil {
				return err
			}
			defer c.Close()

			tags, err := c.ListTags(context.Background(), args[0])
			if err != nil {
				return err
			}
			w := newTabWriter()
			fmt.Fprintln(w, "TAG\tSNAPSHOT\tUPDATED")
			for _, t := range tags {
				fmt.Fprintf(w, "%s\t%s\t%s\n", t.Name, t.SnapshotID, t.UpdatedAt.Format(time.RFC3339))
			}
			return w.Flush()
		},
	}
}

func newTagRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <uri> <tag>",
		Short: "Delete a tag (the snapshot it pointed at is untouched)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			uri, name := args[0], args[1]
			c, err := openClient()
			if err != nil {
				return err
			}
			defer c.Close()

			if err := c.DeleteTag(context.Background(), uri, name); err != nil {
				return err
			}
			fmt.Printf("Deleted tag %s@%s\n", uri, name)
			return nil
		},
	}
}
