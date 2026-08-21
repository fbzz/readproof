## What this changes

<!-- One or two sentences. If it fixes an issue: "Fixes #123". -->

## Why

<!-- The constraint or failure that made this necessary. Reviewers read this
     first; it is usually the part that cannot be recovered from the diff. -->

## Checklist

Mirrors [`CONTRIBUTING.md`](../CONTRIBUTING.md). Tick what applies; strike out
what does not, with a word on why.

- [ ] `go build ./... && go vet ./... && gofmt -l . && go test ./...` is clean
      with no external services running
- [ ] `cd sdk/typescript && npm ci && npm run build && npm test` is clean
- [ ] Touched the SDK's public surface? Then also
      `cd examples/langgraph-ts && npm ci && npm run build`,
      `cd examples/support-agent && npm ci && npm run build && SUPPORT_FAKE_MODEL=1 npm test`,
      and `cd integrations/deepseek-harness/dsh-plugin-readproof && npm ci && npm test`
      — all three consume `@readproof/sdk` as a `file:` dependency
- [ ] Touched `docker-compose.yml`, `Dockerfile`, or anything `readproofd`
      reads at startup? Then `docker compose down -v && docker compose up -d --build`
      from clean, with every service healthy
- [ ] New behavior has a test; a bug fix has a regression test
- [ ] Docs updated — `README.md`, `docs/`, and the relevant package README
- [ ] `CHANGELOG.md` has a line under `## Unreleased`
- [ ] No new dependency, or the PR says why one was unavoidable

## Anything reviewers should look at closely

<!-- A trade-off you are unsure about, a shape you would like a second opinion
     on, a follow-up you deliberately left out. -->
