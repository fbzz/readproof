# Architecture and reference

The long-form companion to the README: how the pieces fit, the full CLI and HTTP surface, the SDK, observability, and how the test suite proves the invariant. Product docs live beside this file (`api.md`, `mcp.md`, `evidence.md`, `observability.md`).

## Client/server mode (`readproofd`)

**⚠️ `docker-compose.yml`'s default credentials (Postgres, MinIO) are
dev-only placeholders — do not reuse them, or this file as-is, outside
local development.** Override via a `.env` file; see `.env.example`.

`docker compose up -d --build` brings up Postgres, MinIO, an OTel
collector, and `readproofd` itself (built from this repo's `Dockerfile`),
healthy, from a clean clone — no manual DB or bucket setup. Point the CLI
at it and every command behaves exactly as it does embedded:

```bash
docker compose up -d --build
curl http://localhost:8080/healthz             # -> ok
export READPROOF_SERVER_URL=http://localhost:8080    # or --server on any command
readproof resource add readproof://demo/hello-world \
  --source-type http --url https://raw.githubusercontent.com/octocat/Hello-World/master/README \
  --policy require_fresh
readproof get readproof://demo/hello-world
```

One real difference: **`readproofd` in a container has no access to your
host filesystem**, so a `filesystem` source only works there if the file
is baked into the image or volume-mounted; GitHub and HTTP sources are the
natural fit (`http://host.docker.internal:<port>/…` reaches a server on
the host). To run `readproofd` outside Compose, build `./cmd/readproofd`
and give it either `--data-dir` (embedded) or `--postgres-dsn` plus the
`--s3-*` flags — see the HTTP API section.

## Data model

Six immutable primitives and one mutable pointer:

- **Source** — physical origin (`internal/source`; filesystem, GitHub, HTTP).
- **Resource** — stable logical identity, `readproof://<namespace>/<path>` (`internal/resource`).
- **Policy** — freshness strategy: `require_fresh` | `allow_stale` | `pinned` (`internal/policy`).
- **Snapshot** — immutable observed state, content-addressed (`internal/snapshot`).
- **Materialization** — the byte form delivered to a consumer; raw/deterministic so far (`internal/materialization`).
- **Manifest** — the ordered, immutable record of everything resolved during a run (`internal/manifest`).
- **Tag** — the one mutable thing: a named pointer `(resource_uri, tag) → snapshot_id` (`internal/tag`), re-pointable at any time.

A **ref** is how a tag enters a run. `readproof://<ns>/<path>@<tag>`
resolves to exactly that snapshot; manifest and run-mount entries record
the bare URI *plus* the `ref` they were mounted by, so a manifest shows
how a snapshot was chosen while still replaying by snapshot and content
hash. Moving a tag afterwards can never change what a committed manifest
replays.

An **evidence bundle** is derived, not stored: an in-toto Statement built
from a manifest, its snapshots, its resource definitions (source config
redacted) and a live replay check, digested by a Merkle root over the
entries — the same bytes from the CLI and the TypeScript SDK
([`docs/evidence.md`](docs/evidence.md)).

`internal/resolver` is the resolution pipeline, `internal/run` the
run/mount/commit orchestrator, `internal/replay` and `internal/diff` the
consumers of a committed manifest, and `internal/merkle` the one
implementation of the manifest digest rule. Every store sits behind a
domain interface with two implementations — `storage/sqlite` +
`storage/blob` (embedded) and `storage/postgres` + `storage/s3blob`
(PostgreSQL + S3/MinIO) — and every `cmd/readproof` command is written
against `internal/client`, which has a `local` (in-process) and a `remote`
(HTTP) implementation, which is why the two modes can't drift.

## CLI

```
readproof resource add <uri> --source-type <filesystem|github|http> [flags] --policy <strategy>
readproof resource list
readproof get <uri>[@<tag>]
readproof inspect <uri>[@<tag>]
readproof history <uri>
readproof tag set <uri> <tag> <snapshot-id> / readproof tag list <uri> / readproof tag rm <uri> <tag>
readproof run start <run-id> / readproof run mount <run-id> <uri>[@<tag>] / readproof run commit <run-id>
readproof run --id <run-id> <uri1> <uri2> ...   # single-shot start+mount+commit
readproof manifest <manifest-or-run>
readproof diff <target-a> <target-b>
readproof replay <manifest-or-run>
readproof evidence export <manifest-or-run> [--with-content] [--out <file>]
readproof evidence verify <bundle.json> [--offline]
readproof mcp
```

Global flags: `--data-dir <path>` (embedded data directory), `--server
<url>` / `$READPROOF_SERVER_URL` (talk to a `readproofd` instead),
`--api-key` / `$READPROOF_API_KEY`.

**Tags and `@ref`.** Tag names match `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`.
Any command taking a URI also takes `readproof://<ns>/<path>@<tag>`, which
delivers exactly that snapshot: **no source fetch, and the resource's
freshness policy is not consulted** (resolve decision `use_tag`). An
unknown tag is an error naming both the URI and the tag. `readproof
history` grows a `TAGS` column, and `readproof manifest` a `REF` column
when a run mounted anything by tag:

```
$ readproof run --id run-c readproof://demo/policies/refunds@prod
$ readproof manifest run-c
POS  URI                                REF   SNAPSHOT          CONTENT_HASH
0    readproof://demo/policies/refunds  prod  snap_01M0GQH8K6…  sha256:c8b0bb212e93…
```

**Diff explains itself.** For every changed entry, `readproof diff` prints
one provenance line before the unified diff — `why: source revision X → Y;
observed T1 → T2`, plus `; ref <a> → <b>` when either side was mounted by
tag. **Replay is strict**: `readproof replay` exits non-zero if any
entry's bytes fail to reproduce their recorded SHA256, or if a blob is
missing. There is no lenient mode.

**Evidence.** `readproof evidence export` writes an in-toto Statement for
a manifest or run (`--with-content` embeds the bytes); `readproof evidence
verify` recomputes the Merkle root, re-hashes embedded content, and
cross-checks the store by replay (`--offline` skips that last part). Both
exit non-zero on failure and work embedded or with `--server`. Format,
Merkle rule, and what it does and doesn't prove:
[`docs/evidence.md`](docs/evidence.md).

**MCP.** `readproof mcp` runs a stdio MCP server: registered resources are
readable `readproof://` resources (`@tag` honored; each read carries
`_meta` with snapshot id, content hash, source revision, observed-at,
decision), and resolve / runs / manifest / diff / replay / tags / evidence
export are 13 tools, reusing the same `--data-dir` / `--server` /
`--api-key` flags as every other command. Claude Code, Claude Desktop, and
Cursor config snippets: [`docs/mcp.md`](docs/mcp.md).

## HTTP API (`readproofd`)

Full request/response schemas: [`docs/api.md`](docs/api.md).

```
Resources  POST /v1/resources · GET /v1/resources · GET /v1/resources/get?uri=
           GET /v1/resources/history?uri= · GET /v1/snapshots?id=
Tags       PUT /v1/tags · GET /v1/tags?uri= · DELETE /v1/tags?uri=&tag=
Resolve    POST /v1/resolve                                  (accepts uri@tag)
Runs       POST /v1/runs · POST /v1/runs/mount · POST /v1/runs/commit
Manifests  GET /v1/manifests?target= · GET /v1/diff?a=&b= · GET /v1/replay?target=
Health     GET /healthz                                   (never requires auth)
```

`readproofd` flags: `--addr`, `--data-dir` (embedded) or `--postgres-dsn`
plus `--s3-endpoint`/`--s3-access-key`/`--s3-secret-key`/`--s3-bucket`/`--s3-use-ssl`
(Postgres+S3) — also settable via `READPROOFD_*` env vars. `--api-key`
(`READPROOFD_API_KEY`) requires a matching `Authorization: Bearer <key>`
on every request except `/healthz`; off by default, and both the CLI and
the TS SDK send it when set. Evidence has no endpoint of its own — bundles
are composed from the calls above, so `readproof evidence` and the SDK's
`buildEvidence` need no new server surface.

## TypeScript SDK

`sdk/typescript` (`@readproof/sdk`) is a typed client for `readproofd`:
`resolve()`, `run({id}).mount()…commit()`,
`setTag`/`listTags`/`deleteTag`,
`registerResource`/`listResources`/`history`/`diff`/`replay`, and
`buildEvidence()` for client-side bundles that hash identically to the
CLI's. `@tag` refs work in `resolve()` and `mount()`; diff entries carry
per-side `source_revision_*`, `observed_at_*`, and `ref_*`. No runtime
dependencies (Node 18+ global `fetch`), no `any` in the public surface.
See [`sdk/typescript/README.md`](sdk/typescript/README.md).

```bash
cd sdk/typescript && npm install && npm run build && npm test
docker compose up -d --build   # from the repo root, for the example below
npm run example                # resolves a real URI against readproofd
```

## Observability

Run-level spans wrap the resolve tree: `readproof.run.start`,
`readproof.run.mount` (parenting that mount's `readproof.resolve` and
`readproof.manifest.append`), and `readproof.run.commit`, whose
`readproof.manifest.merkle_root` is the same digest `readproof evidence
export` signs — so a trace and an evidence bundle join on one field.
`readproof.resolve` carries the identity of what was delivered
(`readproof.snapshot.content_hash`, `readproof.snapshot.source_revision`,
`readproof.snapshot.observed_at`, `readproof.materialization.bytes`,
`readproof.source.type`, `readproof.policy.strategy`,
`readproof.policy.decision`) plus the OpenTelemetry GenAI attribute
`gen_ai.data_source.id` = `readproof://<namespace>`.
`readproof.policy.decision` is the canonical name for the value
`readproof.freshness.status` also holds. Two more metrics:
`readproof_run_committed_total`, `readproof_tag_resolve_total`. Full
tables, a worked trace, and the GenAI/OpenInference correlation proposal
are in [`docs/observability.md`](docs/observability.md).

Resolved content is never attached to spans or metrics — tests scan every
recorded attribute and event for the fixture's bytes and fail if they
appear. Set `OTEL_EXPORTER_OTLP_ENDPOINT` to export; unset, every
instrumentation call is a no-op, so no collector is required. `docker
compose up -d` already wires `readproofd` to one.

## Testing

```bash
go build ./... && go vet ./... && go test ./...
```

Live-infra tests skip themselves unless their env vars are set;
[`CONTRIBUTING.md`](CONTRIBUTING.md) has that block and the pre-PR
checklist. `internal/e2e/` runs the Refund Agent demo over embedded SQLite,
over Postgres+MinIO, and over a real HTTP round-trip through
`internal/api` + `internal/client/remote` — each asserting the SHA256
replay invariant, each mounting a `@prod` tag.

