package resolver

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"ctx/internal/ids"
	"ctx/internal/materialization"
	"ctx/internal/policy"
	"ctx/internal/resource"
	"ctx/internal/snapshot"
	"ctx/internal/source"
	"ctx/internal/storage/blob"
	"ctx/internal/telemetry"
)

// ResolveResult is what a single Resolve call produces. Manifest-entry
// creation is deliberately NOT part of this result — that's run.Builder's
// job, since Resolve backs both `ctx get` and `run.Builder.Mount`.
type ResolveResult struct {
	Resource        resource.Resource
	Snapshot        snapshot.Snapshot
	Materialization materialization.Materialization
	Content         []byte
	Decision        policy.Decision
}

type Resolver struct {
	Resources        resource.Store
	Snapshots        snapshot.Store
	Materializations materialization.Store
	Blobs            blob.Store
	Sources          *source.Registry
	Materializer     materialization.Materializer
	// Clock defaults to time.Now().UTC() when nil; overridable for tests.
	Clock func() time.Time
}

func (r *Resolver) now() time.Time {
	if r.Clock != nil {
		return r.Clock()
	}
	return time.Now().UTC()
}

// Resolve runs the full resolution pipeline: parse URI -> resource lookup ->
// policy/freshness evaluation -> fetch-or-reuse -> snapshot creation ->
// get-or-create materialization -> result. Every stage is traced (span
// names match spec §35: ctx.resolve, ctx.resource.lookup,
// ctx.policy.evaluate, ctx.cache.lookup, ctx.source.fetch,
// ctx.snapshot.create, ctx.materialize) and the top-level call is
// metered. Resolved content is never attached to spans/metrics.
func (r *Resolver) Resolve(ctx context.Context, uri string) (result ResolveResult, err error) {
	ctx, span := telemetry.Tracer.Start(ctx, "ctx.resolve", trace.WithAttributes(attribute.String("ctx.resource.uri", uri)))
	start := time.Now()
	defer func() {
		telemetry.RecordResolve(ctx, uri, time.Since(start).Seconds(), err)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetAttributes(
				attribute.String("ctx.snapshot.id", result.Snapshot.SnapshotID),
				attribute.String("ctx.materialization.id", result.Materialization.MaterializationID),
				attribute.String("ctx.freshness.status", result.Decision.String()),
			)
		}
		span.End()
	}()

	if _, err = resource.ParseURI(uri); err != nil {
		return ResolveResult{}, err
	}

	var res resource.Resource
	err = func() error {
		lctx, lspan := telemetry.Tracer.Start(ctx, "ctx.resource.lookup")
		defer lspan.End()
		var e error
		res, e = r.Resources.Get(lctx, uri)
		if e != nil {
			lspan.RecordError(e)
			lspan.SetStatus(codes.Error, e.Error())
		}
		return e
	}()
	if err != nil {
		err = fmt.Errorf("resolver: %w", err)
		return ResolveResult{}, err
	}

	var current snapshot.Snapshot
	hasCurrent := res.CurrentSnapshotID != ""
	var decision policy.Decision
	now := r.now()
	err = func() error {
		pctx, pspan := telemetry.Tracer.Start(ctx, "ctx.policy.evaluate", trace.WithAttributes(attribute.String("ctx.policy.strategy", string(res.Policy.Strategy))))
		defer pspan.End()
		if hasCurrent {
			var e error
			current, e = r.Snapshots.Get(pctx, res.CurrentSnapshotID)
			if e != nil {
				pspan.RecordError(e)
				pspan.SetStatus(codes.Error, e.Error())
				return e
			}
		}
		decision = policy.Evaluate(res.Policy, hasCurrent, current.ObservedAt, now)
		pspan.SetAttributes(attribute.String("ctx.freshness.status", decision.String()))
		return nil
	}()
	if err != nil {
		err = fmt.Errorf("resolver: load current snapshot: %w", err)
		return ResolveResult{}, err
	}

	telemetry.RecordCacheResult(ctx, decision != policy.DecisionFetch)

	var snap snapshot.Snapshot
	var content []byte

	switch decision {
	case policy.DecisionUsePinned:
		err = func() error {
			cctx, cspan := telemetry.Tracer.Start(ctx, "ctx.cache.lookup", trace.WithAttributes(attribute.Bool("ctx.cache.hit", true)))
			defer cspan.End()
			var e error
			snap, e = r.Snapshots.Get(cctx, res.Policy.PinnedSnapshotID)
			if e != nil {
				cspan.RecordError(e)
				cspan.SetStatus(codes.Error, e.Error())
				return fmt.Errorf("load pinned snapshot: %w", e)
			}
			content, e = r.Blobs.Get(snap.ContentHash)
			if e != nil {
				cspan.RecordError(e)
				cspan.SetStatus(codes.Error, e.Error())
				return fmt.Errorf("load pinned blob: %w", e)
			}
			return nil
		}()
		if err != nil {
			err = fmt.Errorf("resolver: %w", err)
			return ResolveResult{}, err
		}

	case policy.DecisionUseCurrent:
		snap = current
		err = func() error {
			_, cspan := telemetry.Tracer.Start(ctx, "ctx.cache.lookup", trace.WithAttributes(attribute.Bool("ctx.cache.hit", true)))
			defer cspan.End()
			var e error
			content, e = r.Blobs.Get(snap.ContentHash)
			if e != nil {
				cspan.RecordError(e)
				cspan.SetStatus(codes.Error, e.Error())
			}
			return e
		}()
		if err != nil {
			err = fmt.Errorf("resolver: load cached blob: %w", err)
			return ResolveResult{}, err
		}

	default: // policy.DecisionFetch
		var fr source.FetchResult
		err = func() error {
			fctx, fspan := telemetry.Tracer.Start(ctx, "ctx.source.fetch", trace.WithAttributes(attribute.String("ctx.source.type", string(res.SourceConfig.Kind))))
			defer fspan.End()
			fetchStart := time.Now()
			var e error
			fr, e = r.Sources.Fetch(fctx, source.FetchRequest{Config: res.SourceConfig})
			telemetry.RecordSourceFetch(ctx, string(res.SourceConfig.Kind), time.Since(fetchStart).Seconds(), e)
			if e != nil {
				fspan.RecordError(e)
				fspan.SetStatus(codes.Error, e.Error())
			}
			return e
		}()
		if err != nil {
			err = fmt.Errorf("resolver: fetch source: %w", err)
			return ResolveResult{}, err
		}

		err = func() error {
			sctx, sspan := telemetry.Tracer.Start(ctx, "ctx.snapshot.create")
			defer sspan.End()

			hash, e := r.Blobs.Put(fr.Content)
			if e != nil {
				sspan.RecordError(e)
				sspan.SetStatus(codes.Error, e.Error())
				return fmt.Errorf("store blob: %w", e)
			}
			// A new snapshot row is always created on fetch, even if
			// content is byte-identical to the current snapshot:
			// snapshots are an observation log, blobs are the dedup layer.
			snap = snapshot.Snapshot{
				SnapshotID:     ids.New("snap"),
				ResourceURI:    uri,
				SourceRevision: fr.SourceRevision,
				ContentHash:    hash,
				ObservedAt:     now,
				CreatedAt:      now,
				ContentType:    fr.ContentType,
				Bytes:          int64(len(fr.Content)),
				Provenance:     fr.Metadata,
			}
			if e := r.Snapshots.Create(sctx, snap); e != nil {
				sspan.RecordError(e)
				sspan.SetStatus(codes.Error, e.Error())
				return fmt.Errorf("create snapshot: %w", e)
			}
			telemetry.RecordSnapshotCreated(ctx)
			if e := r.Resources.UpdateCurrentSnapshot(sctx, uri, snap.SnapshotID); e != nil {
				sspan.RecordError(e)
				sspan.SetStatus(codes.Error, e.Error())
				return fmt.Errorf("update current snapshot: %w", e)
			}
			sspan.SetAttributes(attribute.String("ctx.snapshot.id", snap.SnapshotID))
			return nil
		}()
		if err != nil {
			err = fmt.Errorf("resolver: %w", err)
			return ResolveResult{}, err
		}
		content = fr.Content
	}

	var mat materialization.Materialization
	err = func() error {
		mctx, mspan := telemetry.Tracer.Start(ctx, "ctx.materialize")
		defer mspan.End()

		found := false
		var e error
		mat, found, e = r.Materializations.GetBySnapshot(mctx, snap.SnapshotID, materialization.StrategyRaw)
		if e != nil {
			mspan.RecordError(e)
			mspan.SetStatus(codes.Error, e.Error())
			return fmt.Errorf("lookup materialization: %w", e)
		}
		mspan.SetAttributes(attribute.Bool("ctx.materialization.cached", found))
		if found {
			return nil
		}

		out, e := r.Materializer.Materialize(mctx, snap, content)
		if e != nil {
			mspan.RecordError(e)
			mspan.SetStatus(codes.Error, e.Error())
			return fmt.Errorf("materialize: %w", e)
		}
		matHash, e := r.Blobs.Put(out)
		if e != nil {
			mspan.RecordError(e)
			mspan.SetStatus(codes.Error, e.Error())
			return fmt.Errorf("store materialization blob: %w", e)
		}
		mat = materialization.Materialization{
			MaterializationID: ids.New("mat"),
			SnapshotID:        snap.SnapshotID,
			Strategy:          materialization.StrategyRaw,
			ContentHash:       matHash,
			Bytes:             int64(len(out)),
			CreatedAt:         now,
		}
		if e := r.Materializations.Create(mctx, mat); e != nil {
			mspan.RecordError(e)
			mspan.SetStatus(codes.Error, e.Error())
			return fmt.Errorf("create materialization: %w", e)
		}
		telemetry.RecordMaterializationCreated(ctx)
		mspan.SetAttributes(attribute.String("ctx.materialization.id", mat.MaterializationID))
		return nil
	}()
	if err != nil {
		err = fmt.Errorf("resolver: %w", err)
		return ResolveResult{}, err
	}

	result = ResolveResult{
		Resource:        res,
		Snapshot:        snap,
		Materialization: mat,
		Content:         content,
		Decision:        decision,
	}
	return result, nil
}
