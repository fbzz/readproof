package evidence

import (
	"context"
	"encoding/base64"
	"fmt"

	"ctx/internal/client"
	"ctx/internal/ids"
)

// VerifyOptions configures the checks Verify runs.
type VerifyOptions struct {
	// Client, when non-nil, adds the store cross-check: the manifest is
	// replayed again now and its hashes are compared with the ones the
	// bundle recorded. Leave nil for a fully offline verification.
	Client client.Client
	// Context is used for those Client calls. The Verify signature is
	// fixed by the evidence API, so the context rides along here instead
	// of being the first argument; nil means context.Background().
	Context context.Context
}

// Check is one verification step and its outcome. Detail always explains a
// failure, and usually describes a pass too.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Report is the result of Verify: every check that ran, plus the counts a
// caller needs for a one-line summary.
type Report struct {
	OK             bool    `json:"ok"`
	Checks         []Check `json:"checks"`
	Entries        int     `json:"entries"`
	MerkleRoot     string  `json:"merkle_root"`
	ContentChecked int     `json:"content_checked"`
	ReplayChecked  bool    `json:"replay_checked"`
	ReplayMatched  int     `json:"replay_matched"`
	ReplayTotal    int     `json:"replay_total"`
}

func (r *Report) add(name string, ok bool, format string, args ...any) {
	r.Checks = append(r.Checks, Check{Name: name, OK: ok, Detail: fmt.Sprintf(format, args...)})
	if !ok {
		r.OK = false
	}
}

// Verify recomputes everything a bundle claims about itself, and — when a
// Client is supplied — cross-checks those claims against the live store.
//
// The error return is reserved for operational failures; verification
// outcomes, including an unreachable store, are always reported as failed
// checks in the Report so a caller can print all of them at once.
func Verify(b Bundle, opts VerifyOptions) (Report, error) {
	report := Report{
		OK:          true,
		Entries:     len(b.Predicate.Entries),
		ReplayTotal: len(b.Predicate.Entries),
	}

	verifyStatement(b, &report)
	root := verifyMerkle(b, &report)
	report.MerkleRoot = root
	verifyEntryOrder(b, &report)
	verifyEmbeddedContent(b, &report)
	verifyAgainstStore(b, opts, &report)

	return report, nil
}

func verifyStatement(b Bundle, report *Report) {
	if b.Type == StatementType {
		report.add("statement_type", true, "%s", b.Type)
	} else {
		report.add("statement_type", false, "expected %q, got %q", StatementType, b.Type)
	}

	if b.PredicateType == PredicateType {
		report.add("predicate_type", true, "%s", b.PredicateType)
	} else {
		// The predicate type is what gives every other field its meaning,
		// so an unrecognized one is a failure rather than a warning.
		report.add("predicate_type", false, "expected %q, got %q", PredicateType, b.PredicateType)
	}
}

// verifyMerkle recomputes the root from the entries and checks it against
// both places the bundle records it: the in-toto subject digest (what an
// external attestation signs) and predicate.merkle.root.
func verifyMerkle(b Bundle, report *Report) string {
	root := MerkleRoot(b.Predicate.Entries)

	switch {
	case len(b.Subject) != 1:
		report.add("subject", false, "expected exactly 1 subject, got %d", len(b.Subject))
	case b.Subject[0].Name != b.Predicate.ManifestID:
		report.add("subject", false, "subject name %q does not match predicate.manifest_id %q", b.Subject[0].Name, b.Predicate.ManifestID)
	default:
		report.add("subject", true, "%s", b.Subject[0].Name)
	}

	if len(b.Subject) == 1 {
		if got := b.Subject[0].Digest.SHA256; got == root {
			report.add("merkle_root", true, "%s (%d entr%s)", root, len(b.Predicate.Entries), pluralY(len(b.Predicate.Entries)))
		} else {
			report.add("merkle_root", false, "recomputed %s, subject digest %s", root, got)
		}
	}

	if b.Predicate.Merkle.Root == root {
		report.add("predicate_merkle_root", true, "%s", root)
	} else {
		report.add("predicate_merkle_root", false, "recomputed %s, predicate.merkle.root %s", root, b.Predicate.Merkle.Root)
	}

	return root
}

// verifyEntryOrder guards the invariant the Merkle leaf depends on: entry
// order is meaningful in Ctx, so positions must be the exact sequence
// 0..n-1 in ascending order.
func verifyEntryOrder(b Bundle, report *Report) {
	for i, e := range b.Predicate.Entries {
		if e.Position != i {
			report.add("entry_order", false, "entry at index %d has position %d", i, e.Position)
			return
		}
	}
	report.add("entry_order", true, "positions 0..%d in order", len(b.Predicate.Entries)-1)
}

func verifyEmbeddedContent(b Bundle, report *Report) {
	for _, e := range b.Predicate.Entries {
		if e.ContentB64 == "" {
			continue
		}
		name := fmt.Sprintf("content[%d]", e.Position)
		raw, err := base64.StdEncoding.DecodeString(e.ContentB64)
		if err != nil {
			report.add(name, false, "%s: content_b64 is not valid base64: %v", e.URI, err)
			continue
		}
		report.ContentChecked++
		if got := ids.ContentHash(raw); got != e.ContentHash {
			report.add(name, false, "%s: embedded content hashes to %s, entry records %s", e.URI, got, e.ContentHash)
			continue
		}
		report.add(name, true, "%s: %d bytes hash to %s", e.URI, len(raw), e.ContentHash)
	}
}

// verifyAgainstStore replays the manifest again, now, and compares the
// hashes the store produces with the ones the bundle recorded — the check
// that turns "this file is internally consistent" into "this file still
// describes what Ctx holds".
func verifyAgainstStore(b Bundle, opts VerifyOptions, report *Report) {
	if opts.Client == nil {
		return
	}
	report.ReplayChecked = true

	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}

	target := b.Predicate.ManifestID
	if target == "" {
		target = b.Predicate.RunID
	}
	result, err := opts.Client.Replay(ctx, target)
	if err != nil {
		report.add("store_replay", false, "replaying %s failed: %v (use --offline to skip store cross-checks)", target, err)
		return
	}

	byPosition := make(map[int]string, len(result.Entries))
	for _, e := range result.Entries {
		byPosition[e.Position] = e.ReplayedHash
	}

	for _, e := range b.Predicate.Entries {
		name := fmt.Sprintf("store_replay[%d]", e.Position)
		got, ok := byPosition[e.Position]
		if !ok {
			report.add(name, false, "%s: position %d is absent from the replayed manifest", e.URI, e.Position)
			continue
		}
		if got != e.ContentHash {
			report.add(name, false, "%s: store replays %s, bundle records %s", e.URI, got, e.ContentHash)
			continue
		}
		report.ReplayMatched++
		report.add(name, true, "%s: %s", e.URI, got)
	}

	if extra := len(result.Entries) - len(b.Predicate.Entries); extra != 0 {
		report.add("store_replay_count", false, "store replayed %d entries, bundle records %d", len(result.Entries), len(b.Predicate.Entries))
	} else {
		report.add("store_replay_count", true, "%d entr%s", len(result.Entries), pluralY(len(result.Entries)))
	}
}

func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
