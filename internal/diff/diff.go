// Package diff computes the resolved-context difference between two
// manifests. It is shared by the CLI's local (embedded) diff command and
// the HTTP API's /v1/diff handler, so the exact same algorithm backs both.
package diff

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/pmezard/go-difflib/difflib"

	"ctx/internal/manifest"
	"ctx/internal/snapshot"
	"ctx/internal/storage/blob"
)

type Status string

const (
	StatusChanged   Status = "changed"
	StatusAdded     Status = "added"
	StatusRemoved   Status = "removed"
	StatusUnchanged Status = "unchanged"
)

// EntryDiff describes one URI's status between manifest A and manifest B.
// The per-side provenance fields answer "why did this change?" without a
// second round of lookups by the caller: they come from the snapshot each
// side's manifest entry names.
type EntryDiff struct {
	URI    string
	Status Status
	// SnapshotIDA/SnapshotIDB are "" when the URI is absent from that side;
	// every other *A/*B field is likewise only set for a side that's present.
	SnapshotIDA string
	SnapshotIDB string
	// SourceRevisionA/B and ObservedAtA/B are the snapshot's recorded
	// provenance — the source's own revision marker and when Ctx observed it.
	SourceRevisionA string
	SourceRevisionB string
	ObservedAtA     time.Time
	ObservedAtB     time.Time
	// RefA/RefB are the "@<tag>" each side was mounted by, "" for a plain URI.
	RefA string
	RefB string
	// UnifiedDiff is set only when Status == StatusChanged.
	UnifiedDiff string
}

// Result is the full comparison, entries sorted by URI.
type Result struct {
	ManifestA manifest.Manifest
	ManifestB manifest.Manifest
	Entries   []EntryDiff
}

func (r Result) Counts() (changed, added, removed, unchanged int) {
	for _, e := range r.Entries {
		switch e.Status {
		case StatusChanged:
			changed++
		case StatusAdded:
			added++
		case StatusRemoved:
			removed++
		case StatusUnchanged:
			unchanged++
		}
	}
	return
}

// Compute diffs manA against manB by URI and content hash, fetching blobs
// to render a unified diff for any changed entry and snapshots to attach
// each side's provenance. Taking the stores as arguments (rather than
// hydrating in the caller) keeps this the single place that knows how a
// diff is assembled, mirroring replay.Replayer.
func Compute(ctx context.Context, manA, manB manifest.Manifest, blobs blob.Store, snapshots snapshot.Store) (Result, error) {
	entriesA := entriesByURI(manA)
	entriesB := entriesByURI(manB)
	uris := unionURIs(entriesA, entriesB)

	result := Result{ManifestA: manA, ManifestB: manB}
	for _, uri := range uris {
		ea, okA := entriesA[uri]
		eb, okB := entriesB[uri]

		entry := EntryDiff{URI: uri}
		if okA {
			side, err := provenanceOf(ctx, snapshots, ea)
			if err != nil {
				return Result{}, err
			}
			entry.SnapshotIDA, entry.SourceRevisionA, entry.ObservedAtA, entry.RefA = side.snapshotID, side.sourceRevision, side.observedAt, side.ref
		}
		if okB {
			side, err := provenanceOf(ctx, snapshots, eb)
			if err != nil {
				return Result{}, err
			}
			entry.SnapshotIDB, entry.SourceRevisionB, entry.ObservedAtB, entry.RefB = side.snapshotID, side.sourceRevision, side.observedAt, side.ref
		}

		switch {
		case okA && !okB:
			entry.Status = StatusRemoved
		case !okA && okB:
			entry.Status = StatusAdded
		case ea.ContentHash == eb.ContentHash:
			entry.Status = StatusUnchanged
		default:
			entry.Status = StatusChanged
			oldContent, err := blobs.Get(ea.ContentHash)
			if err != nil {
				return Result{}, fmt.Errorf("diff: load %s content: %w", uri, err)
			}
			newContent, err := blobs.Get(eb.ContentHash)
			if err != nil {
				return Result{}, fmt.Errorf("diff: load %s content: %w", uri, err)
			}
			text, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
				A:        difflib.SplitLines(string(oldContent)),
				B:        difflib.SplitLines(string(newContent)),
				FromFile: "a/" + uri,
				ToFile:   "b/" + uri,
				Context:  3,
			})
			if err != nil {
				return Result{}, fmt.Errorf("diff: render %s: %w", uri, err)
			}
			entry.UnifiedDiff = text
		}
		result.Entries = append(result.Entries, entry)
	}
	return result, nil
}

// side is one manifest's view of a URI: what the entry recorded plus the
// provenance of the snapshot it names.
type side struct {
	snapshotID     string
	sourceRevision string
	observedAt     time.Time
	ref            string
}

func provenanceOf(ctx context.Context, snapshots snapshot.Store, e manifest.Entry) (side, error) {
	snap, err := snapshots.Get(ctx, e.SnapshotID)
	if err != nil {
		return side{}, fmt.Errorf("diff: load %s snapshot %s: %w", e.URI, e.SnapshotID, err)
	}
	return side{
		snapshotID:     e.SnapshotID,
		sourceRevision: snap.SourceRevision,
		observedAt:     snap.ObservedAt,
		ref:            e.Ref,
	}, nil
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
