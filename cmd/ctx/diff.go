package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"ctx/internal/diff"
)

func newDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff <target-a> <target-b>",
		Short: "Diff the resolved context between two manifests or runs",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetA, targetB := args[0], args[1]
			c, err := openClient()
			if err != nil {
				return err
			}
			defer c.Close()

			result, err := c.Diff(context.Background(), targetA, targetB)
			if err != nil {
				return err
			}
			printDiff(targetA, targetB, result)
			return nil
		},
	}
}

func printDiff(targetA, targetB string, result diff.Result) {
	fmt.Printf("--- %s (%s)\n", targetA, result.ManifestA.ManifestID)
	fmt.Printf("+++ %s (%s)\n\n", targetB, result.ManifestB.ManifestID)

	for _, e := range result.Entries {
		switch e.Status {
		case diff.StatusRemoved:
			fmt.Printf("- %s (removed, was %s)\n\n", e.URI, e.SnapshotIDA)
		case diff.StatusAdded:
			fmt.Printf("+ %s (added, %s)\n\n", e.URI, e.SnapshotIDB)
		case diff.StatusChanged:
			fmt.Printf("~ %s  (%s -> %s)\n", e.URI, e.SnapshotIDA, e.SnapshotIDB)
			fmt.Print(indent(e.UnifiedDiff, "  "))
			fmt.Println()
		}
	}

	changed, added, removed, unchanged := result.Counts()
	fmt.Printf("%d resource changed, %d added, %d removed, %d unchanged\n", changed, added, removed, unchanged)
}
