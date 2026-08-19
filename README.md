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

## Status

Implemented against the v0.1 launch Definition of Done: storage swap
(Postgres/MinIO), all three source adapters, the HTTP API + `ctxd` + CLI
`--server` mode, a `docker compose up` bring-up of the full stack (Postgres
+ MinIO + `ctxd`, `ctxd`'s image built from this repo's `Dockerfile`), and
OpenTelemetry tracing/metrics with a collector wired into Compose. Still
ahead: the TypeScript SDK, the security baseline (auth, credential
redaction, dependency scanning), and release docs/CI — see the project
plan for the full checklist.
