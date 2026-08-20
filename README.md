# Ctx

**Ctx is the lockfile + replay primitive for what agents read.**

> Models are probabilistic, but many context failures are infrastructural.
> Agent reliability is bounded by context reliability.

Every external document, policy, or dataset an agent consumes gets three
things it usually doesn't have: a **stable identity**
(`ctx://<namespace>/<path>`, independent of where the bytes live), a
**freshness policy** (`require_fresh` | `allow_stale` | `pinned`, plus
movable `@tags` for "whatever we promoted to prod"), and a
**content-addressed snapshot** of the bytes actually delivered. Every run
is then recorded as an immutable **manifest** — the ordered list of what
was resolved, by content hash — which can be **diffed** against another run
with provenance (which source revision changed, and when), **replayed
byte-for-byte** without touching the live source, and **exported as
evidence**: an [in-toto
Statement](https://github.com/in-toto/attestation) whose subject digest is
a Merkle root over the run's entries, verifiable by whoever you hand the
file to.

## What it is, and what it isn't

Ctx is **not** a vector database, **not** an observability tool, **not** a
prompt registry, **not** a memory system. It sits underneath those:
whatever does the retrieving, prompting, or remembering, Ctx is the layer
that can say which exact bytes went in — and produce them again a year
later.

It's also not a competitor to install-time lockfiles. Microsoft's Agent
Package Manager, `skills-lock.json`, the "agent.lock" idea pin an agent's
*static configuration* — prompts, skills, MCP server versions — at install.
Ctx pins the *runtime* documents, per run: the refund policy the agent
actually read at 14:02 on Tuesday, at the revision it had then. Different
lifetimes, different locks; you want both.

It plugs into what you already run: **MCP** (`ctx mcp` serves Ctx resources
and tools over stdio to any MCP client — [`docs/mcp.md`](docs/mcp.md));
**OpenTelemetry** (GenAI-aligned attributes, and the run's Merkle root
appears in both the trace and the evidence bundle, so the two join on one
field); **LangGraph** ([`examples/langgraph-ts`](examples/langgraph-ts)
mounts `ctx://` URIs inside a graph node and stores the manifest id in the
checkpoint); and anything that speaks HTTP, via `ctxd`'s JSON API or the
TypeScript SDK. The CLI behaves identically embedded or against a server.

## Why now

Stale, unpinned context is a failure mode that shows up repeatedly in
production agent postmortems: the retrieval worked, the model was fine, the
document had changed underneath. Separately, the EU AI Act's Article 12
automatic-logging obligations for Annex III (high-risk) systems apply from
2 August 2026, and SOC 2 reviews increasingly ask a version of "what data
did the agent consider for this decision?" A manifest plus an evidence
bundle answers that concretely — content hashes, the policy in force, and a
reconstruction check that runs without the original source still being
reachable.

**Not legal advice.** Nothing Ctx produces makes a system compliant with
anything; what you owe depends on your system, role, and jurisdiction. See
[`docs/evidence.md`](docs/evidence.md) for the full framing.

## See the invariant — 60 seconds

Requires Go 1.26+. No external services: embedded mode keeps everything in
a local `.ctx` directory.

Register a document — identity, source, freshness policy — then run an
agent turn. `run` resolves each URI and commits a manifest:

```
$ go build -o ctx ./cmd/ctx
$ ctx resource add ctx://demo/policies/refunds \
    --source-type filesystem --path policies/refunds.md --policy require_fresh
Registered resource ctx://demo/policies/refunds
  source: filesystem
  policy: require_fresh

$ ctx run --id run-a ctx://demo/policies/refunds
Started run run-a
Mounted ctx://demo/policies/refunds -> snapshot snap_01M0GQH8K6… (position 0)
Committed manifest manifest_01M0GQH8K8… for run run-a (1 entry)
```

Promote what that run saw behind a movable tag, then let someone edit the
document and a later run pick up the change:

```
$ ctx tag set ctx://demo/policies/refunds prod snap_01M0GQH8K6…
Tagged ctx://demo/policies/refunds@prod -> snap_01M0GQH8K6…
  resolve it with: ctx get ctx://demo/policies/refunds@prod

$ printf 'Products can be refunded within 14 days.\n' > policies/refunds.md
$ ctx run --id run-b ctx://demo/policies/refunds
Started run run-b
Mounted ctx://demo/policies/refunds -> snapshot snap_01M0GQHP7V… (position 0)
Committed manifest manifest_01M0GQHP7W… for run run-b (1 entry)
```

Diff the two runs. The `why:` line is the provenance — which revision
moved, and when each side was observed:

```
$ ctx diff run-a run-b
--- run-a (manifest_01M0GQH8K8…)
+++ run-b (manifest_01M0GQHP7W…)

~ ctx://demo/policies/refunds  (snap_01M0GQH8K6… -> snap_01M0GQHP7V…)
  why: source revision sha256:c8b0bb212e93 → sha256:8f4b00474456; observed 2026-08-20T23:19:09Z → 2026-08-20T23:19:23Z
  --- a/ctx://demo/policies/refunds
  +++ b/ctx://demo/policies/refunds
  @@ -1,2 +1,2 @@
  -Products can be refunded within 30 days.
  +Products can be refunded within 14 days.

1 resource changed, 0 added, 0 removed, 0 unchanged
```

The tag still delivers the old bytes — no fetch, and `require_fresh` is
never consulted:

```
$ ctx get ctx://demo/policies/refunds@prod
uri:          ctx://demo/policies/refunds@prod
ref:          prod
snapshot:     snap_01M0GQH8K6…
content_hash: sha256:c8b0bb212e93…
freshness:    tagged (@prod -> snapshot snap_01M0GQH8K6…, observed 2026-08-20T23:19:09Z, policy not consulted)
…
--- content ---
Products can be refunded within 30 days.
```

Replay the first run against a source that no longer says this, then export
it as evidence and verify it the way an auditor would:

```
$ ctx replay run-a
Replaying manifest manifest_01M0GQH8K8… (run run-a), 1 entry

[0] ctx://demo/policies/refunds
    content_hash (recorded):  sha256:c8b0bb212e93…
    content_hash (replayed):  sha256:c8b0bb212e93…
    match: OK

--- content ---
Products can be refunded within 30 days.

Replay verified: SHA256 match for 1/1 entries.

$ ctx evidence export run-a --with-content --out bundle.json
evidence bundle written to bundle.json: 1 entry, merkle root 8482a7671b1d…

$ ctx evidence verify bundle.json
evidence verified: 1 entry, merkle root 8482a7671b1d…, embedded content 1/1 re-hashed, replay match 1/1
```

The same walkthrough against this repo's own fixture — plus the automated
version that runs in `go test ./...` — is in
[`examples/refund-agent/README.md`](examples/refund-agent/README.md).

## Client/server mode (`ctxd`)

**⚠️ `docker-compose.yml`'s default credentials (Postgres, MinIO) are
dev-only placeholders — do not reuse them, or this file as-is, outside
local development.** Override via a `.env` file; see `.env.example`.

`docker compose up -d --build` brings up Postgres, MinIO, an OTel
collector, and `ctxd` itself (built from this repo's `Dockerfile`), healthy,
from a clean clone — no manual DB or bucket setup. Point the CLI at it and
every command behaves exactly as it does embedded:

```bash
docker compose up -d --build
curl http://localhost:8080/healthz             # -> ok
export CTX_SERVER_URL=http://localhost:8080    # or --server on any command
ctx resource add ctx://demo/hello-world \
  --source-type http --url https://raw.githubusercontent.com/octocat/Hello-World/master/README \
  --policy require_fresh
ctx get ctx://demo/hello-world
```

One real difference: **`ctxd` in a container has no access to your host
filesystem**, so a `filesystem` source only works there if the file is
baked into the image or volume-mounted; GitHub and HTTP sources are the
natural fit (`http://host.docker.internal:<port>/…` reaches a server on the
host). To run `ctxd` outside Compose, build `./cmd/ctxd` and give it either
`--data-dir` (embedded) or `--postgres-dsn` plus the `--s3-*` flags — see
the HTTP API section below.

## Data model

Six immutable primitives and one mutable pointer:

- **Source** — physical origin (`internal/source`; filesystem, GitHub, HTTP).
- **Resource** — stable logical identity, `ctx://<namespace>/<path>` (`internal/resource`).
- **Policy** — freshness strategy: `require_fresh` | `allow_stale` | `pinned` (`internal/policy`).
- **Snapshot** — immutable observed state, content-addressed (`internal/snapshot`).
- **Materialization** — the byte form delivered to a consumer; raw/deterministic so far (`internal/materialization`).
- **Manifest** — the ordered, immutable record of everything resolved during a run (`internal/manifest`).
- **Tag** — the one mutable thing: a named pointer `(resource_uri, tag) → snapshot_id` (`internal/tag`), re-pointable at any time.

A **ref** is how a tag enters a run. `ctx://<ns>/<path>@<tag>` resolves to
exactly that snapshot; manifest and run-mount entries record the bare URI
*plus* the `ref` they were mounted by, so a manifest shows how a snapshot
was chosen while still replaying by snapshot and content hash. Moving a tag
afterwards can never change what a committed manifest replays.

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
(PostgreSQL + S3/MinIO) — and every `cmd/ctx` command is written against
`internal/client`, which has a `local` (in-process) and a `remote` (HTTP)
implementation, which is why the two modes can't drift.

## CLI

```
ctx resource add <uri> --source-type <filesystem|github|http> [flags] --policy <strategy>
ctx resource list
ctx get <uri>[@<tag>]
ctx inspect <uri>[@<tag>]
ctx history <uri>
ctx tag set <uri> <tag> <snapshot-id> / ctx tag list <uri> / ctx tag rm <uri> <tag>
ctx run start <run-id> / ctx run mount <run-id> <uri>[@<tag>] / ctx run commit <run-id>
ctx run --id <run-id> <uri1> <uri2> ...   # single-shot start+mount+commit
ctx manifest <manifest-or-run>
ctx diff <target-a> <target-b>
ctx replay <manifest-or-run>
ctx evidence export <manifest-or-run> [--with-content] [--out <file>]
ctx evidence verify <bundle.json> [--offline]
ctx mcp
```

Global flags: `--data-dir <path>` (embedded data directory), `--server
<url>` / `$CTX_SERVER_URL` (talk to a `ctxd` instead), `--api-key` /
`$CTX_API_KEY`.

**Tags and `@ref`.** Tag names match `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`.
Any command taking a URI also takes `ctx://<ns>/<path>@<tag>`, which
delivers exactly that snapshot: **no source fetch, and the resource's
freshness policy is not consulted** (resolve decision `use_tag`). An
unknown tag is an error naming both the URI and the tag. `ctx history`
grows a `TAGS` column, and `ctx manifest` a `REF` column when a run mounted
anything by tag:

```
$ ctx run --id run-c ctx://demo/policies/refunds@prod
$ ctx manifest run-c
POS  URI                          REF   SNAPSHOT          CONTENT_HASH
0    ctx://demo/policies/refunds  prod  snap_01M0GQH8K6…  sha256:c8b0bb212e93…
```

**Diff explains itself.** For every changed entry, `ctx diff` prints one
provenance line before the unified diff — `why: source revision X → Y;
observed T1 → T2`, plus `; ref <a> → <b>` when either side was mounted by
tag. **Replay is strict**: `ctx replay` exits non-zero if any entry's bytes
fail to reproduce their recorded SHA256, or if a blob is missing. There is
no lenient mode.

**Evidence.** `ctx evidence export` writes an in-toto Statement for a
manifest or run (`--with-content` embeds the bytes); `ctx evidence verify`
recomputes the Merkle root, re-hashes embedded content, and cross-checks
the store by replay (`--offline` skips that last part). Both exit non-zero
on failure and work embedded or with `--server`. Format, Merkle rule, and
what it does and doesn't prove: [`docs/evidence.md`](docs/evidence.md).

**MCP.** `ctx mcp` runs a stdio MCP server: registered resources are
readable `ctx://` resources (`@tag` honored; each read carries `_meta`
with snapshot id, content hash, source revision, observed-at, decision),
and resolve / runs / manifest / diff / replay / tags / evidence export are
13 tools, reusing the same `--data-dir` / `--server` / `--api-key` flags
as every other command. Claude Code, Claude Desktop, and Cursor config
snippets: [`docs/mcp.md`](docs/mcp.md).

## HTTP API (`ctxd`)

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

`ctxd` flags: `--addr`, `--data-dir` (embedded) or `--postgres-dsn` +
`--s3-endpoint`/`--s3-access-key`/`--s3-secret-key`/`--s3-bucket`/`--s3-use-ssl`
(Postgres+S3) — also settable via `CTXD_*` env vars. `--api-key`
(`CTXD_API_KEY`) requires a matching `Authorization: Bearer <key>` on every
request except `/healthz`; off by default, and both the CLI and the TS SDK
send it when set. Evidence has no endpoint of its own — bundles are
composed from the calls above, so `ctx evidence` and the SDK's
`buildEvidence` need no new server surface.

## TypeScript SDK

`sdk/typescript` (`@ctx/sdk`) is a typed client for `ctxd`: `resolve()`,
`run({id}).mount()…commit()`, `setTag`/`listTags`/`deleteTag`,
`registerResource`/`listResources`/`history`/`diff`/`replay`, and
`buildEvidence()` for client-side bundles that hash identically to the
CLI's. `@tag` refs work in `resolve()` and `mount()`; diff entries carry
per-side `source_revision_*`, `observed_at_*`, and `ref_*`. No runtime
dependencies (Node 18+ global `fetch`), no `any` in the public surface.
See [`sdk/typescript/README.md`](sdk/typescript/README.md).

```bash
cd sdk/typescript && npm install && npm run build && npm test
docker compose up -d --build   # from the repo root, for the example below
npm run example                # resolves a real URI against ctxd
```

## Observability

Run-level spans wrap the resolve tree: `ctx.run.start`, `ctx.run.mount`
(parenting that mount's `ctx.resolve` and `ctx.manifest.append`), and
`ctx.run.commit`, whose `ctx.manifest.merkle_root` is the same digest `ctx
evidence export` signs — so a trace and an evidence bundle join on one
field. `ctx.resolve` carries the identity of what was delivered
(`ctx.snapshot.content_hash`, `ctx.snapshot.source_revision`,
`ctx.snapshot.observed_at`, `ctx.materialization.bytes`, `ctx.source.type`,
`ctx.policy.strategy`, `ctx.policy.decision`) plus the OpenTelemetry GenAI
attribute `gen_ai.data_source.id` = `ctx://<namespace>`.
`ctx.policy.decision` is the canonical name for the value
`ctx.freshness.status` also holds. Two more metrics:
`ctx_run_committed_total`, `ctx_tag_resolve_total`. Full tables, a worked
trace, and the GenAI/OpenInference correlation proposal are in
[`docs/observability.md`](docs/observability.md).

Resolved content is never attached to spans or metrics — tests scan every
recorded attribute and event for the fixture's bytes and fail if they
appear. Set `OTEL_EXPORTER_OTLP_ENDPOINT` to export; unset, every
instrumentation call is a no-op, so no collector is required. `docker
compose up -d` already wires `ctxd` to one.

## Docs

| | |
| --- | --- |
| [`docs/api.md`](docs/api.md) | `ctxd`'s HTTP API, endpoint by endpoint |
| [`docs/mcp.md`](docs/mcp.md) | `ctx mcp`: resources, tools, client config |
| [`docs/evidence.md`](docs/evidence.md) | Bundle format, Merkle rule, what it proves |
| [`docs/observability.md`](docs/observability.md) | Spans, attributes, metrics, GenAI mapping |
| [`docs/roadmap.md`](docs/roadmap.md) | What's next, in priority order |
| [`docs/mvp.md`](docs/mvp.md) | The v0.2 work-package plan and its status |
| [`examples/refund-agent`](examples/refund-agent) | Reference walkthrough of the replay invariant |
| [`examples/langgraph-ts`](examples/langgraph-ts) | LangGraph.js: manifest id in the checkpoint, replay from it |

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

## Security

A baseline, not enterprise IAM.

- **No plaintext credentials at rest.** `GITHUB_TOKEN` and HTTP source
  headers of the form `"${VAR_NAME}"` are resolved from ctxd's own
  environment at fetch time, never stored (`internal/source/http`). For
  headers supplied as raw values instead, `internal/redact` masks sensitive
  values in every API response, in `ctx inspect`, and in evidence bundles —
  including in embedded mode, where they never crossed a wire.
- **Optional API auth.** `ctxd --api-key` (`CTXD_API_KEY`); off by default.
- **Dev-only credentials, clearly labeled.** `docker-compose.yml`'s
  Postgres/MinIO credentials are placeholders; override via `.env`
  (`.env.example` documents every variable, and `.env` is gitignored).
- **Dependency scanning.** `govulncheck ./...` and `npm audit`
  (`sdk/typescript`) are clean. One transitive advisory,
  [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932)
  (`golang.org/x/crypto/openpgp`, unmaintained by design, no fix): not
  imported by any package this module builds — a `go.sum` entry only,
  reviewed and accepted.
- **SSRF**: the HTTP source adapter has no target-IP restrictions —
  acceptable while registration is only ever done by the operator running
  `ctx`/`ctxd`, a real requirement once `ctxd` accepts registration from
  less-trusted callers ([`docs/roadmap.md`](docs/roadmap.md)).

## Status

**v0.2.0 (2026-08-21).** New since v0.1.0: tags and `@ref` resolution
(`use_tag`; policy not consulted) across CLI, HTTP API, and SDK;
provenance-aware `ctx diff`; strict `ctx replay`; evidence bundles (`ctx
evidence export/verify`, `buildEvidence` in the SDK, a Merkle root shared
with the trace); OpenTelemetry GenAI attributes, run-level spans, and two
new metrics; the LangGraph.js example; and an MCP server (`ctx mcp`). Full
list in [`CHANGELOG.md`](CHANGELOG.md). Storage migration `0002` applies
automatically on open, for both SQLite and Postgres.

CI (`.github/workflows/ci.yml`) runs Go build/vet/gofmt/test, the TS SDK's
build and tests, the LangGraph example's build, and a Docker Compose
integration test — including a CLI-driven rerun of the reference demo
against the built `ctxd` image — on every push and PR. No LICENSE yet; this
is a private-repo checkpoint, not a public release, and the name is a
placeholder that will change before launch.
