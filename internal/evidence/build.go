package evidence

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"ctx/internal/client"
	"ctx/internal/manifest"
	"ctx/internal/redact"
	"ctx/internal/replay"
	"ctx/internal/resource"
	"ctx/internal/snapshot"
	"ctx/internal/source"
)

// Options controls what Build puts in the bundle.
type Options struct {
	// WithContent embeds each entry's replayed bytes as base64. Off by
	// default: the metadata-only bundle is the shareable one.
	WithContent bool
	// Now overrides the clock for generated_at / replay.verified_at so
	// tests can produce byte-stable bundles.
	Now func() time.Time
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now().UTC()
}

// Build assembles an evidence bundle for a manifest id or run id, using
// only client.Client calls so the result is identical in embedded mode and
// against a remote ctxd.
func Build(ctx context.Context, c client.Client, target string, opts Options) (Bundle, error) {
	man, err := c.GetManifest(ctx, target)
	if err != nil {
		return Bundle{}, fmt.Errorf("evidence: load manifest %q: %w", target, err)
	}

	// Replay is the source of both the reconstruction check and (with
	// --with-content) the bytes themselves: replay.EntryResult carries the
	// blob content it re-hashed, so no direct storage access is needed.
	replayed, replayErr := c.Replay(ctx, target)
	byPosition := make(map[int]replay.EntryResult, len(replayed.Entries))
	for _, e := range replayed.Entries {
		byPosition[e.Position] = e
	}

	entries, err := buildEntries(ctx, c, man, byPosition, opts.WithContent)
	if err != nil {
		return Bundle{}, err
	}

	resources, err := buildResources(ctx, c, man)
	if err != nil {
		return Bundle{}, err
	}

	now := opts.now()
	root := MerkleRoot(entries)

	return Bundle{
		Type: StatementType,
		Subject: []Subject{{
			Name:   man.ManifestID,
			Digest: Digest{SHA256: root},
		}},
		PredicateType: PredicateType,
		Predicate: Predicate{
			RunID:             man.RunID,
			ManifestID:        man.ManifestID,
			ManifestCreatedAt: man.CreatedAt,
			GeneratedAt:       now,
			Exporter:          Exporter{Name: ExporterName, Version: ExporterVersion},
			Merkle:            Merkle{Algorithm: MerkleAlgorithm, Leaf: MerkleLeafFormula, Root: root},
			Entries:           entries,
			Resources:         resources,
			Replay:            buildReplay(now, man, replayed, replayErr),
		},
	}, nil
}

func buildEntries(
	ctx context.Context,
	c client.Client,
	man manifest.Manifest,
	replayedByPosition map[int]replay.EntryResult,
	withContent bool,
) ([]Entry, error) {
	entries := make([]Entry, len(man.Entries))
	for i, me := range man.Entries {
		// A manifest entry pointing at a snapshot that no longer exists is
		// an integrity failure of the store itself, not something to
		// record and move past — fail loudly rather than emit evidence
		// that quietly omits what the agent saw.
		snap, err := c.GetSnapshot(ctx, me.SnapshotID)
		if err != nil {
			return nil, fmt.Errorf("evidence: load snapshot %s for entry %d: %w", me.SnapshotID, me.Position, err)
		}

		e := Entry{
			Position:          me.Position,
			URI:               me.URI,
			Ref:               me.Ref,
			SnapshotID:        me.SnapshotID,
			MaterializationID: me.MaterializationID,
			ContentHash:       me.ContentHash,
			SourceRevision:    snap.SourceRevision,
			ObservedAt:        snap.ObservedAt,
			ContentType:       snap.ContentType,
			Bytes:             snap.Bytes,
			Provenance:        provenanceOrEmpty(snap),
		}
		if withContent {
			if r, ok := replayedByPosition[me.Position]; ok {
				e.ContentB64 = base64.StdEncoding.EncodeToString(r.Content)
			}
		}
		entries[i] = e
	}
	return entries, nil
}

// provenanceOrEmpty normalizes a nil provenance map to an empty one so the
// field always encodes as {} — a verifier should never have to distinguish
// "absent" from "empty" for a purely descriptive field.
func provenanceOrEmpty(s snapshot.Snapshot) map[string]string {
	if s.Provenance == nil {
		return map[string]string{}
	}
	return s.Provenance
}

func buildResources(ctx context.Context, c client.Client, man manifest.Manifest) ([]Resource, error) {
	seen := make(map[string]bool, len(man.Entries))
	out := make([]Resource, 0, len(man.Entries))
	for _, me := range man.Entries {
		if seen[me.URI] {
			continue
		}
		seen[me.URI] = true

		res, err := c.GetResource(ctx, me.URI)
		if err != nil {
			if !isNotFound(err) {
				return nil, fmt.Errorf("evidence: load resource %s: %w", me.URI, err)
			}
			out = append(out, missingResource(me.URI))
			continue
		}
		out = append(out, Resource{
			URI:       res.URI,
			Namespace: res.Namespace,
			Path:      res.Path,
			Source:    sourceToBundle(res.SourceConfig),
			Policy: Policy{
				Strategy:         string(res.Policy.Strategy),
				MaxAgeSeconds:    int64(res.Policy.MaxAge.Seconds()),
				PinnedSnapshotID: res.Policy.PinnedSnapshotID,
			},
		})
	}
	return out, nil
}

// missingResource records a URI whose resource definition is gone, keeping
// whatever identity the URI itself still carries.
func missingResource(uri string) Resource {
	r := Resource{URI: uri, Missing: true}
	if parsed, err := resource.ParseURI(uri); err == nil {
		r.Namespace = parsed.Namespace
		r.Path = parsed.Path
	}
	return r
}

// isNotFound recognizes a missing resource from either client. The local
// client returns the typed sentinel; the remote client flattens the
// server's 404 body into a plain error, so the message is all that's left
// to match on.
func isNotFound(err error) bool {
	if errors.Is(err, resource.ErrNotFound) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

func sourceToBundle(cfg source.Config) Source {
	out := Source{Kind: string(cfg.Kind)}
	if cfg.Filesystem != nil {
		out.Config.Filesystem = &FilesystemConfig{Path: cfg.Filesystem.Path}
	}
	if cfg.GitHub != nil {
		out.Config.GitHub = &GitHubConfig{
			Owner: cfg.GitHub.Owner,
			Repo:  cfg.GitHub.Repo,
			Path:  cfg.GitHub.Path,
			Ref:   cfg.GitHub.Ref,
		}
	}
	if cfg.HTTP != nil {
		// Always redacted, including in embedded mode where the raw header
		// values never crossed a wire: the bundle is the thing that gets
		// exported, attached to a ticket, and mailed to an auditor.
		out.Config.HTTP = &HTTPConfig{
			URL:     cfg.HTTP.URL,
			Headers: redact.Headers(cfg.HTTP.Headers),
		}
	}
	return out
}

func buildReplay(now time.Time, man manifest.Manifest, result replay.Result, replayErr error) Replay {
	if replayErr != nil {
		return Replay{VerifiedAt: now, AllMatch: false, Entries: []ReplayEntry{}, Error: replayErr.Error()}
	}

	entries := make([]ReplayEntry, len(result.Entries))
	for i, e := range result.Entries {
		entries[i] = ReplayEntry{
			Position:     e.Position,
			Match:        e.Match,
			ExpectedHash: e.RecordedHash,
			ActualHash:   e.ReplayedHash,
		}
	}

	r := Replay{VerifiedAt: now, AllMatch: result.AllMatch(), Entries: entries}
	// A short replay is a mismatch even if every entry it did return
	// matched, so say so explicitly rather than reporting all_match.
	if len(result.Entries) != len(man.Entries) {
		r.AllMatch = false
		r.Error = fmt.Sprintf("replay returned %d of %d manifest entries", len(result.Entries), len(man.Entries))
	}
	return r
}
