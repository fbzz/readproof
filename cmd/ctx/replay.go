package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newReplayCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "replay <manifest-or-run>",
		Short: "Reconstruct the exact context originally delivered for a manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			a, err := openApp()
			if err != nil {
				return err
			}
			defer a.Close()

			result, err := a.Replayer.Replay(context.Background(), target)
			if err != nil {
				return err
			}

			fmt.Printf("Replaying manifest %s (run %s), %d entr%s\n\n",
				result.Manifest.ManifestID, result.Manifest.RunID, len(result.Entries), pluralEntries(len(result.Entries)))

			matched := 0
			for _, e := range result.Entries {
				fmt.Printf("[%d] %s\n", e.Position, e.URI)
				fmt.Printf("    materialization: %s\n", e.MaterializationID)
				fmt.Printf("    content_hash (recorded):  %s\n", e.RecordedHash)
				fmt.Printf("    content_hash (replayed):  %s\n", e.ReplayedHash)
				if e.Match {
					fmt.Println("    match: OK")
					matched++
				} else {
					fmt.Println("    match: MISMATCH")
				}
				fmt.Println()
				fmt.Println("--- content ---")
				fmt.Println(string(e.Content))
			}

			fmt.Printf("Replay verified: SHA256 match for %d/%d entries.\n", matched, len(result.Entries))
			if matched != len(result.Entries) {
				mismatched := len(result.Entries) - matched
				return fmt.Errorf("replay verification failed for %d entr%s", mismatched, pluralEntries(mismatched))
			}
			return nil
		},
	}
}
