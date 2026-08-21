# Website brief — Ctx (landing page + documentation)

Paste this whole file into the model you want to build the site. Fill in the
**Style direction** section first; everything else is fixed product truth.

---

## 1. The job

Build a small static website for **Ctx**, an open source infrastructure tool
for AI agents: a **landing page** (`site/index.html`) and a **documentation
page** (`site/docs/index.html`), plus a branded `site/404.html`.

Hard constraints:
- Each page is **one self-contained HTML file** (inline CSS and JS, inline SVG
  icons). No build step, no framework, no CDN scripts. The only allowed
  external resource is Google Fonts.
- Relative links between pages: landing → `docs/` and `docs/#<anchor>`; docs →
  `../`. The site must work from a static host (GitHub Pages) as-is.
- Light **and** dark theme (respect `prefers-color-scheme`; no unreadable
  combinations in either). Responsive down to 360px; tables and code blocks
  scroll inside their own container, never the page.
- Accessible basics: semantic landmarks (`nav`, `main`, `section`, `footer`),
  skip link, visible focus states, `aria-label` on decorative SVG, real alt text.
- `<title>`, meta description, Open Graph tags, favicon (inline data URI is fine).
  Add `<meta name="robots" content="noindex">` — the project is pre launch.
- **Do not invent product behavior.** Every command, flag, endpoint, and output
  on the site must come from section 5 below. If you need something that is
  not listed, leave a clearly marked `TODO` instead of guessing.
- **No fabricated proof**: no testimonials, customer logos, user counts, or
  made up statistics. Real numbers you may use are in section 4.
- No "Lorem ipsum", no placeholder brands, no exclamation marks, sentence case
  headings, plain engineer to engineer voice, no marketing clichés.
- Name caveat: the product is currently called **Ctx** (CLI `ctx`, server
  `ctxd`, URI scheme `ctx://`). The name will change before public launch, so
  keep it as a plain word mark (no logo needed) and make it easy to swap.

Deliverables: the three HTML files, and a short note listing anything you
could not verify against this brief.

---

## 2. Style direction (FILL THIS IN)

> Owner: describe the look you want here, in your own words. Examples of
> things to specify: overall mood (e.g. brutalist / warm editorial / Swiss /
> playful), light-first or dark-first, typeface family or families, accent
> color(s), density (airy vs compact), how much motion, whether the hero uses
> a product "terminal" or an illustration, any sites you like as references.

Context on what has already been tried (so you do not repeat it unasked):
1. An editorial developer-docs look: Bricolage Grotesque display + Source Sans 3
   body + JetBrains Mono, cool grey ground, indigo accent, dark terminal panels,
   inline SVG mechanism diagram.
2. A strict design-system version: Geist + Geist Mono only, Tailwind type
   scale, floating "island" glass nav with hamburger morph, gradient hero
   heading text, scroll reveals with blur, word-by-word tagline reveal,
   Phosphor icons.

The owner did not like the style of these. Content and structure were fine.

---

## 3. What Ctx is (use this positioning)

**One line:** Ctx is the lockfile and replay primitive for what AI agents read.

**Thesis (quote it somewhere):** "Models are probabilistic, but many context
failures are infrastructural. Agent reliability is bounded by context
reliability."

**What it does:** every external document an agent consumes (a policy file, a
GitHub file, an HTTP resource) gets a **stable identity** (`ctx://<namespace>/<path>`),
a **freshness policy** (`require_fresh` | `allow_stale` with a max age | pin a
reviewed snapshot by `@tag`), and a **content addressed snapshot** (SHA256).
Every agent run is recorded as an immutable **manifest** — the ordered list of
what was delivered, by hash — which can be **diffed** against another run
(with provenance: which source revision changed, observed when), **replayed
byte for byte without touching the live source**, and **exported as an
evidence bundle** (an in-toto Statement whose subject digest is a Merkle root
over the run).

**What it is not:** not a vector database, not a RAG tool, not an observability
tool, not a prompt registry, not a memory system. It sits underneath those.
It complements install-time lockfiles (Microsoft APM, `skills-lock.json`,
the "agent.lock" idea) which pin *static* agent configuration; Ctx pins the
*runtime documents, per run*. Do not call it a "context layer" or "context
platform" (crowded, generic labels).

**Why now (factual):** stale or unpinned context is a recurring failure mode
in production agent postmortems; EU AI Act Article 12 automatic logging for
Annex III systems applies from 2 August 2026; SOC 2 reviews ask "what data
did the agent consider". A manifest plus an evidence bundle is that record.
Always add: **not legal advice**.

**Audience:** engineers on agent platform teams; teams in regulated products
(finance, insurance, health, public sector); people building evals and
incident workflows.

**Primary action on the landing page:** click through to the docs quickstart
("run the 60 second quickstart"). There is no signup form and no backend.
**Risk reversal:** no signup, no services, one Go binary, nothing leaves your
machine; delete the `.ctx` directory to uninstall.

**Typical objections to answer (FAQ):** is it a vector DB; does it replace my
tracing tool; do I need a server; which sources; how are credentials handled;
what does the evidence prove and not prove; does it work with Claude Code and
Cursor; what is the license / why will the name change.

---

## 4. Honest proof and real numbers

- Version **0.2.0**, private checkpoint, no LICENSE yet (chosen at launch).
- **3 source adapters** (filesystem, GitHub, HTTP); **2 storage backends**
  (embedded SQLite + local blobs, or Postgres + S3 compatible store);
  **13 MCP tools**; TypeScript SDK with zero runtime dependencies;
  OpenTelemetry spans with GenAI attributes; a LangGraph.js example.
- The core invariant is a test: the reference demo asserts
  `SHA256(original) == SHA256(replay)` over SQLite, over Postgres + MinIO,
  and over a real HTTP round trip.
- Evidence follows the in-toto Statement v1 shape; Merkle vectors were checked
  against an independent implementation; tamper tests must fail.
- CI on every push: Go build/vet/test, SDK tests, and a Docker Compose
  integration run that replays the demo against the built `ctxd` image.
- The MCP server uses the official Go MCP SDK and is tested through a real
  MCP client over both the embedded and the remote (HTTP) paths.

---

## 5. Product facts and verbatim output (copy from here, do not invent)

### 5.1 Concepts
- **Source** — where bytes live: `filesystem`, `github`, `http`. Credentials
  are read from the `ctx`/`ctxd` process environment at fetch time, never stored.
- **Resource** — stable logical identity `ctx://<namespace>/<path>`.
- **Policy** — `require_fresh` (re-verify on every resolve; unchanged bytes
  dedupe to the same content hash) | `allow_stale --max-age <duration>`
  (reuse while younger than the TTL; `0` = never refresh once a snapshot
  exists). Pinning is done with **tags**: `ctx://ns/path@prod` resolves to the
  tagged snapshot with no fetch and the policy not consulted.
- **Snapshot** — immutable observation: `sha256:…` content hash, source
  revision (commit SHA for GitHub, content hash prefix for files, ETag/etc for
  HTTP), observed-at, provenance map, content type, bytes.
- **Materialization** — the bytes delivered (raw only in v0.2).
- **Manifest** — ordered, immutable record of everything a run resolved:
  position, URI, `ref` (the `@tag` used, if any), snapshot id, content hash.
  Order is an invariant.
- **Evidence bundle** — in-toto Statement v1: `subject[0].digest.sha256` is a
  Merkle root over entries (leaf = `sha256(position_be_uint32 || 0x00 || uri || 0x00 || content_hash)`),
  `predicateType` currently `urn:ctx:evidence:v0.2`, predicate carries entries
  (with optional `content_b64`), redacted resource definitions, and a replay
  check. `ctx evidence verify` recomputes the root, re-hashes embedded content,
  and (unless `--offline`) cross-checks against the store via replay.

### 5.2 CLI (exact surface)
```
ctx resource add <uri> --source-type filesystem|github|http [--path p] [--owner o --repo r --ref main] [--url u] [--header 'K: V']... --policy require_fresh|allow_stale [--max-age 1h]
ctx resource list · ctx inspect <uri>[@tag] · ctx history <uri> · ctx get <uri>[@tag]
ctx run --id <run> <uri>[@tag]...        # one shot: start + mount + commit
ctx run start <run> · ctx run mount <run> <uri>[@tag] · ctx run commit <run>
ctx manifest <manifest-id|run-id> · ctx diff <a> <b> · ctx replay <manifest-id|run-id>
ctx tag set <uri> <tag> <snapshot-id> · ctx tag list <uri> · ctx tag rm <uri> <tag>
ctx evidence export <target> [--with-content] [--out file] · ctx evidence verify <bundle.json> [--offline]
ctx mcp                                   # stdio MCP server
ctx version
Global flags: --data-dir <dir> (embedded; default .ctx or $CTX_HOME) · --server <url> / $CTX_SERVER_URL · --api-key / $CTX_API_KEY
```
HTTP header values may reference environment variables: `--header 'Authorization: Bearer ${PRICING_TOKEN}'`
(resolved at fetch time; sensitive headers are masked in API responses, `ctx inspect`, and evidence bundles).
`GITHUB_TOKEN` is read from the environment for GitHub sources.

### 5.3 Verbatim outputs (ctx 0.2.0, 2026-08-21; trim IDs/hashes for width if needed)
```
$ ctx resource add ctx://demo/policies/refunds --source-type filesystem --path policies/refunds.md --policy require_fresh
Registered resource ctx://demo/policies/refunds
  source: filesystem
  policy: require_fresh

$ ctx get ctx://demo/policies/refunds
uri:          ctx://demo/policies/refunds
snapshot:     snap_01M0HRB5GNVBJ1MYZXCPHA51VZ
content_hash: sha256:c8b0bb212e93151d720746e36ff3b7076727cb577614feafa0d61f168965aedb
freshness:    fresh (observed 2026-08-21T08:52:32Z, policy require_fresh)
provenance:   path=policies/refunds.md source_type=filesystem
bytes:        41
content_type: text/markdown

--- content ---
Products can be refunded within 30 days.

$ ctx run --id run-a ctx://demo/policies/refunds
Started run run-a
Mounted ctx://demo/policies/refunds -> snapshot snap_01M0HRB5KJ2HNJBYH2TV22BBT0 (position 0)
Committed manifest manifest_01M0HRB5KJMHPS0X0GGXF853CP for run run-a (1 entry)

$ ctx manifest run-a
Manifest manifest_01M0HRB5KJMHPS0X0GGXF853CP (run run-a), created 2026-08-21T08:52:32Z, 1 entry

POS  URI                          SNAPSHOT                         CONTENT_HASH
0    ctx://demo/policies/refunds  snap_01M0HRB5KJ2HNJBYH2TV22BBT0  sha256:c8b0bb212e93151d720746e36ff3b7076727cb577614feafa0d61f168965aedb

$ ctx tag set ctx://demo/policies/refunds prod snap_01M0HRB5KJ2HNJBYH2TV22BBT0
Tagged ctx://demo/policies/refunds@prod -> snap_01M0HRB5KJ2HNJBYH2TV22BBT0
  resolve it with: ctx get ctx://demo/policies/refunds@prod

$ printf 'Products can be refunded within 14 days.\n' > policies/refunds.md
$ ctx run --id run-b ctx://demo/policies/refunds
Started run run-b
Mounted ctx://demo/policies/refunds -> snapshot snap_01M0HRB68RV3WBGM6JE7QR19TA (position 0)
Committed manifest manifest_01M0HRB68SZDF1N6TTNXC0VQCT for run run-b (1 entry)

$ ctx diff run-a run-b
--- run-a (manifest_01M0HRB5KJMHPS0X0GGXF853CP)
+++ run-b (manifest_01M0HRB68SZDF1N6TTNXC0VQCT)

~ ctx://demo/policies/refunds  (snap_01M0HRB5KJ2HNJBYH2TV22BBT0 -> snap_01M0HRB68RV3WBGM6JE7QR19TA)
  why: source revision sha256:c8b0bb212e93 → sha256:8f4b00474456; observed 2026-08-21T08:52:32Z → 2026-08-21T08:52:33Z
  --- a/ctx://demo/policies/refunds
  +++ b/ctx://demo/policies/refunds
  @@ -1,2 +1,2 @@
  -Products can be refunded within 30 days.
  +Products can be refunded within 14 days.

1 resource changed, 0 added, 0 removed, 0 unchanged

$ ctx get ctx://demo/policies/refunds@prod
uri:          ctx://demo/policies/refunds@prod
ref:          prod
snapshot:     snap_01M0HRB5KJ2HNJBYH2TV22BBT0
content_hash: sha256:c8b0bb212e93151d720746e36ff3b7076727cb577614feafa0d61f168965aedb
freshness:    tagged (@prod -> snapshot snap_01M0HRB5KJ2HNJBYH2TV22BBT0, observed 2026-08-21T08:52:32Z, policy not consulted)
provenance:   path=policies/refunds.md source_type=filesystem
bytes:        41
content_type: text/markdown

--- content ---
Products can be refunded within 30 days.

$ ctx history ctx://demo/policies/refunds
SNAPSHOT                         OBSERVED              REVISION             TAGS
snap_01M0HRB68RV3WBGM6JE7QR19TA  2026-08-21T08:52:33Z  sha256:8f4b00474456  -
snap_01M0HRB5KJ2HNJBYH2TV22BBT0  2026-08-21T08:52:32Z  sha256:c8b0bb212e93  prod
snap_01M0HRB5GNVBJ1MYZXCPHA51VZ  2026-08-21T08:52:32Z  sha256:c8b0bb212e93  -

$ ctx replay run-a
Replaying manifest manifest_01M0HRB5KJMHPS0X0GGXF853CP (run run-a), 1 entry

[0] ctx://demo/policies/refunds
    materialization: mat_01M0HRB5KJHXZ6DHBAX3XN74NC
    content_hash (recorded):  sha256:c8b0bb212e93151d720746e36ff3b7076727cb577614feafa0d61f168965aedb
    content_hash (replayed):  sha256:c8b0bb212e93151d720746e36ff3b7076727cb577614feafa0d61f168965aedb
    match: OK

--- content ---
Products can be refunded within 30 days.

Replay verified: SHA256 match for 1/1 entries.

$ ctx run --id run-c ctx://demo/policies/refunds@prod
Mounted ctx://demo/policies/refunds@prod -> snapshot snap_01M0HRB5KJ2HNJBYH2TV22BBT0 (position 0)
Committed manifest manifest_01M0HRB6SEQQ37906N5HJ4EP1Z for run run-c (1 entry)

$ ctx manifest run-c
POS  URI                          REF   SNAPSHOT                         CONTENT_HASH
0    ctx://demo/policies/refunds  prod  snap_01M0HRB5KJ2HNJBYH2TV22BBT0  sha256:c8b0bb212e93…

$ ctx evidence export run-a --with-content --out bundle.json
evidence bundle written to bundle.json: 1 entry, merkle root 8482a7671b1d7ba1081f4a7b3a3e7400a3ab58dc3a3687430bae6fa316adbd68
$ ctx evidence verify bundle.json
evidence verified: 1 entry, merkle root 8482a7671b1d7ba1081f4a7b3a3e7400a3ab58dc3a3687430bae6fa316adbd68, embedded content 1/1 re-hashed, replay match 1/1

$ ctx inspect ctx://demo/policies/refunds
Resource:  ctx://demo/policies/refunds
Namespace: demo
Path:      policies/refunds
Source:   type: filesystem  path: policies/refunds.md
Policy:   strategy: require_fresh  max_age: n/a
Tags:     prod -> snap_01M0HRB5KJ2HNJBYH2TV22BBT0 (updated 2026-08-21T08:52:33Z)
Current snapshot: snap_01M0HRB68RV3WBGM6JE7QR19TA  observed 2026-08-21T08:52:33Z  source_revision sha256:8f4b00474456

$ ctx version
ctx 0.2.0
```
Strict replay: any hash mismatch or missing blob exits non zero.
Unknown run on `run commit`/`run mount` → error (HTTP 404); committing twice → error (HTTP 409).

### 5.4 Install and modes
```
git clone <repo> ctx && cd ctx
go build -o ctx ./cmd/ctx           # Go 1.26+
# embedded mode: everything in ./.ctx (SQLite + blobs), no services

# client/server mode
docker compose up -d --build        # Postgres + MinIO + ctxd + OTel collector (dev credentials are placeholders; override via .env)
curl http://localhost:8080/healthz  # ok
export CTX_SERVER_URL=http://localhost:8080
# containerized ctxd cannot see the host filesystem: use GitHub/HTTP sources there, or run ctxd on the host:
go build -o ctxd ./cmd/ctxd && ./ctxd --addr :8080 --data-dir ~/.ctx
# ctxd flags (also CTXD_* env): --addr, --data-dir | --postgres-dsn --s3-endpoint --s3-access-key --s3-secret-key --s3-bucket --s3-use-ssl, --api-key, --version
```

### 5.5 HTTP API (ctxd)
```
POST /v1/resources · GET /v1/resources · GET /v1/resources/get?uri= · GET /v1/resources/history?uri=
GET  /v1/snapshots?id=
PUT  /v1/tags {uri, tag, snapshot_id} · GET /v1/tags?uri= · DELETE /v1/tags?uri=&tag=
POST /v1/resolve {uri}            (uri may carry @tag)
POST /v1/runs · POST /v1/runs/mount · POST /v1/runs/commit
GET  /v1/manifests?target= · GET /v1/diff?a=&b= · GET /v1/replay?target=
GET  /healthz
Auth: optional `Authorization: Bearer <key>` when ctxd runs with --api-key.
```

### 5.6 TypeScript SDK (`@ctx/sdk`, Node 18+, zero deps; not on a registry yet: build from `sdk/typescript`)
```ts
import { Ctx, buildEvidence, encodeEvidence } from "@ctx/sdk";
const ctx = new Ctx({ endpoint: "http://localhost:8080", apiKey: process.env.CTX_API_KEY });
const policy = await ctx.resolve("ctx://acme/policies/refunds");     // policy.content, policy.snapshot.id, policy.snapshot.content_hash
const run = ctx.run({ id: "run_9182" });                               // starts lazily on first mount
await run.mount("ctx://acme/policies/refunds@prod");                   // pinned by tag: freshness.status === "use_tag", resource.ref === "prod"
await run.mount(`ctx://acme/customers/${customerId}`);
const manifest = await run.commit();                                   // manifest.manifest_id
await ctx.setTag("ctx://acme/policies/refunds", "prod", policy.snapshot.id);
await ctx.listTags("ctx://acme/policies/refunds"); await ctx.deleteTag("ctx://acme/policies/refunds", "prod");
const diff = await ctx.diff("run-a", "run-b");                         // entries[].status, unified_diff, source_revision_a/_b, observed_at_a/_b, ref_a/_b
const replay = await ctx.replay("run-a");                              // entries.every(e => e.match)
const bundle = await buildEvidence(ctx, "run-a", { withContent: true }); // same Merkle root as `ctx evidence export`
await fs.writeFile("bundle.json", encodeEvidence(bundle));
```
Errors are `CtxError` with HTTP status and message.

### 5.7 MCP (`ctx mcp`, stdio)
```
claude mcp add ctx -- /abs/path/to/ctx mcp --data-dir /abs/path/to/.ctx
claude mcp add ctx --env CTX_API_KEY=sk-... -- /abs/path/to/ctx mcp --server https://ctxd.internal
```
Claude Desktop / Cursor: `{"mcpServers":{"ctx":{"command":"/abs/path/to/ctx","args":["mcp","--data-dir","/abs/path/to/.ctx"]}}}`
Resources: registered docs as `ctx://` resources via template `ctx://{namespace}/{+path}`; `@tag` honored; each read carries `_meta`
`{uri, ref, snapshot_id, content_hash, source_revision, observed_at, decision, materialization_id, content_type, bytes}`.
Tools (13): ctx_resources_list, ctx_resolve, ctx_history, ctx_run_start, ctx_run_mount, ctx_run_commit, ctx_manifest, ctx_diff,
ctx_replay, ctx_tag_set, ctx_tag_list, ctx_tag_delete, ctx_evidence_export. Inline content capped at 1 MiB with a truncation marker.
Stdio = local trust; `--server` mode inherits ctxd's API key auth. Resolve/mount have side effects (may create snapshots).

### 5.8 LangGraph example (`examples/langgraph-ts`)
A `load_context` node does `ctx.run({id: "langgraph-<thread_id>"}).mount(uri)` for each URI, `commit()`s, and stores
`ctx_manifest_id` (plus the mounted bytes) in graph state, which lands in the checkpoint (`graph.getState(config)`).
`answer_question` prompts the model with exactly those bytes. `npm run replay` replays the checkpointed manifest and
shows the original bytes even after the source file changed (it flags the live source as CHANGED).
Run: build the SDK, start `ctxd` on the host in embedded mode, `npm ci && npm run build && npm run start && npm run replay`.
Default model is a fake in-memory model; a real one is used if `ANTHROPIC_API_KEY` is set.

### 5.9 Observability
Set `OTEL_EXPORTER_OTLP_ENDPOINT` to export; unset = no op. Spans: `ctx.run.start`, `ctx.run.mount` (parents `ctx.resolve`
and `ctx.manifest.append`), `ctx.run.commit` (`ctx.manifest.id`, `ctx.manifest.entries`, `ctx.manifest.merkle_root` — equal to
the evidence bundle's subject digest), `ctx.resolve` (`ctx.resource.uri`, `ctx.resource.ref`, `ctx.snapshot.id`,
`ctx.snapshot.content_hash`, `ctx.snapshot.source_revision`, `ctx.snapshot.observed_at`, `ctx.policy.strategy`,
`ctx.policy.decision`, `ctx.source.type`, `ctx.materialization.bytes`, `gen_ai.data_source.id` = `ctx://<namespace>`),
`ctx.resource.lookup`, `ctx.policy.evaluate`, `ctx.tag.lookup`, `ctx.cache.lookup`, `ctx.source.fetch`, `ctx.snapshot.create`,
`ctx.materialize`, `ctx.manifest.append`. Content is never attached to spans or metrics. Metrics: ctx_resolve_total/_duration_seconds/
_errors_total, ctx_cache_hit_total/_miss_total, ctx_source_fetch_*, ctx_snapshot_created_total, ctx_manifest_created_total,
ctx_run_committed_total, ctx_tag_resolve_total.

### 5.10 Roadmap (in order) and status
Rename + LICENSE + public repo · Python SDK · trace context propagation over the HTTP API · MCP HTTP transport · a policy file
(allowed sources / SSRF allow list / content scanning) · signed and OCI distributed evidence bundles · `tag promote` ·
more adapters (S3, Confluence/Notion, generic git) · Temporal/Restate helpers · auth beyond one API key · operator UI.
Security baseline today: no plaintext credentials at rest, redaction everywhere, optional single API key, dev only Compose
credentials labeled, dependency scanning clean. Not enterprise IAM; bundles not signed yet.

---

## 6. Pages and structure

### Landing (`site/index.html`)
Sections in order (content per sections 3–5; you choose the visual treatment):
1. Nav: word mark, links to sections, "Docs" → `docs/`.
2. Hero: headline (outcome + audience), subheadline, **one** primary CTA → `docs/#install`, risk line, one honest proof
   signal, hero visual (a terminal rendition of the real output above works well; an illustration is fine).
3. Problem → solution (stale/unpinned, not reproducible, "what did the agent consider").
4. Benefits (3–5, outcome first, each backed by a real command).
5. How it works (register → record → prove, real commands and output).
6. Proof (section 4; no fakes).
7. Integrations (MCP, OpenTelemetry, LangGraph, TypeScript SDK, HTTP API, storage) with one or two short code panels.
8. FAQ (6–12; the objections in section 3) — plain question/answer so it reads well for search/AEO.
9. Final CTA identical to the hero CTA + risk reversal.
10. Footer: docs links, privacy/terms (a short "this page sets no cookies and collects nothing" section is honest and enough).

### Docs (`site/docs/index.html`)
Single page with a sticky table of contents. Keep these anchor ids (the landing links to them):
`#overview #how #install #sources #runs #tags #evidence #server #sdk #mcp #langgraph #otel #cli #faq`.
Content to cover under each: overview/thesis; mechanism + the six concepts (+ tags, evidence); install & first resolve
(verbatim output); sources (filesystem/GitHub/HTTP with the header env var pattern) and policies table; runs/manifest/diff/replay
walkthrough with verbatim output; tags workflow; evidence export/verify + bundle shape + what it proves/doesn't + Art. 12/SOC 2
note (not legal advice); server mode, ctxd flags, endpoint table, a curl example; SDK examples; MCP setup for Claude Code /
Claude Desktop / Cursor + tool table + "try it" prompts; LangGraph pattern; observability span tree + attributes; CLI cheat sheet;
FAQ & status/roadmap. A link back to the landing (`../`).

### 404 (`site/404.html`)
Branded, one line of product voice, links to `/` and `/docs/`.

---

## 7. Existing material you may reuse
The repo already contains a complete `site/` (landing, docs, 404) whose **content and structure are approved** — only the
visual style is being replaced. You may lift copy, tables, code blocks, and the SVG mechanism diagram from those files, or
rewrite from this brief. Phosphor icon SVGs (MIT) are inlined there if you want them.

## 8. Acceptance checklist
- [ ] Every command/flag/endpoint/output appears in section 5 (no invented behavior)
- [ ] One primary CTA above the fold; final CTA identical
- [ ] No fabricated proof; "not legal advice" present wherever compliance is mentioned
- [ ] Light and dark both readable; mobile 360px; code/tables scroll inside their container
- [ ] Self-contained files; only Google Fonts external; relative links work from a static host
- [ ] `noindex`, title/meta/OG, favicon, skip link, semantic landmarks, focus states, no dead links
- [ ] Docs anchors listed in section 6 exist
