package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	var runID string
	cmd := &cobra.Command{
		Use:   "run [uris...]",
		Short: "Start, mount, and commit a context run in one shot (requires --id)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runID == "" {
				return fmt.Errorf("--id is required")
			}
			if len(args) == 0 {
				return fmt.Errorf("at least one <uri> is required")
			}
			c, err := openClient()
			if err != nil {
				return err
			}
			defer c.Close()

			ctx := context.Background()
			if err := c.RunStart(ctx, runID); err != nil {
				return err
			}
			fmt.Printf("Started run %s\n", runID)

			for _, uri := range args {
				result, position, err := c.RunMount(ctx, runID, uri)
				if err != nil {
					return err
				}
				fmt.Printf("Mounted %s -> snapshot %s (position %d)\n", uri, result.Snapshot.SnapshotID, position)
			}

			man, err := c.RunCommit(ctx, runID)
			if err != nil {
				return err
			}
			fmt.Printf("Committed manifest %s for run %s (%d entr%s)\n", man.ManifestID, runID, len(man.Entries), pluralEntries(len(man.Entries)))
			return nil
		},
	}
	cmd.Flags().StringVar(&runID, "id", "", "run id (required)")

	cmd.AddCommand(newRunStartCmd(), newRunMountCmd(), newRunCommitCmd())
	return cmd
}

func newRunStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <run-id>",
		Short: "Start a new context run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := openClient()
			if err != nil {
				return err
			}
			defer c.Close()
			if err := c.RunStart(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Printf("Started run %s\n", args[0])
			return nil
		},
	}
}

func newRunMountCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mount <run-id> <uri>",
		Short: "Resolve a resource and mount it into an open run",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID, uri := args[0], args[1]
			c, err := openClient()
			if err != nil {
				return err
			}
			defer c.Close()

			result, position, err := c.RunMount(context.Background(), runID, uri)
			if err != nil {
				return err
			}
			fmt.Printf("Mounted %s -> snapshot %s (position %d)\n", uri, result.Snapshot.SnapshotID, position)
			return nil
		},
	}
}

func newRunCommitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "commit <run-id>",
		Short: "Commit an open run into an immutable manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID := args[0]
			c, err := openClient()
			if err != nil {
				return err
			}
			defer c.Close()

			man, err := c.RunCommit(context.Background(), runID)
			if err != nil {
				return err
			}
			fmt.Printf("Committed manifest %s for run %s (%d entr%s)\n", man.ManifestID, runID, len(man.Entries), pluralEntries(len(man.Entries)))
			return nil
		},
	}
}
