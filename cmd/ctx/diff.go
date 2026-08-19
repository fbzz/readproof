package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/pmezard/go-difflib/difflib"
	"github.com/spf13/cobra"

	"ctx/internal/manifest"
)

func newDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff <target-a> <target-b>",
		Short: "Diff the resolved context between two manifests or runs",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetA, targetB := args[0], args[1]
			a, err := openApp()
			if err != nil {
				return err
			}
			defer a.Close()
			ctx := context.Background()

			manA, err := a.Manifests.GetByIDOrRun(ctx, targetA)
			if err != nil {
				return err
			}
			manB, err := a.Manifests.GetByIDOrRun(ctx, targetB)
			if err != nil {
				return err
			}

			entriesA := entriesByURI(manA)
			entriesB := entriesByURI(manB)
			uris := unionURIs(entriesA, entriesB)

			fmt.Printf("--- %s (%s)\n", targetA, manA.ManifestID)
			fmt.Printf("+++ %s (%s)\n\n", targetB, manB.ManifestID)

			changed, added, removed, unchanged := 0, 0, 0, 0
			for _, uri := range uris {
				ea, okA := entriesA[uri]
				eb, okB := entriesB[uri]
				switch {
				case okA && !okB:
					removed++
					fmt.Printf("- %s (removed, was %s)\n\n", uri, ea.SnapshotID)
				case !okA && okB:
					added++
					fmt.Printf("+ %s (added, %s)\n\n", uri, eb.SnapshotID)
				case ea.ContentHash == eb.ContentHash:
					unchanged++
				default:
					changed++
					fmt.Printf("~ %s  (%s -> %s)\n", uri, ea.SnapshotID, eb.SnapshotID)
					oldContent, err := a.Blobs.Get(ea.ContentHash)
					if err != nil {
						return err
					}
					newContent, err := a.Blobs.Get(eb.ContentHash)
					if err != nil {
						return err
					}
					text, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
						A:        difflib.SplitLines(string(oldContent)),
						B:        difflib.SplitLines(string(newContent)),
						FromFile: "a/" + uri,
						ToFile:   "b/" + uri,
						Context:  3,
					})
					if err != nil {
						return err
					}
					fmt.Print(indent(text, "  "))
					fmt.Println()
				}
			}

			fmt.Printf("%d resource changed, %d added, %d removed, %d unchanged\n", changed, added, removed, unchanged)
			return nil
		},
	}
}

func entriesByURI(m manifest.Manifest) map[string]manifest.Entry {
	out := make(map[string]manifest.Entry, len(m.Entries))
	for _, e := range m.Entries {
		out[e.URI] = e
	}
	return out
}

func unionURIs(a, b map[string]manifest.Entry) []string {
	set := make(map[string]struct{})
	for uri := range a {
		set[uri] = struct{}{}
	}
	for uri := range b {
		set[uri] = struct{}{}
	}
	uris := make([]string, 0, len(set))
	for uri := range set {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	return uris
}
