# Changelog

All notable changes to this project are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security

Remediation of the August 2026 pre-launch audit
([`docs/security-audit-2026-08.md`](docs/security-audit-2026-08.md)). Every
finding is closed except RP-07 (repository hygiene) and RP-15 (no in-process
TLS or rate limiting — now documented as a reverse-proxy deployment).

**⚠️ Breaking defaults for `readproofd`.** A resource definition tells the
server which file to read, which address to connect to, and which of its own
environment variables to send. All three now default to **deny**, and each is
opened by one explicit flag. Existing resources of these kinds stop resolving
on an upgraded server until the matching flag is set — the refusal is a `400`
naming the flag, at registration and at resolve. **The embedded `readproof`
CLI is unchanged**: it reads the operator's own files, with the operator's own
environment, as the operator.

- **`--filesystem-root <dir>`** (repeatable; env
  `READPROOFD_FILESYSTEM_ROOTS`, `,`- or path-separator-separated) —
  filesystem sources resolve only for files inside an allow-listed root, with
  symlinks resolved *before* the containment check. **With no root configured,
  filesystem sources are refused outright** (RP-01, High: previously a
  registered resource could read any file on the host).
- **`--header-env-allow NAME`** (repeatable; env
  `READPROOFD_HEADER_ENV_ALLOWLIST`, comma-separated) — `"${VAR}"` in an HTTP
  source header expands only for allow-listed names. **With none configured,
  no variable expands** (RP-02, High). `readproofd`'s own credentials stay
  refused even if allow-listed.
- **`--allow-private-sources`** (env `READPROOFD_ALLOW_PRIVATE_SOURCES=1`) —
  HTTP sources may not reach loopback, link-local (`169.254.169.254`
  included), private, CGNAT, unique-local, multicast or unspecified addresses.
  Checked at dial time on the resolved address (so DNS rebinding does not get
  past it) and on every redirect hop, chain capped at 5 (RP-04). Set this to
  restore the old behaviour on a trusted network; `docker-compose.yml` sets it,
  because that stack fetches from the host via `host.docker.internal`.

Also fixed:

- **RP-08** — the DSH plugin's `readproof_evidence_export --with-content` now
  respects `maxInlineBytes`, refusing past the cap rather than truncating (a
  cut bundle no longer matches its own Merkle root) and naming the CLI export
  in the error. New plugin config `filesystemRoots: string[]` passes
  `--filesystem-root` to a spawned `readproofd`.
- **RP-09 / RP-25** — the container runs as a non-root user (uid 65532) on
  `alpine:3.24`; `/var/lib/readproof` is created and chowned in the image so a
  mounted volume comes up writable.
- **RP-11** — `500` responses return a generic message plus a request id; the
  detail is logged server-side under the same id instead of being returned.
- **RP-12** — the Postgres DSN, and the password inside it, are scrubbed from
  startup errors; `readproofd` logs only host and database name.
- **RP-13** — `readproof` and `readproofd` warn when `--api-key` arrives on
  argv (visible in `ps`); prefer `READPROOF_API_KEY` / `READPROOFD_API_KEY`.
- **RP-14** — the data directory is created `0700`, and blobs, the SQLite
  database and exported evidence bundles `0600`.
- **RP-16 / RP-17** — `permissions: contents: read` on `ci.yml` and
  `dsh-plugin.yml`; every `uses:` in all five workflows pinned to a full
  commit SHA with the version in a comment.
- **RP-19 / RP-20 / RP-24** — `@readproof/sdk` gains a request timeout
  (`timeoutMs`, default 30s), a response size cap (`maxResponseBytes`, default
  16 MiB, refused not truncated), constructor validation of `endpoint`
  (absolute http/https), and error messages that truncate echoed bodies to
  ~512 characters.
- **RP-21** — the support-agent example validates a ticket id
  (`^[A-Za-z0-9._-]{1,64}$`) before it reaches a path or a run id.
- **RP-22** — the DSH plugin spawns `readproofd` with a minimal environment
  (process basics plus `READPROOF*`/`READPROOFD*`) instead of the full parent
  environment.
- **RP-23** — "tamper-evident" is qualified wherever it appeared unqualified:
  bundles are unsigned, so they are integrity-checked, and tamper-evident
  against the store rather than offline.

### Fixed

- `dsh-plugin-readproof` 0.3.2: the published manifest now depends on
  `@readproof/sdk@^0.3.1` (0.3.1 on npm carried a `file:` path because the
  prepack rewrite does not reach the manifest `npm publish` sends). In-repo
  development uses an npm `overrides` entry instead. 0.3.1 is deprecated.

## [0.3.1] - 2026-08-21

Release plumbing for the first public release. No runtime behaviour, wire
shape, or storage layout changes.

### Changed

- **Go module path** `readproof` → `github.com/fbzz/readproof`, so that
  `go install github.com/fbzz/readproof/cmd/readproof@latest` resolves.
  Every `"readproof/internal/…"` import follows, as does
  `-ldflags "-X github.com/fbzz/readproof/internal/version.Commit=…"`.
- **npm package versions.** `@readproof/sdk` and `dsh-plugin-readproof`
  both go 0.1.0 → 0.3.0, in step with `internal/version`. Both gain
  `repository`/`homepage`/`bugs`/`keywords`, `publishConfig.access:
  public`, and a LICENSE + NOTICE copy inside the package.

### Added

- **Release pipeline.** [`.goreleaser.yaml`](.goreleaser.yaml) and
  [`.github/workflows/release.yml`](.github/workflows/release.yml): a `v*`
  tag builds `readproof` and `readproofd` for linux/darwin/windows ×
  amd64/arm64, publishes checksummed archives and a grouped changelog, and
  pushes the Homebrew cask to `fbzz/homebrew-tap` (skipped, rather than
  failed, when `HOMEBREW_TAP_GITHUB_TOKEN` is absent).
- **npm publishing.**
  [`.github/workflows/publish-npm.yml`](.github/workflows/publish-npm.yml)
  publishes both packages on the same tag with `--provenance`, skipping any
  version already on the registry. The plugin's `file:` dependency on the
  SDK is rewritten to the published semver at pack time by
  `scripts/prepack.mjs`, and restored by `scripts/postpack.mjs`.
- **`docs/releasing.md`** — the whole procedure, including every secret the
  owner has to create.
- **MCP registry entry** in
  [`integrations/mcp-registry/`](integrations/mcp-registry/).
- **Repository hygiene.** Issue templates and chooser, a pull request
  template, and `CODE_OF_CONDUCT.md` (Contributor Covenant 2.1).
- **README Install section** — `go install`, `brew install
  fbzz/tap/readproof`, and the release archives.

## [0.3.0] - 2026-08-21

### Added

- `LICENSE` (Apache-2.0) and `NOTICE`.

Renamed Ctx → Readproof (breaking: module path, binaries, `readproof://`
scheme, env vars, MCP tool names, OTel names, evidence predicateType, npm
package/class names). Nothing else changed: no behaviour, no wire shapes,
no storage layout. The full mapping is in
[`docs/rename.md`](docs/rename.md).

### Changed

- **Go module** `ctx` → `readproof`; every import path follows, as does
  `-ldflags "-X github.com/fbzz/readproof/internal/version.Commit=…"`.
- **Binaries.** `ctx` → `readproof` and `ctxd` → `readproofd`: the cobra
  command name, the help text, the `readproof:` / `readproofd:` error
  prefixes, the Compose service, and the Docker image entrypoint.
- **URI scheme** `ctx://` → `readproof://`. Namespaces and paths are
  unchanged, but every stored URI string is not, so an existing data
  directory or database has to be re-registered rather than upgraded.
  Because the URI is part of the Merkle leaf, evidence bundles for the same
  documents now have different roots.
- **Environment variables.** `CTX_*` → `READPROOF_*` and `CTXD_*` →
  `READPROOFD_*` (`READPROOF_HOME`, `READPROOF_SERVER_URL`,
  `READPROOF_API_KEY`, `READPROOF_ENDPOINT`, `READPROOF_TEST_*`,
  `READPROOFD_*`). Default local data directory `.ctx` → `.readproof`.
- **MCP.** Server name `ctx` → `readproof`, every tool `ctx_*` →
  `readproof_*`, resource template `readproof://{namespace}/{+path}`.
  Harness allowlists move from `mcp__ctx__ctx_*` to
  `mcp__readproof__readproof_*`.
- **OpenTelemetry.** Tracer and meter name `ctx` → `readproof`, spans and
  attributes `ctx.*` → `readproof.*`, metrics `ctx_*` → `readproof_*`, and
  `gen_ai.data_source.id` is now `readproof://<namespace>`. Existing
  dashboards and alerts query names that no longer exist.
- **Evidence.** `predicateType` `urn:ctx:evidence:v0.2` →
  `urn:readproof:evidence:v0.3` and exporter name `ctx` → `readproof`, in
  both the Go and TypeScript exporters. A verifier pinned to the old URN
  rejects new bundles.
- **TypeScript SDK.** Package `@ctx/sdk` → `@readproof/sdk`, class `Ctx` →
  `Readproof`, `CtxOptions` → `ReadproofOptions`, `CtxError` →
  `ReadproofError`. Method names are unchanged.
- **DeepSeek Harness plugin.** `dsh-plugin-ctx` → `dsh-plugin-readproof`,
  plugin name `readproof`, default `toolPrefix` `readproof_`, overlays
  `readproof-mcp.cordis.yml` and `readproof-plugin.cordis.yml`,
  `__CTX_REPO__` → `__READPROOF_REPO__`.
- **Compose dev environment.** Database, user, MinIO root user, passwords,
  bucket, and volume names are all `readproof*` (`readproof`,
  `readproof_dev_password`, `readproofadmin`, `readproof-blobs`).

### Migration

1. `go build -o readproof ./cmd/readproof` and
   `go build -o readproofd ./cmd/readproofd`.
2. `mv ~/.ctx ~/.readproof`, then re-register resources: stored `ctx://`
   URIs are not rewritten.
3. Rename environment variables `CTX_*` → `READPROOF_*` and `CTXD_*` →
   `READPROOFD_*`; rename Compose credentials or keep the old ones by
   setting them explicitly in `.env`.
4. Swap `@ctx/sdk` for `@readproof/sdk` and `new Ctx(…)` for
   `new Readproof(…)`.
5. Re-add the MCP server under its new name
   (`claude mcp add readproof -- /abs/path/to/readproof mcp …`) and update
   tool allowlists.
6. Repoint OTel dashboards from `ctx.*` / `ctx_*` to `readproof.*` /
   `readproof_*`.
7. Update evidence verifiers to accept `urn:readproof:evidence:v0.3`.

## [0.2.0] - 2026-08-21

*(released under the name Ctx)*

The release that turns the v0.1 walking skeleton into something usable
from outside the repo: movable tags, diffs that explain themselves,
exportable evidence, GenAI-aligned traces, and an MCP server. Plan and
status: [`docs/mvp.md`](docs/mvp.md).

### Added

- **Tags and `@ref` resolution.** A tag is a named, movable pointer
  `(resource_uri, tag) → snapshot_id`. `readproof tag set|list|rm`, and any URI
  argument may carry a trailing `@<tag>` (`readproof get`, `readproof inspect`,
  `readproof run mount`, `readproof run --id`, `POST /v1/resolve`,
  `POST /v1/runs/mount`, SDK `resolve()`/`mount()`), which delivers exactly
  that snapshot: no source fetch, and the resource's freshness policy is
  not consulted (decision `use_tag`). An unknown tag is an error naming
  both the URI and the tag. Manifest and run-mount entries record the bare
  URI plus the `ref` they were mounted by, so moving a tag afterwards can
  never change what a committed manifest replays.
- **Tag endpoints**: `PUT /v1/tags`, `GET /v1/tags?uri=`,
  `DELETE /v1/tags?uri=&tag=`, with wire types and both `client.Client`
  implementations.
- **Evidence bundles** (`internal/evidence`, `readproof evidence`).
  `readproof evidence export <manifest-or-run> [--with-content] [--out f]`
  writes an in-toto Statement v1 whose subject digest is a Merkle root
  over the manifest's entries, carrying per-entry identity, redacted
  resource definitions, and a live replay check. `readproof evidence
  verify <bundle> [--offline]` recomputes the root, re-hashes embedded
  content, and cross-checks the store by replay, printing every check and
  exiting non-zero on failure. Documented in
  [`docs/evidence.md`](docs/evidence.md), including the EU AI Act Art. 12
  / SOC 2 framing and an explicit not-legal-advice note.
- **`internal/merkle`**: the single implementation of the manifest digest
  rule (leaf = `sha256(position_be_uint32 ‖ 0x00 ‖ uri ‖ 0x00 ‖
  content_hash)`, root over leaves in position order), shared by evidence
  export and the `readproof.run.commit` span, with fixed test vectors.
- **OpenTelemetry GenAI attributes and run spans.** `readproof.resolve`
  now carries `readproof.snapshot.content_hash`,
  `readproof.snapshot.source_revision`, `readproof.snapshot.observed_at`,
  `readproof.materialization.bytes`, `readproof.source.type`,
  `readproof.policy.strategy`, `readproof.policy.decision`,
  `readproof.resource.ref`, and `gen_ai.data_source.id` (=
  `readproof://<namespace>`). New spans `readproof.run.start`,
  `readproof.run.mount` (parenting that mount's `readproof.resolve` and
  `readproof.manifest.append`), `readproof.run.commit` (with
  `readproof.manifest.merkle_root`), and `readproof.tag.lookup`. New
  metrics `readproof_run_committed_total` and
  `readproof_tag_resolve_total`. Content is never attached to a span or
  metric; tests assert that. Full reference:
  [`docs/observability.md`](docs/observability.md).
- **TypeScript SDK**: `setTag` / `listTags` / `deleteTag`, `@tag` support
  in `resolve()` and `mount()`, per-side diff provenance fields, and
  `buildEvidence()` / `encodeEvidence()` / `merkleRoot()` / `merkleLeaf()`
  — a client-side bundle with the same Merkle root as `readproof evidence
  export`, using only `node:crypto`.
- **MCP server**: `readproof mcp` serves registered resources over stdio
  (`resources/list`, `resources/read` via the template
  `readproof://{namespace}/{+path}`, `@tag` honored, per-content `_meta`
  with snapshot id / content hash / source revision / observed-at /
  decision) and 13 tools (`readproof_resolve`, `readproof_run_*`,
  `readproof_manifest`, `readproof_diff`, `readproof_replay`,
  `readproof_tag_*`, `readproof_evidence_export`, …), reusing `--data-dir`
  / `--server` / `--api-key`. Official Go MCP SDK. See
  [`docs/mcp.md`](docs/mcp.md).
- **LangGraph.js example** ([`examples/langgraph-ts`](examples/langgraph-ts)):
  a two-node graph that mounts `readproof://` resources in a node, records the
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
- **`readproof diff` is provenance-aware.** Every changed entry gets a
  one-line `why:` before its unified diff — `source revision X → Y;
  observed T1 → T2`, plus `; ref <a> → <b>` when either side was mounted
  by tag. `diff.EntryDiff` and the wire/SDK types carry each side's
  `SourceRevision`, `ObservedAt`, and `Ref`.
- **`readproof history` and `readproof manifest`** show tag information:
  a `TAGS` column on history, a `REF` column on manifests that mounted by
  tag.
- **`readproof get` / `readproof inspect`** report a tagged resolve as
  `tagged (@<tag> -> snapshot …, policy not consulted)`.
- The HTTP source adapter records `etag` / `last_modified` in snapshot
  provenance when the origin sends them.
- `readproof.policy.decision` is the canonical attribute name for the value
  `readproof.freshness.status` also holds; the latter is kept for existing
  dashboards.
- Storage: migration `0002` (SQLite and Postgres) adds the `tags` table
  and a `ref` column on `manifest_entries` and `run_mounts`. It applies
  automatically when an existing store is opened — no manual step — and a
  test proves it upgrades a database created at `0001`.

### Fixed

- `readproof run commit` / `POST /v1/runs/commit` / MCP
  `readproof_run_commit` on a run that was never started used to succeed
  with an empty manifest; it now fails with `run: not found` (HTTP 404).
  Committing an already-committed run used to mint a second manifest; it
  now fails with `run: already committed` (HTTP 409). `run mount` applies
  the same guards before resolving, so a bogus run id can no longer create
  snapshots or orphan mount rows.
- Runtime errors no longer dump the cobra usage block (`readproof:
  <error>` once, non-zero exit); genuine usage errors (missing args,
  unknown flags) still show usage.
- `readproof version` / `readproof --version` / `readproofd --version`
  report a single `internal/version` source (`0.2.0`, `+<sha>` via
  `-ldflags -X github.com/fbzz/readproof/internal/version.Commit=…`); the evidence
  exporter and MCP server version strings read the same constant in Go and
  TS.


- **`readproof replay` is strict**: it exits non-zero if any entry's
  bytes fail to reproduce their recorded SHA256, or if a blob is missing.
  There is no lenient mode; regression tests cover a corrupted blob, a
  missing blob, and an intact store.
- Evidence bundles carry each entry's `ref`, so a bundle records how a
  snapshot was chosen — descriptive only, never part of the Merkle leaf.

## [0.1.0] - 2026-08-19

*(released under the name Ctx)*

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
- `readproof` CLI (`resource`, `get`, `inspect`, `history`, `run`, `manifest`,
  `diff`, `replay`) and the `readproofd` server, both driven through
  `internal/client` so embedded and client/server mode cannot drift.
- HTTP API with the `internal/wire` JSON contract, documented in
  [`docs/api.md`](docs/api.md).
- `docker compose up -d --build`: Postgres, MinIO, an OTel collector, and
  `readproofd` built from this repo's `Dockerfile`, healthy from a clean
  clone.
- OpenTelemetry tracing and metrics across the resolve pipeline, no-op
  without `OTEL_EXPORTER_OTLP_ENDPOINT`.
- TypeScript SDK (`@readproof/sdk`): typed client, no runtime dependencies.
- Security baseline: no plaintext credentials at rest, `${VAR}` header
  references resolved from readproofd's environment, response redaction
  (`internal/redact`), optional `--api-key` bearer auth, dependency
  scanning.
- Reference demo ([`examples/refund-agent`](examples/refund-agent)) and
  `internal/e2e` suites asserting `SHA256(original) == SHA256(replay)`
  over SQLite, over Postgres+MinIO, and over a real HTTP round-trip.
- CI: Go build/vet/gofmt/test, TypeScript SDK build/test, and a Docker
  Compose integration test including a CLI-driven rerun of the demo.
