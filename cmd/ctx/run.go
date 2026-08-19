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
			a, err := openApp()
			if err != nil {
				return err
			}
			defer a.Close()

			ctx := context.Background()
			if err := a.RunBuilder.Start(ctx, runID); err != nil {
				return err
			}
			fmt.Printf("Started run %s\n", runID)

			for i, uri := range args {
				result, err := a.RunBuilder.Mount(ctx, runID, uri)
				if err != nil {
					return err
				}
				fmt.Printf("Mounted %s -> snapshot %s (position %d)\n", uri, result.Snapshot.SnapshotID, i)
			}

			man, err := a.RunBuilder.Commit(ctx, runID)
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
			a, err := openApp()
			if err != nil {
				return err
			}
			defer a.Close()
			if err := a.RunBuilder.Start(context.Background(), args[0]); err != nil {
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
			a, err := openApp()
			if err != nil {
				return err
			}
			defer a.Close()

			ctx := context.Background()
			mounts, err := a.Runs.ListMounts(ctx, runID)
			if err != nil {
				return err
			}
			position := len(mounts)

			result, err := a.RunBuilder.Mount(ctx, runID, uri)
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
			a, err := openApp()
			if err != nil {
				return err
			}
			defer a.Close()

			man, err := a.RunBuilder.Commit(context.Background(), runID)
			if err != nil {
				return err
			}
			fmt.Printf("Committed manifest %s for run %s (%d entr%s)\n", man.ManifestID, runID, len(man.Entries), pluralEntries(len(man.Entries)))
			return nil
		},
	}
}
