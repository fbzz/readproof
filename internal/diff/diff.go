// Package diff computes the resolved-context difference between two
// manifests. It is shared by the CLI's local (embedded) diff command and
// the HTTP API's /v1/diff handler, so the exact same algorithm backs both.
package diff

import (
	"fmt"
	"sort"

	"github.com/pmezard/go-difflib/difflib"

	"ctx/internal/manifest"
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
type EntryDiff struct {
	URI    string
	Status Status
	// SnapshotIDA/SnapshotIDB are "" when the URI is absent from that side.
	SnapshotIDA string
	SnapshotIDB string
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
// to render a unified diff for any changed entry.
func Compute(manA, manB manifest.Manifest, blobs blob.Store) (Result, error) {
	entriesA := entriesByURI(manA)
	entriesB := entriesByURI(manB)
	uris := unionURIs(entriesA, entriesB)

	result := Result{ManifestA: manA, ManifestB: manB}
	for _, uri := range uris {
		ea, okA := entriesA[uri]
		eb, okB := entriesB[uri]
		switch {
		case okA && !okB:
			result.Entries = append(result.Entries, EntryDiff{URI: uri, Status: StatusRemoved, SnapshotIDA: ea.SnapshotID})
		case !okA && okB:
			result.Entries = append(result.Entries, EntryDiff{URI: uri, Status: StatusAdded, SnapshotIDB: eb.SnapshotID})
		case ea.ContentHash == eb.ContentHash:
			result.Entries = append(result.Entries, EntryDiff{URI: uri, Status: StatusUnchanged, SnapshotIDA: ea.SnapshotID, SnapshotIDB: eb.SnapshotID})
		default:
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
			result.Entries = append(result.Entries, EntryDiff{
				URI: uri, Status: StatusChanged,
				SnapshotIDA: ea.SnapshotID, SnapshotIDB: eb.SnapshotID,
				UnifiedDiff: text,
			})
		}
	}
	return result, nil
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
