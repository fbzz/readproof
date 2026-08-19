# Ctx

Infrastructure for delivering reliable, versioned, inspectable, reproducible
context to AI agents.

> Models are probabilistic, but many context failures are infrastructural.
> Agent reliability is bounded by context reliability.

This repository implements Ctx's core domain model — Source, Resource,
Snapshot, Materialization, Manifest, Policy — proving the resolve →
manifest → diff → replay loop end-to-end, both as an embedded CLI tool and
as a client/server system (`ctx` + `ctxd`) backed by Postgres and an
S3-compatible object store. See "Status" below for what's still ahead of a
full v0.1 launch.

## Quickstart — embedded mode

Requires Go 1.22+. No external services needed.

```bash
go build -o ctx ./cmd/ctx

./ctx resource add ctx://demo/policies/refunds \
  --source-type filesystem \
  --path examples/refund-agent/policies/refunds.md \
  --policy require_fresh

./ctx get ctx://demo/policies/refunds
```

For the full reference demo (register → resolve → diff → replay, proving
`SHA256(original) == SHA256(replay)`), see
[`examples/refund-agent/README.md`](examples/refund-agent/README.md).

## Quickstart — client/server mode

**⚠️ `docker-compose.yml`'s default credentials (Postgres, MinIO) are
dev-only placeholders — do not reuse them, or this file as-is, outside
local development.** Override via a `.env` file; see `.env.example`.

`docker compose up -d` brings up Postgres, MinIO, and `ctxd` itself
(building `ctxd`'s image from the repo's `Dockerfile`), healthy, from a
clean clone — no manual DB/bucket setup:

```bash
docker compose up -d --build
curl http://localhost:8080/healthz   # -> ok
```

Point the CLI at it:

```bash
go build -o ctx ./cmd/ctx
export CTX_SERVER_URL=http://localhost:8080   # or --server on any command

./ctx resource add ctx://demo/policies/refunds \
  --source-type http --url https://raw.githubusercontent.com/<owner>/<repo>/main/refunds.md \
  --policy require_fresh
./ctx get ctx://demo/policies/refunds
```

Every CLI command works identically in embedded and server mode — same
output, same semantics. One real difference: **`ctxd` running via Docker
Compose has no access to your host filesystem**, so a `filesystem` source
only works there if the file is baked into the image or volume-mounted;
GitHub and HTTP sources are the natural fit for a containerized `ctxd`
(for local host-side testing, `http://host.docker.internal:<port>/...`
reaches a server running on the host).

To run `ctxd` directly instead of via Compose (e.g. against Postgres/MinIO
you started separately, or in embedded mode by omitting `--postgres-dsn`):

```bash
go build -o ctxd ./cmd/ctxd
./ctxd --addr :8080 \
  --postgres-dsn 'postgres://ctx:ctx_dev_password@localhost:5434/ctx?sslmode=disable' \
  --s3-endpoint localhost:9000 --s3-access-key ctxadmin --s3-secret-key ctx_dev_password_minio
```

## Data model

Six domain primitives, one composition root:

- **Source** — physical origin (`internal/source`; filesystem, GitHub, and HTTP adapters).
- **Resource** — stable logical identity, `ctx://<namespace>/<path>` (`internal/resource`).
- **Policy** — freshness strategy: `require_fresh` | `allow_stale` | `pinned` (`internal/policy`).
- **Snapshot** — immutable observed state, content-addressed (`internal/snapshot`).
- **Materialization** — the representation delivered to a consumer; raw/deterministic only in v0.1 (`internal/materialization`).
- **Manifest** — the ordered, immutable record of everything resolved during a run (`internal/manifest`).

`internal/resolver` is the resolution pipeline; `internal/run` is the
run/mount/commit orchestrator standing in for the future SDK's
`ctx.run({id}).mount(uri)...commit()`; `internal/replay` reconstructs a
manifest's exact delivered bytes without touching the live source;
`internal/diff` computes the resolved-context difference between two
manifests. Every store is behind a domain interface with two
implementations:

- `internal/storage/sqlite` + `internal/storage/blob` — embedded SQLite + local disk.
- `internal/storage/postgres` + `internal/storage/s3blob` — PostgreSQL + an S3-compatible store (MinIO in dev).

`internal/app` is the composition root (`Open` for embedded, `OpenPostgres`
for Postgres+S3). `internal/client` defines the operations the CLI needs,
with a `local` implementation (direct in-process calls) and a `remote`
implementation (HTTP calls to `ctxd`) — every `cmd/ctx` command is written
against this interface, so it's identical either way. `internal/wire`
defines the JSON contract shared by `internal/api` (the server) and
`internal/client/remote` (the client).

## CLI

```
ctx resource add <uri> --source-type <filesystem|github|http> [flags] --policy <strategy>
ctx resource list
ctx get <uri>
ctx inspect <uri>
ctx history <uri>
ctx run start <run-id> / ctx run mount <run-id> <uri> / ctx run commit <run-id>
ctx run --id <run-id> <uri1> <uri2> ...   # single-shot start+mount+commit
ctx manifest <manifest-or-run>
ctx diff <target-a> <target-b>
ctx replay <manifest-or-run>
```

Global flags: `--data-dir <path>` (embedded mode data directory) and
`--server <url>` / `$CTX_SERVER_URL` (talk to a `ctxd` instead).

## HTTP API (`ctxd`)

```
POST /v1/resources              register a resource
GET  /v1/resources               list resources
GET  /v1/resources/get?uri=      get one resource
GET  /v1/resources/history?uri=  snapshot history
GET  /v1/snapshots?id=           get one snapshot
POST /v1/resolve                 resolve a uri
POST /v1/runs                    start a run
POST /v1/runs/mount               mount a uri into a run
POST /v1/runs/commit               commit a run into a manifest
GET  /v1/manifests?target=          get a manifest (by manifest id or run id)
GET  /v1/diff?a=&b=                  diff two manifests/runs
GET  /v1/replay?target=               replay a manifest/run
GET  /healthz
```

`ctxd` flags: `--addr`, `--data-dir` (embedded mode) or `--postgres-dsn` +
`--s3-endpoint`/`--s3-access-key`/`--s3-secret-key`/`--s3-bucket`/`--s3-use-ssl`
(Postgres+S3 mode) — also settable via `CTXD_*` env vars.

`--api-key` (`CTXD_API_KEY`) requires a matching `Authorization: Bearer
<key>` on every request except `/healthz`; off (unauthenticated) by
default. The CLI (`ctx --api-key` / `CTX_API_KEY`) and TS SDK
(`new Ctx({ apiKey })`) send it when set.

## TypeScript SDK

`sdk/typescript` (`@ctx/sdk`) is a typed client for `ctxd`'s HTTP API —
`ctx.resolve(uri)`, `ctx.run({ id }).mount(uri)...commit()`, plus
`registerResource`/`listResources`/`history`/`diff`/`replay`. No runtime
dependencies (Node 18+ global `fetch`), no `any` in the public surface. See
[`sdk/typescript/README.md`](sdk/typescript/README.md).

```bash
cd sdk/typescript && npm install && npm run build && npm test
docker compose up -d --build   # from the repo root, for the example below
npm run example                 # resolves a real URI against ctxd
```

## Observability

Every stage of the resolve pipeline is traced — span names match the
product spec: `ctx.resolve` (root), `ctx.resource.lookup`,
`ctx.policy.evaluate`, `ctx.cache.lookup`, `ctx.source.fetch`,
`ctx.snapshot.create`, `ctx.materialize`, `ctx.manifest.append`. Baseline
metrics: `ctx_resolve_total`/`_duration_seconds`/`_errors_total`,
`ctx_cache_hit_total`/`_miss_total`, `ctx_source_fetch_total`/
`_duration_seconds`/`_errors_total`, `ctx_snapshot_created_total`,
`ctx_materialization_created_total`, `ctx_manifest_created_total`.
Resolved content is never attached to spans or metrics.

Set `OTEL_EXPORTER_OTLP_ENDPOINT` to enable export (both `ctx` and `ctxd`
read it); unset, every instrumentation call is a safe no-op — no collector
required for normal use. `docker compose up -d` already wires `ctxd` to a
collector (`otel-collector`, logging received telemetry via its `debug`
exporter — see `otel-collector-config.yaml`); watch it with
`docker compose logs -f otel-collector`.

## Testing

```bash
go build ./...
go vet ./...
go test ./...
```

Live-infra tests (Postgres/MinIO storage backends, and the Postgres+MinIO
demo replay) are skipped unless their env vars are set:

```bash
docker compose up -d
export CTX_TEST_POSTGRES_DSN='postgres://ctx:ctx_dev_password@localhost:5434/ctx?sslmode=disable'
export CTX_TEST_MINIO_ENDPOINT=localhost:9000
export CTX_TEST_MINIO_ACCESS_KEY=ctxadmin
export CTX_TEST_MINIO_SECRET_KEY=ctx_dev_password_minio
go test ./...
```

`internal/e2e/` runs the full Refund Agent demo programmatically — over
embedded SQLite (`demo_test.go`), over Postgres+MinIO
(`demo_postgres_test.go`), and over a real HTTP round-trip through
`internal/api` + `internal/client/remote` (`demo_remote_test.go`) — each
asserting the SHA256 replay invariant.

## Security

This is a v0.1 baseline, not enterprise IAM.

- **No plaintext credentials at rest.** `GITHUB_TOKEN` is read from
  ctxd's own environment at fetch time and never stored. HTTP source
  headers support `"${VAR_NAME}"` references (e.g.
  `Authorization: Bearer ${GITHUB_TOKEN}`), resolved from ctxd's
  environment at fetch time — see `internal/source/http`. As
  defense-in-depth for headers supplied as raw values instead,
  `internal/redact` masks sensitive header values (`Authorization`,
  `Cookie`, anything matching `*token*`/`*key*`/`*secret*`/`*password*`/
  `*credential*`/`*auth*`) in every API response and in `ctx inspect` —
  see `internal/api/api_test.go`'s `TestHTTPHeaderCredentialsAreRedactedInResponses`.
- **Optional API auth.** `ctxd --api-key` (`CTXD_API_KEY`) requires a
  matching `Authorization: Bearer` on every request except `/healthz`;
  off by default. The CLI and TS SDK both support sending it.
- **Dev-only credentials, clearly labeled.** `docker-compose.yml`'s
  Postgres/MinIO credentials are placeholders, called out in this README
  and in the compose file itself; override via `.env` (`.env.example`
  documents every variable). `.env` is gitignored.
- **Dependency scanning.** `govulncheck ./...` and `npm audit`
  (`sdk/typescript`) are clean. One transitive advisory,
  [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) (`golang.org/x/crypto/openpgp`,
  unmaintained by design, no fix available): not imported by any package
  this module builds (`go mod why golang.org/x/crypto` confirms the main
  module doesn't need it) — a transitive `go.sum` entry only, reviewed
  and accepted.
- **SSRF**: the HTTP source adapter has no target-IP restrictions —
  acceptable while resource registration is only ever done by the
  operator running `ctx`/`ctxd`. This becomes a real requirement once
  `ctxd` accepts registration from less-trusted callers (noted in
  `internal/source/http`).

## Status

Implemented against the v0.1 launch Definition of Done: storage swap
(Postgres/MinIO), all three source adapters, the HTTP API + `ctxd` + CLI
`--server` mode, a `docker compose up` bring-up of the full stack (Postgres
+ MinIO + `ctxd`, `ctxd`'s image built from this repo's `Dockerfile`),
OpenTelemetry tracing/metrics with a collector wired into Compose, the
TypeScript SDK, and the security baseline (see "Security" above). Still
ahead: release docs/CI and tagging v0.1.0 — see the project plan for the
full checklist.
