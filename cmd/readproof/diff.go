package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"readproof/internal/diff"
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
			fmt.Println(whyLine(e))
			fmt.Print(indent(e.UnifiedDiff, "  "))
			fmt.Println()
		}
	}

	changed, added, removed, unchanged := result.Counts()
	fmt.Printf("%d resource changed, %d added, %d removed, %d unchanged\n", changed, added, removed, unchanged)
}

// whyLine summarises, in one line, the provenance behind a changed entry:
// what the source itself called each revision and when Readproof observed
// it — the question "why did the bytes change?" answered before showing
// how.
func whyLine(e diff.EntryDiff) string {
	line := fmt.Sprintf("  why: source revision %s → %s; observed %s → %s",
		orDash(e.SourceRevisionA), orDash(e.SourceRevisionB),
		e.ObservedAtA.Format(time.RFC3339), e.ObservedAtB.Format(time.RFC3339))
	// Refs only matter when at least one side was mounted by tag.
	if e.RefA != "" || e.RefB != "" {
		line += fmt.Sprintf("; ref %s → %s", orDash(e.RefA), orDash(e.RefB))
	}
	return line
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
