package replay

import (
	"context"
	"fmt"

	"readproof/internal/ids"
	"readproof/internal/manifest"
	"readproof/internal/materialization"
	"readproof/internal/storage/blob"
)

// EntryResult verifies one manifest entry by recomputing the content hash
// from the bytes actually retrieved from the blob store — a true
// reconstruction check, not just a comparison of two stored copies of the
// same value.
type EntryResult struct {
	Position          int
	URI               string
	MaterializationID string
	RecordedHash      string
	ReplayedHash      string
	Content           []byte
	Match             bool
}

type Result struct {
	Manifest manifest.Manifest
	Entries  []EntryResult
}

func (r Result) AllMatch() bool {
	for _, e := range r.Entries {
		if !e.Match {
			return false
		}
	}
	return true
}

// Replayer reconstructs content purely from manifest_entries ->
// materializations -> blob store. It never re-fetches from the live source,
// proving a manifest is a durable, replayable record independent of source
// availability.
type Replayer struct {
	Manifests        manifest.Store
	Materializations materialization.Store
	Blobs            blob.Store
}

func (r *Replayer) Replay(ctx context.Context, manifestOrRun string) (Result, error) {
	man, err := r.Manifests.GetByIDOrRun(ctx, manifestOrRun)
	if err != nil {
		return Result{}, fmt.Errorf("replay: load manifest: %w", err)
	}

	entries := make([]EntryResult, len(man.Entries))
	for i, e := range man.Entries {
		mat, err := r.Materializations.Get(ctx, e.MaterializationID)
		if err != nil {
			return Result{}, fmt.Errorf("replay: load materialization %s: %w", e.MaterializationID, err)
		}
		content, err := r.Blobs.Get(mat.ContentHash)
		if err != nil {
			return Result{}, fmt.Errorf("replay: load blob %s: %w", mat.ContentHash, err)
		}
		replayedHash := ids.ContentHash(content)
		entries[i] = EntryResult{
			Position:          e.Position,
			URI:               e.URI,
			MaterializationID: e.MaterializationID,
			RecordedHash:      e.ContentHash,
			ReplayedHash:      replayedHash,
			Content:           content,
			Match:             e.ContentHash == replayedHash,
		}
	}
	return Result{Manifest: man, Entries: entries}, nil
}
