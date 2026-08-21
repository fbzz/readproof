# Rename: Ctx → Readproof

The product was renamed in v0.3.0. This page is the complete mapping, the
reason each line is a breaking change, and a ready-to-run script for the
one place the rename could not reach from this repository: the marketing
site (`site/`, on the `website` branch).

Everything here is a *name* change. No behaviour, wire shape, storage
layout, or algorithm changed with it. The one derived consequence worth
knowing: the resource URI is part of the Merkle leaf, so an evidence
bundle over the same documents now digests to a different root.

## The mapping

| Old | New | Where |
| --- | --- | --- |
| Product name `Ctx` / `ctx` (prose, titles, headings) | `Readproof` / `readproof` | every doc, README, page title |
| Go module `ctx`, imports `"ctx/internal/…"` | `readproof`, `"readproof/internal/…"` | `go.mod`, every `.go` file, `-ldflags "-X readproof/internal/version.Commit=…"` |
| CLI binary `ctx` (`cmd/ctx`) | `readproof` (`cmd/readproof`) | cobra `Use`, help text, version line, error prefix `readproof:`, every `./ctx …` in docs |
| Server `ctxd` (`cmd/ctxd`) | `readproofd` (`cmd/readproofd`) | Dockerfile, Compose service, image entrypoint, `readproofd:` error prefixes in `internal/client/remote` |
| URI scheme `ctx://` | `readproof://` | `resource.ParseURI`/`SplitRef` prefix and error messages, every doc, example, fixture and test |
| MCP resource template `ctx://{namespace}/{+path}` | `readproof://{namespace}/{+path}` | `internal/mcp` |
| `CTX_HOME`, `CTX_SERVER_URL`, `CTX_API_KEY`, `CTX_ENDPOINT`, `CTX_TEST_*` | `READPROOF_HOME`, `READPROOF_SERVER_URL`, `READPROOF_API_KEY`, `READPROOF_ENDPOINT`, `READPROOF_TEST_*` | code, docs, Compose, `.env.example`, CI, examples, plugin |
| `CTXD_*` (`CTXD_ADDR`, `CTXD_POSTGRES_DSN`, `CTXD_S3_*`, `CTXD_API_KEY`, `CTXD_BIN`) | `READPROOFD_*` | same |
| Data dir `.ctx` | `.readproof` | `internal/app.DataDir`, `--data-dir` defaults, docs, `.gitignore` |
| SQLite file `ctx.db` | `readproof.db` | `internal/app` |
| Compose db/user `ctx`, `ctx_dev_password`, `ctx_dev_password_minio`, `ctxadmin`, bucket `ctx-blobs`, volumes `ctx_*_data` | `readproof`, `readproof_dev_password`, `readproof_dev_password_minio`, `readproofadmin`, `readproof-blobs`, `readproof_*_data` | `docker-compose.yml`, `.env.example`, CI integration job, Postgres test DSNs |
| MCP server name `ctx`, tools `ctx_*` | `readproof`, `readproof_*` | `internal/mcp`, `docs/mcp.md`, DSH plugin |
| `claude mcp add ctx -- … ctx mcp` | `claude mcp add readproof -- … readproof mcp` | `docs/mcp.md` |
| Harness tool ids `mcp__ctx__ctx_*` | `mcp__readproof__readproof_*` | DSH MCP overlay |
| OTel tracer/meter name `"ctx"`, spans `ctx.resolve`, `ctx.resource.lookup`, `ctx.policy.evaluate`, `ctx.cache.lookup`, `ctx.source.fetch`, `ctx.snapshot.create`, `ctx.materialize`, `ctx.manifest.append`, `ctx.tag.lookup`, `ctx.run.start/mount/commit` | `"readproof"`, `readproof.*` | `internal/telemetry`, `internal/resolver`, `internal/run`, `docs/observability.md` |
| OTel attributes `ctx.resource.*`, `ctx.snapshot.*`, `ctx.policy.*`, `ctx.source.type`, `ctx.cache.hit`, `ctx.materialization.*`, `ctx.manifest.*`, `ctx.run.id`, `ctx.freshness.status` | `readproof.*` | same |
| Metrics `ctx_resolve_total`, `ctx_run_committed_total`, `ctx_tag_resolve_total`, … | `readproof_*` | `internal/telemetry/metrics.go` |
| `gen_ai.data_source.id` value `ctx://<ns>` | `readproof://<ns>` | `internal/resolver` |
| Evidence `predicateType` `urn:ctx:evidence:v0.2` | `urn:readproof:evidence:v0.3` | `internal/evidence`, `sdk/typescript/src/evidence.ts`, `docs/evidence.md` |
| Evidence exporter name `ctx` | `readproof` | Go + TS |
| npm package `@ctx/sdk` | `@readproof/sdk` | SDK, examples, plugin (`file:` deps and lockfiles) |
| TS class `Ctx`, `CtxOptions`, `CtxError` | `Readproof`, `ReadproofOptions`, `ReadproofError` | SDK and every consumer. **Method names are unchanged.** |
| TS client variable `ctx` in examples/docs | `rp` | so it is never confused with Go's `context.Context` or Cordis's `ctx` |
| `integrations/deepseek-harness/dsh-plugin-ctx` | `…/dsh-plugin-readproof` | package name, plugin `name`, default `toolPrefix` `readproof_` |
| `ctx-mcp.cordis.yml`, `ctx-plugin.cordis.yml` | `readproof-mcp.cordis.yml`, `readproof-plugin.cordis.yml` | plus `__CTX_REPO__` → `__READPROOF_REPO__`, row ids and `serverName` |
| Example packages `ctx-support-agent-example`, `ctx-langgraph-example` | `readproof-*-example` | `package.json`, lockfiles |
| Version `0.2.0` | `0.3.0` | `internal/version`, `EVIDENCE_EXPORTER_VERSION`, `docs/evidence.md` |

## What deliberately did *not* change

- **Go's `ctx context.Context`** parameters and variables, and every
  `ctx.Done()` / `ctx.Err()` / `context.Background()`. Only quoted
  strings, module and file paths, and cobra/flag names moved.
- **The Cordis `ctx: Context` parameter** in the DSH plugin —
  `apply(ctx, config)`, `ctx.tools.register`, `ctx.on(…)`, `ctx.get(…)`,
  `ctx.logger(…)`. That is the harness context, not this product.
- **The word "context"** everywhere: "context engineering", the
  `Context` type, and `examples/support-agent/context/policies/`, which
  keeps its directory name.
- **Go package names under `internal/`** — none of them was `ctx`.
- **Session run ids** in the DSH plugin (`dsh-<id>`).

## Applying the same rename to `site/`

`site/` does not exist on `main`; it lives on the `website` branch and was
therefore out of scope here. The landing page, the docs HTML, and
`BRIEF.md` use the same vocabulary, so the same substitutions apply.

Run this from the repository root **with the `website` branch checked
out**. Longest match first is what makes it safe: `ctx://` before `ctx`,
`ctxd` before `ctx`, `CTXD_` before `CTX_`, `@ctx/sdk` before `Ctx`.

Save the substitutions as `rename-site.pl`:

```perl
s{urn:ctx:evidence:v0\.2}{urn:readproof:evidence:v0.3}g;
s{urn:ctx}{urn:readproof}g;
s{ctx://}{readproof://}g;
s{ctx:\\/\\/}{readproof:\\/\\/}g;       # escaped, in JS regexes / JSON
s{mcp__ctx__ctx_}{mcp__readproof__readproof_}g;
s{mcp__ctx__}{mcp__readproof__}g;
s{\@ctx/sdk}{\@readproof/sdk}g;
s{CTXD_}{READPROOFD_}g;
s{CTX_}{READPROOF_}g;
s{\bctxadmin\b}{readproofadmin}g;
s{\bctx-blobs\b}{readproof-blobs}g;
s{\bctxd}{readproofd}g;                 # also ctxdPath, ctxdBin, …
s{\bctx_(?=[a-z])}{readproof_}g;        # tool and metric names
s{\bctx_(?![a-zA-Z])}{readproof_}g;     # a bare toolPrefix "ctx_"
s{\bcmd/ctx\b}{cmd/readproof}g;
s{\bdsh-plugin-ctx\b}{dsh-plugin-readproof}g;
s{\.ctx\b}{.readproof}g;                # data dir; see the warning below
s{\bCtxError\b}{ReadproofError}g;
s{\bCtxOptions\b}{ReadproofOptions}g;
s{\bCtx\b}{Readproof}g;
s{\./ctx }{./readproof }g;
s{\bctx mcp\b}{readproof mcp}g;
s{\bctx (resource|get|inspect|history|tag|run|manifest|diff|replay|evidence|version|--server)\b}{readproof $1}g;
s{\bctx\b}{readproof}g;                 # last: everything still standing
```

Then run it over the site's text files:

```bash
#!/usr/bin/env bash
# Rename Ctx -> Readproof across site/. Review `git diff` before committing.
set -euo pipefail

find site \
  -type d \( -name node_modules -o -name dist -o -name .git \) -prune -o \
  -type f \( -name '*.html' -o -name '*.md' -o -name '*.css' \
          -o -name '*.js' -o -name '*.ts' -o -name '*.json' \
          -o -name '*.yml' -o -name '*.yaml' -o -name '*.txt' \) -print0 \
| xargs -0 perl -pi -f rename-site.pl
```

**Warnings — check these before you commit the diff:**

- **Never touch the word `context`.** Every pattern above is anchored on a
  word boundary (`\b`) or a following `/`, `_`, `:` or `.`, so `context`,
  `Context`, and `context engineering` survive. If you add a pattern, keep
  that property.
- The final `s{\bctx\b}{readproof}g` is a catch-all. It is safe on `site/`
  because that tree has no Go `ctx context.Context` and no Cordis
  `ctx: Context`. **Do not** reuse it on source trees that do.
- `s{\.ctx\b}{.readproof}g` is meant for the `.ctx` data directory. If the
  site has JavaScript with a `.ctx` property access (a canvas 2D context,
  for example), exclude those files or drop that line.
- The site quotes example output: manifest tables, the trace tree, and
  terminal transcripts have hand-aligned columns that a six-character-longer
  name pushes out of true. Re-align them by hand.
- Any quoted Merkle root over `ctx://` URIs is now wrong, because the URI
  is part of the leaf. Regenerate the example rather than search-replacing
  it.
- Update the npm install line to `@readproof/sdk`, the MCP setup line to
  `claude mcp add readproof -- /abs/path/to/readproof mcp …`, and any
  `mcp__ctx__ctx_*` tool ids to `mcp__readproof__readproof_*`.

Afterwards, the same leftover scan used in this repository should come back
with only intentional hits (historical notes that name the old product):

```bash
grep -rIn --exclude-dir=node_modules --exclude-dir=.git --exclude-dir=dist \
  -E 'ctx://|\bctxd\b|@ctx/|\bCtx(Error|Options)?\b|CTX_|CTXD_|\bctx_[a-z]|"ctx\.|urn:ctx|\.ctx\b' site
```
