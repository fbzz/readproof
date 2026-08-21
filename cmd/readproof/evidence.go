package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"readproof/internal/evidence"
)

func newEvidenceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evidence",
		Short: "Export and verify tamper-evident evidence bundles for a run",
	}
	cmd.AddCommand(newEvidenceExportCmd(), newEvidenceVerifyCmd())
	return cmd
}

func newEvidenceExportCmd() *cobra.Command {
	var out string
	var withContent bool

	cmd := &cobra.Command{
		Use:   "export <manifest-or-run>",
		Short: "Export an in-toto evidence bundle for a manifest or run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			c, err := openClient()
			if err != nil {
				return err
			}
			defer c.Close()

			bundle, err := evidence.Build(context.Background(), c, target, evidence.Options{WithContent: withContent})
			if err != nil {
				return err
			}
			data, err := evidence.Encode(bundle)
			if err != nil {
				return err
			}

			// Warn but still export: a manifest that no longer replays is
			// exactly the thing an auditor needs a signed record of.
			if !bundle.Predicate.Replay.AllMatch {
				fmt.Fprintf(os.Stderr, "warning: replay did not verify for every entry (%s)\n", replayWarning(bundle))
			}

			if out == "" {
				_, err := os.Stdout.Write(data)
				return err
			}
			if err := os.WriteFile(out, data, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", out, err)
			}
			fmt.Printf("evidence bundle written to %s: %d entr%s, merkle root %s\n",
				out, len(bundle.Predicate.Entries), pluralEntries(len(bundle.Predicate.Entries)), bundle.Predicate.Merkle.Root)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "write the bundle to a file instead of stdout")
	cmd.Flags().BoolVar(&withContent, "with-content", false, "embed each entry's bytes as base64 (content_b64)")
	return cmd
}

func replayWarning(b evidence.Bundle) string {
	if b.Predicate.Replay.Error != "" {
		return b.Predicate.Replay.Error
	}
	mismatched := 0
	for _, e := range b.Predicate.Replay.Entries {
		if !e.Match {
			mismatched++
		}
	}
	return fmt.Sprintf("%d mismatched entr%s", mismatched, pluralEntries(mismatched))
}

func newEvidenceVerifyCmd() *cobra.Command {
	var offline bool

	cmd := &cobra.Command{
		Use:   "verify <bundle.json>",
		Short: "Verify an evidence bundle's merkle root, embedded content, and (unless --offline) the store",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			bundle, err := evidence.Decode(data)
			if err != nil {
				return err
			}

			opts := evidence.VerifyOptions{Context: context.Background()}
			if !offline {
				c, err := openClient()
				if err != nil {
					return err
				}
				defer c.Close()
				opts.Client = c
			}

			report, err := evidence.Verify(bundle, opts)
			if err != nil {
				return err
			}

			if !report.OK {
				// Print every check, not just the failures: knowing which
				// checks passed is what tells an operator whether the
				// bundle was tampered with or the store moved on.
				for _, check := range report.Checks {
					status := "ok  "
					if !check.OK {
						status = "FAIL"
					}
					fmt.Printf("  %s  %-22s %s\n", status, check.Name, check.Detail)
				}
				return fmt.Errorf("evidence verification failed: %d of %d checks failed", failedChecks(report), len(report.Checks))
			}

			fmt.Printf("evidence verified: %d entr%s, merkle root %s%s, %s\n",
				report.Entries, pluralEntries(report.Entries), report.MerkleRoot,
				contentSummary(report), replaySummary(report))
			return nil
		},
	}
	cmd.Flags().BoolVar(&offline, "offline", false, "skip the store cross-check and verify the bundle on its own")
	return cmd
}

func failedChecks(r evidence.Report) int {
	n := 0
	for _, c := range r.Checks {
		if !c.OK {
			n++
		}
	}
	return n
}

func contentSummary(r evidence.Report) string {
	if r.ContentChecked == 0 {
		return ""
	}
	return fmt.Sprintf(", embedded content %d/%d re-hashed", r.ContentChecked, r.Entries)
}

func replaySummary(r evidence.Report) string {
	if !r.ReplayChecked {
		return "replay cross-check skipped (--offline)"
	}
	return fmt.Sprintf("replay match %d/%d", r.ReplayMatched, r.ReplayTotal)
}
