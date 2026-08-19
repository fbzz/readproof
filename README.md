# Ctx

Infrastructure for delivering reliable, versioned, inspectable, reproducible
context to AI agents.

> Models are probabilistic, but many context failures are infrastructural.
> Agent reliability is bounded by context reliability.

This repository is the **walking skeleton**: a CLI-only implementation of
Ctx's core domain model — Source, Resource, Snapshot, Materialization,
Manifest, Policy — proving the resolve → manifest → diff → replay loop
end-to-end. It is not yet the full v0.1 MVP described in the product spec
(no HTTP API, TypeScript SDK, Postgres/Redis/MinIO, Docker Compose, or
OpenTelemetry — see "Status" below).

## Quickstart

Requires Go 1.22+.

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

## Data model

Six domain primitives, one composition root:

- **Source** — physical origin (`internal/source`; filesystem and GitHub adapters).
- **Resource** — stable logical identity, `ctx://<namespace>/<path>` (`internal/resource`).
- **Policy** — freshness strategy: `require_fresh` | `allow_stale` | `pinned` (`internal/policy`).
- **Snapshot** — immutable observed state, content-addressed (`internal/snapshot`).
- **Materialization** — the representation delivered to a consumer; raw/deterministic only in v0.1 (`internal/materialization`).
- **Manifest** — the ordered, immutable record of everything resolved during a run (`internal/manifest`).

`internal/resolver` is the resolution pipeline; `internal/run` is the
CLI-only run/mount/commit orchestrator standing in for the future SDK's
`ctx.run({id}).mount(uri)...commit()`; `internal/replay` reconstructs a
manifest's exact delivered bytes without touching the live source.
`internal/storage/sqlite` and `internal/storage/blob` are the local
metadata/blob stores — both behind interfaces so Postgres/S3
implementations are drop-in swaps later.

## CLI

```
ctx resource add <uri> --source-type <filesystem|github> [flags] --policy <strategy>
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

## Testing

```bash
go build ./...
go vet ./...
go test ./...
```

`internal/e2e/demo_test.go` runs the full Refund Agent demo programmatically
and asserts the SHA256 replay invariant as a real test.

## Status

This is the walking-skeleton milestone. See the project plan for the full
Definition of Done targeting a public v0.1 launch (HTTP Resolve API,
TypeScript SDK, Postgres/Redis/MinIO via Docker Compose, OpenTelemetry,
security baseline, and docs) — every store here is behind a Go interface
specifically so that work extends this code rather than replacing it.
