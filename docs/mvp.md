# MVP plan — v0.2 "public + pluggable"

Status page for the work that turns the v0.1 walking skeleton into a
minimum viable product. Planned 2026-08-20 from the landscape review
(closest neighbors: ContextNest, Microsoft APM, Laminar, lakeFS for
Agentic AI — none combine identity + policy + snapshot + manifest + diff +
replay over external sources). Rename happens before launch, not here.

**MVP = all four work packages below green, `go test ./...` + SDK tests +
Compose CI passing, README repositioned, tagged v0.2.0.** Out of scope for
MVP: more source adapters, a UI, enterprise IAM, non-raw materializations,
the rename, LICENSE choice (owner decision).

## WP-A — Model: tags, provenance-aware diff, strict replay  (core Go + SDK)

- [x] **Tags**: named, movable pointers `(resource_uri, tag) → snapshot_id`.
  - `ctx tag set <uri> <tag> <snapshot-id>` / `ctx tag list <uri>` / `ctx tag rm <uri> <tag>`
  - URI suffix `ctx://ns/path@<tag>` accepted by `get`, `inspect`, `run mount`,
    `run --id`, `/v1/resolve`, `/v1/runs/mount`, SDK `resolve()`/`mount()`:
    resolves to the tagged snapshot **without fetching** (decision `use_tag`),
    policy not consulted. Unknown tag → clear error.
  - Manifest/run-mount entries record the bare URI plus a new optional `ref`
    (the `@tag` used, "" otherwise). Replay/diff unaffected by ref.
  - Storage: migration `0002` for sqlite and postgres (`tags` table;
    `ref` column on `manifest_entries` and `run_mounts`). Both backends.
  - API: `PUT /v1/tags`, `GET /v1/tags?uri=`, `DELETE /v1/tags?uri=&tag=`;
    wire types; `client.Client` + local + remote; TS SDK `setTag/listTags/deleteTag`.
- [x] **Provenance-aware diff**: `diff.EntryDiff` carries each side's
  `SourceRevision`, `ObservedAt`, `Ref`; `ctx diff` prints a one-line "why"
  for changed entries (`source revision X → Y, observed T1 → T2`); wire +
  SDK types extended. HTTP adapter records `etag`/`last_modified` in
  snapshot provenance when present (verify; add if missing).
- [x] **Strict replay**: `ctx replay` exits non-zero on any mismatch or
  missing blob (verify current behavior; add `--strict` only if it doesn't).
- [x] Tests: storage (both backends), resolver tag path, API round-trip,
  e2e demo extended with a `@prod` tag mounted in a run; SDK tests.
- [x] Docs: README CLI/API sections, `docs/api.md`.

## WP-B — MCP server  (`ctx mcp`)

- [x] `ctx mcp` subcommand: stdio MCP server via the official Go SDK
  (`github.com/modelcontextprotocol/go-sdk`), built on `client.Client` so it
  works embedded (`--data-dir`) or against `ctxd` (`--server`).
- [x] Resources: `resources/list` = registered resources as `ctx://` URIs
  (+ resource template `ctx://{namespace}/{path}`); `resources/read` =
  resolve (policy + `@tag` honored), text or base64 blob, with `_meta`
  `{snapshot_id, content_hash, source_revision, observed_at, decision}`.
- [x] Tools: `ctx_resolve`, `ctx_run_start`, `ctx_run_mount`,
  `ctx_run_commit`, `ctx_manifest`, `ctx_diff`, `ctx_replay`, `ctx_history`,
  `ctx_resources_list`, `ctx_evidence_export` — JSON-schema'd inputs, JSON results.
- [x] Tests: in-process SDK client over in-memory transport: list → read →
  run start/mount/commit → manifest → replay.
- [x] Docs: `docs/mcp.md` with Claude Code (`claude mcp add …`), Claude
  Desktop, and Cursor config snippets; README section.

## WP-C — OpenTelemetry GenAI attributes

- [x] `ctx.resolve` span: `ctx.resource.uri`, `ctx.resource.ref`,
  `ctx.policy.strategy`, `ctx.policy.decision`, `ctx.snapshot.id`,
  `ctx.snapshot.content_hash`, `ctx.snapshot.source_revision`,
  `ctx.source.type`, `gen_ai.data_source.id` (= `ctx://<namespace>`).
- [x] Run spans: `ctx.run.mount` (`ctx.run.id`, position) and
  `ctx.run.commit` (`ctx.run.id`, `ctx.manifest.id`, entry count, Merkle root).
- [x] Never attach content. Tests assert attributes via an in-memory exporter.
- [x] `docs/observability.md`: attribute table + the proposal to carry
  `hash/version/uri` on `gen_ai.retrieval.documents` / OpenInference
  `document.metadata`.

## WP-D — Evidence export  (`ctx evidence`)

- [x] `internal/evidence`: `Build(ctx, client, target, opts) → Bundle`,
  composed purely from existing client calls (manifest, snapshots,
  resources, replay) — no new storage or wire types.
- [x] Bundle = in-toto Statement v1 shape: `_type`, `subject` =
  `{name: manifest_id, digest: {sha256: <merkle root>}}`, `predicateType`
  (placeholder URN, updated at rename), `predicate` = manifest + per-entry
  `{position, uri, ref, snapshot_id, content_hash, source_revision,
  observed_at, content_type, bytes, provenance, materialization_id}` +
  resource definitions (redacted source config, policy) + replay
  verification + `generated_at` + exporter version; `--with-content` embeds
  base64 bytes per entry.
- [x] Merkle root: leaf = sha256(position ‖ uri ‖ content_hash), root over
  leaves in position order; documented, deterministic, tested.
- [x] `ctx evidence export <manifest-or-run> [--out f] [--with-content]`,
  `ctx evidence verify <bundle>` (recompute root; re-hash embedded content;
  if a store is reachable, cross-check via replay). Non-zero exit on failure.
- [x] TS SDK: `evidence(target, {withContent})` composed client-side.
- [x] `docs/evidence.md`: what the bundle proves / doesn't, EU AI Act Art.
  12 and SOC 2 "what did the agent see" framing, explicit not-legal-advice note.

## WP-E — LangGraph example  (`examples/langgraph-ts`)

- [x] Graph with a node that `run.start/mount/commit`s `ctx://` policy docs,
  calls a model (fake in-memory model by default, real one if API key set),
  stores `ctx_manifest_id` in checkpoint metadata; a second script replays
  from a checkpoint's manifest id and asserts identical bytes.
- [x] Pinned deps; README. (CI build step → WP-F)

## WP-F — Release polish

- [ ] README: reposition as "lockfile + replay primitive for what agents
  read" (complement to APM/skills-lock for static config); MCP/evidence/
  tags/OTel sections; `docs/roadmap.md`; CHANGELOG; CI covers new tests;
  cold-clone dogfood; tag v0.2.0.

## Rounds

1. WP-A ∥ WP-D ∥ WP-E (disjoint files; D/E touch no shared interface)
2. WP-B ∥ WP-C (after A lands, so MCP can expose tags/evidence)
3. WP-F, full verification, tag
