# Changelog

All notable changes to this project are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - Unreleased

The release that turns the v0.1 walking skeleton into something usable
from outside the repo: movable tags, diffs that explain themselves,
exportable evidence, GenAI-aligned traces, and an MCP server. Plan and
status: [`docs/mvp.md`](docs/mvp.md).

### Added

- **Tags and `@ref` resolution.** A tag is a named, movable pointer
  `(resource_uri, tag) → snapshot_id`. `ctx tag set|list|rm`, and any URI
  argument may carry a trailing `@<tag>` (`ctx get`, `ctx inspect`,
  `ctx run mount`, `ctx run --id`, `POST /v1/resolve`,
  `POST /v1/runs/mount`, SDK `resolve()`/`mount()`), which delivers exactly
  that snapshot: no source fetch, and the resource's freshness policy is
  not consulted (decision `use_tag`). An unknown tag is an error naming
  both the URI and the tag. Manifest and run-mount entries record the bare
  URI plus the `ref` they were mounted by, so moving a tag afterwards can
  never change what a committed manifest replays.
- **Tag endpoints**: `PUT /v1/tags`, `GET /v1/tags?uri=`,
  `DELETE /v1/tags?uri=&tag=`, with wire types and both `client.Client`
  implementations.
- **Evidence bundles** (`internal/evidence`, `ctx evidence`). `ctx evidence
  export <manifest-or-run> [--with-content] [--out f]` writes an in-toto
  Statement v1 whose subject digest is a Merkle root over the manifest's
  entries, carrying per-entry identity, redacted resource definitions, and
  a live replay check. `ctx evidence verify <bundle> [--offline]`
  recomputes the root, re-hashes embedded content, and cross-checks the
  store by replay, printing every check and exiting non-zero on failure.
  Documented in [`docs/evidence.md`](docs/evidence.md), including the
  EU AI Act Art. 12 / SOC 2 framing and an explicit not-legal-advice note.
- **`internal/merkle`**: the single implementation of the manifest digest
  rule (leaf = `sha256(position_be_uint32 ‖ 0x00 ‖ uri ‖ 0x00 ‖
  content_hash)`, root over leaves in position order), shared by evidence
  export and the `ctx.run.commit` span, with fixed test vectors.
- **OpenTelemetry GenAI attributes and run spans.** `ctx.resolve` now
  carries `ctx.snapshot.content_hash`, `ctx.snapshot.source_revision`,
  `ctx.snapshot.observed_at`, `ctx.materialization.bytes`,
  `ctx.source.type`, `ctx.policy.strategy`, `ctx.policy.decision`,
  `ctx.resource.ref`, and `gen_ai.data_source.id` (= `ctx://<namespace>`).
  New spans `ctx.run.start`, `ctx.run.mount` (parenting that mount's
  `ctx.resolve` and `ctx.manifest.append`), `ctx.run.commit` (with
  `ctx.manifest.merkle_root`), and `ctx.tag.lookup`. New metrics
  `ctx_run_committed_total` and `ctx_tag_resolve_total`. Content is never
  attached to a span or metric; tests assert that. Full reference:
  [`docs/observability.md`](docs/observability.md).
- **TypeScript SDK**: `setTag` / `listTags` / `deleteTag`, `@tag` support
  in `resolve()` and `mount()`, per-side diff provenance fields, and
  `buildEvidence()` / `encodeEvidence()` / `merkleRoot()` / `merkleLeaf()`
  — a client-side bundle with the same Merkle root as `ctx evidence
  export`, using only `node:crypto`.
- **MCP server** (WP-B, landing): `ctx mcp` serves registered resources
  (`resources/list`, `resources/read`) and Ctx operations as tools over
  stdio, reusing `--data-dir` / `--server` / `--api-key`. See
  [`docs/mcp.md`](docs/mcp.md).
- **LangGraph.js example** ([`examples/langgraph-ts`](examples/langgraph-ts)):
  a two-node graph that mounts `ctx://` resources in a node, records the
  manifest id in the checkpoint, and replays it byte-for-byte from a
  second process after the source has changed. Pinned dependencies; built
  in CI.
- **Docs**: [`docs/evidence.md`](docs/evidence.md),
  [`docs/observability.md`](docs/observability.md),
  [`docs/mcp.md`](docs/mcp.md), [`docs/roadmap.md`](docs/roadmap.md),
  [`docs/mvp.md`](docs/mvp.md), and this changelog.
- **CI**: a job that builds the TypeScript SDK and then the LangGraph
  example (build only, no run).

### Changed

- **README repositioned** around the lockfile + replay primitive framing,
  with a 60-second walkthrough of the register → run → tag → edit → diff →
  replay → evidence loop, and sections for tags, evidence, MCP, and the
  updated observability surface.
- **`ctx diff` is provenance-aware.** Every changed entry gets a one-line
  `why:` before its unified diff — `source revision X → Y; observed T1 →
  T2`, plus `; ref <a> → <b>` when either side was mounted by tag.
  `diff.EntryDiff` and the wire/SDK types carry each side's
  `SourceRevision`, `ObservedAt`, and `Ref`.
- **`ctx history` and `ctx manifest`** show tag information: a `TAGS`
  column on history, a `REF` column on manifests that mounted by tag.
- **`ctx get` / `ctx inspect`** report a tagged resolve as `tagged (@<tag>
  -> snapshot …, policy not consulted)`.
- The HTTP source adapter records `etag` / `last_modified` in snapshot
  provenance when the origin sends them.
- `ctx.policy.decision` is the canonical attribute name for the value
  `ctx.freshness.status` also holds; the latter is kept for existing
  dashboards.
- Storage: migration `0002` (SQLite and Postgres) adds the `tags` table
  and a `ref` column on `manifest_entries` and `run_mounts`. It applies
  automatically when an existing store is opened — no manual step — and a
  test proves it upgrades a database created at `0001`.

### Fixed

- **`ctx replay` is strict**: it exits non-zero if any entry's bytes fail
  to reproduce their recorded SHA256, or if a blob is missing. There is no
  lenient mode; regression tests cover a corrupted blob, a missing blob,
  and an intact store.
- Evidence bundles carry each entry's `ref`, so a bundle records how a
  snapshot was chosen — descriptive only, never part of the Merkle leaf.

## [0.1.0] - 2026-08-19

The walking skeleton: the full resolve → manifest → diff → replay loop,
proven end-to-end against live infrastructure (milestones M1–M8).

### Added

- Core domain model — Source, Resource, Policy, Snapshot, Materialization,
  Manifest — with the resolve pipeline (`internal/resolver`), the
  run/mount/commit orchestrator (`internal/run`), byte-exact replay
  (`internal/replay`), and manifest diff (`internal/diff`).
- Source adapters: filesystem, GitHub, and HTTP.
- Freshness policies: `require_fresh`, `allow_stale`, `pinned`.
- Two storage backends behind one domain interface: embedded SQLite +
  local disk, and PostgreSQL + an S3-compatible object store.
- `ctx` CLI (`resource`, `get`, `inspect`, `history`, `run`, `manifest`,
  `diff`, `replay`) and the `ctxd` server, both driven through
  `internal/client` so embedded and client/server mode cannot drift.
- HTTP API with the `internal/wire` JSON contract, documented in
  [`docs/api.md`](docs/api.md).
- `docker compose up -d --build`: Postgres, MinIO, an OTel collector, and
  `ctxd` built from this repo's `Dockerfile`, healthy from a clean clone.
- OpenTelemetry tracing and metrics across the resolve pipeline, no-op
  without `OTEL_EXPORTER_OTLP_ENDPOINT`.
- TypeScript SDK (`@ctx/sdk`): typed client, no runtime dependencies.
- Security baseline: no plaintext credentials at rest, `${VAR}` header
  references resolved from ctxd's environment, response redaction
  (`internal/redact`), optional `--api-key` bearer auth, dependency
  scanning.
- Reference demo ([`examples/refund-agent`](examples/refund-agent)) and
  `internal/e2e` suites asserting `SHA256(original) == SHA256(replay)`
  over SQLite, over Postgres+MinIO, and over a real HTTP round-trip.
- CI: Go build/vet/gofmt/test, TypeScript SDK build/test, and a Docker
  Compose integration test including a CLI-driven rerun of the demo.
