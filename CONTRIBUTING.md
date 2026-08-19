# Contributing

## Prerequisites

- Go 1.26+
- Node 18+ (for `sdk/typescript`)
- Docker + Docker Compose (for the Postgres/MinIO/OTel-backed tests and the client/server mode)

## Build & test — Go

```bash
go build ./...
go vet ./...
gofmt -l .        # should print nothing
go test ./...
```

`go test ./...` is green with no external services running — the
Postgres/MinIO/OTel-backed tests skip themselves when their env vars are
unset. To run everything, including those:

```bash
docker compose up -d
export CTX_TEST_POSTGRES_DSN='postgres://ctx:ctx_dev_password@localhost:5434/ctx?sslmode=disable'
export CTX_TEST_MINIO_ENDPOINT=localhost:9000
export CTX_TEST_MINIO_ACCESS_KEY=ctxadmin
export CTX_TEST_MINIO_SECRET_KEY=ctx_dev_password_minio
go test ./...
```

## Build & test — TypeScript SDK

```bash
cd sdk/typescript
npm install
npm run build
npm test
```

`npm run example` additionally needs a running `ctxd` (`docker compose up
-d --build` from the repo root).

## Before opening a PR

- `go build ./... && go vet ./... && gofmt -l . && go test ./...` clean
- `cd sdk/typescript && npm run build && npm test` clean
- If you touched `docker-compose.yml`, `Dockerfile`, or anything `ctxd`
  reads at startup: `docker compose up -d --build` from a clean state
  (`docker compose down -v` first) and confirm all services report
  healthy
- New behavior gets a test. Bug fixes get a regression test.

## Code style

- Go: standard `gofmt`; match the existing package's error-wrapping and
  naming conventions rather than introducing new ones
- No new dependencies without a good reason — both the Go module and the
  SDK are deliberately minimal
- Comments explain *why*, not *what*: a hidden constraint, a workaround,
  a non-obvious invariant. Don't restate what well-named identifiers
  already say

## Reporting issues

Open an issue describing what you expected vs. what happened, including
`ctx`/`ctxd` version (git commit) and whether you're in embedded or
client/server mode.
