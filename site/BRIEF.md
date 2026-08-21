# Website brief — Readproof (landing page + documentation)

Paste this whole file into the model you want to build the site. Fill in the
**Style direction** section first; everything else is fixed product truth.

---

## 1. The job

Build a small static website for **Readproof**, an open source infrastructure tool
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
- Name: the product is **Readproof** (CLI `readproof`, server `readproofd`, URI
  scheme `readproof://`). Keep it as a plain word mark (no logo yet).

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

## 3. What Readproof is (use this positioning)

**One line:** Readproof is the lockfile and replay primitive for what AI agents read.

**Thesis (quote it somewhere):** "Models are probabilistic, but many context
failures are infrastructural. Agent reliability is bounded by context
reliability."

**What it does:** every external document an agent consumes (a policy file, a
GitHub file, an HTTP resource) gets a **stable identity** (`readproof://<namespace>/<path>`),
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
the "agent.lock" idea) which pin *static* agent configuration; Readproof pins the
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
machine; delete the `.readproof` directory to uninstall.

**Typical objections to answer (FAQ):** is it a vector DB; does it replace my
tracing tool; do I need a server; which sources; how are credentials handled;
what does the evidence prove and not prove; does it work with Claude Code and
Cursor; what is the license (Apache-2.0).

---

## 4. Honest proof and real numbers

- Version **0.3.0**, licensed Apache-2.0, pre public launch.
- **3 source adapters** (filesystem, GitHub, HTTP); **2 storage backends**
  (embedded SQLite + local blobs, or Postgres + S3 compatible store);
  **13 MCP tools**; TypeScript SDK with zero runtime dependencies;
  OpenTelemetry spans with GenAI attributes; a LangGraph.js example; a DeepSeek Harness plugin (26 tests);
  a support agent example on Ollama (7 e2e tests).
- The core invariant is a test: the reference demo asserts
  `SHA256(original) == SHA256(replay)` over SQLite, over Postgres + MinIO,
  and over a real HTTP round trip.
- Evidence follows the in-toto Statement v1 shape; Merkle vectors were checked
  against an independent implementation; tamper tests must fail.
- CI on every push: Go build/vet/test, SDK tests, and a Docker Compose
  integration run that replays the demo against the built `readproofd` image.
- The MCP server uses the official Go MCP SDK and is tested through a real
  MCP client over both the embedded and the remote (HTTP) paths.

---

## 5. Product facts and verbatim output (copy from here, do not invent)

### 5.1 Concepts
- **Source** — where bytes live: `filesystem`, `github`, `http`. Credentials
  are read from the `readproof`/`readproofd` process environment at fetch time, never stored.
- **Resource** — stable logical identity `readproof://<namespace>/<path>`.
- **Policy** — `require_fresh` (re-verify on every resolve; unchanged bytes
  dedupe to the same content hash) | `allow_stale --max-age <duration>`
  (reuse while younger than the TTL; `0` = never refresh once a snapshot
  exists). Pinning is done with **tags**: `readproof://ns/path@prod` resolves to the
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
  `predicateType` currently `urn:readproof:evidence:v0.3`, predicate carries entries
  (with optional `content_b64`), redacted resource definitions, and a replay
  check. `readproof evidence verify` recomputes the root, re-hashes embedded content,
  and (unless `--offline`) cross-checks against the store via replay.

### 5.2 CLI (exact surface)
```
readproof resource add <uri> --source-type filesystem|github|http [--path p] [--owner o --repo r --ref main] [--url u] [--header 'K: V']... --policy require_fresh|allow_stale [--max-age 1h]
readproof resource list · readproof inspect <uri>[@tag] · readproof history <uri> · readproof get <uri>[@tag]
readproof run --id <run> <uri>[@tag]...        # one shot: start + mount + commit
readproof run start <run> · readproof run mount <run> <uri>[@tag] · readproof run commit <run>
readproof manifest <manifest-id|run-id> · readproof diff <a> <b> · readproof replay <manifest-id|run-id>
readproof tag set <uri> <tag> <snapshot-id> · readproof tag list <uri> · readproof tag rm <uri> <tag>
readproof evidence export <target> [--with-content] [--out file] · readproof evidence verify <bundle.json> [--offline]
readproof mcp                                   # stdio MCP server
readproof version
Global flags: --data-dir <dir> (embedded; default .readproof or $READPROOF_HOME) · --server <url> / $READPROOF_SERVER_URL · --api-key / $READPROOF_API_KEY
```
HTTP header values may reference environment variables: `--header 'Authorization: Bearer ${PRICING_TOKEN}'`
(resolved at fetch time; sensitive headers are masked in API responses, `readproof inspect`, and evidence bundles).
`GITHUB_TOKEN` is read from the environment for GitHub sources.

### 5.3 Verbatim outputs (readproof 0.2.0, 2026-08-21; trim IDs/hashes for width if needed)
```
$ readproof resource add readproof://demo/policies/refunds --source-type filesystem --path policies/refunds.md --policy require_fresh
Registered resource readproof://demo/policies/refunds
  source: filesystem
  policy: require_fresh

$ readproof get readproof://demo/policies/refunds
uri:          readproof://demo/policies/refunds
snapshot:     snap_01M0HRB5GNVBJ1MYZXCPHA51VZ
content_hash: sha256:c8b0bb212e93151d720746e36ff3b7076727cb577614feafa0d61f168965aedb
freshness:    fresh (observed 2026-08-21T08:52:32Z, policy require_fresh)
provenance:   path=policies/refunds.md source_type=filesystem
bytes:        41
content_type: text/markdown

--- content ---
Products can be refunded within 30 days.

$ readproof run --id run-a readproof://demo/policies/refunds
Started run run-a
Mounted readproof://demo/policies/refunds -> snapshot snap_01M0HRB5KJ2HNJBYH2TV22BBT0 (position 0)
Committed manifest manifest_01M0HRB5KJMHPS0X0GGXF853CP for run run-a (1 entry)

$ readproof manifest run-a
Manifest manifest_01M0HRB5KJMHPS0X0GGXF853CP (run run-a), created 2026-08-21T08:52:32Z, 1 entry

POS  URI                          SNAPSHOT                         CONTENT_HASH
0    readproof://demo/policies/refunds  snap_01M0HRB5KJ2HNJBYH2TV22BBT0  sha256:c8b0bb212e93151d720746e36ff3b7076727cb577614feafa0d61f168965aedb

$ readproof tag set readproof://demo/policies/refunds prod snap_01M0HRB5KJ2HNJBYH2TV22BBT0
Tagged readproof://demo/policies/refunds@prod -> snap_01M0HRB5KJ2HNJBYH2TV22BBT0
  resolve it with: readproof get readproof://demo/policies/refunds@prod

$ printf 'Products can be refunded within 14 days.\n' > policies/refunds.md
$ readproof run --id run-b readproof://demo/policies/refunds
Started run run-b
Mounted readproof://demo/policies/refunds -> snapshot snap_01M0HRB68RV3WBGM6JE7QR19TA (position 0)
Committed manifest manifest_01M0HRB68SZDF1N6TTNXC0VQCT for run run-b (1 entry)

$ readproof diff run-a run-b
--- run-a (manifest_01M0HRB5KJMHPS0X0GGXF853CP)
+++ run-b (manifest_01M0HRB68SZDF1N6TTNXC0VQCT)

~ readproof://demo/policies/refunds  (snap_01M0HRB5KJ2HNJBYH2TV22BBT0 -> snap_01M0HRB68RV3WBGM6JE7QR19TA)
  why: source revision sha256:c8b0bb212e93 → sha256:8f4b00474456; observed 2026-08-21T08:52:32Z → 2026-08-21T08:52:33Z
  --- a/readproof://demo/policies/refunds
  +++ b/readproof://demo/policies/refunds
  @@ -1,2 +1,2 @@
  -Products can be refunded within 30 days.
  +Products can be refunded within 14 days.

1 resource changed, 0 added, 0 removed, 0 unchanged

$ readproof get readproof://demo/policies/refunds@prod
uri:          readproof://demo/policies/refunds@prod
ref:          prod
snapshot:     snap_01M0HRB5KJ2HNJBYH2TV22BBT0
content_hash: sha256:c8b0bb212e93151d720746e36ff3b7076727cb577614feafa0d61f168965aedb
freshness:    tagged (@prod -> snapshot snap_01M0HRB5KJ2HNJBYH2TV22BBT0, observed 2026-08-21T08:52:32Z, policy not consulted)
provenance:   path=policies/refunds.md source_type=filesystem
bytes:        41
content_type: text/markdown

--- content ---
Products can be refunded within 30 days.

$ readproof history readproof://demo/policies/refunds
SNAPSHOT                         OBSERVED              REVISION             TAGS
snap_01M0HRB68RV3WBGM6JE7QR19TA  2026-08-21T08:52:33Z  sha256:8f4b00474456  -
snap_01M0HRB5KJ2HNJBYH2TV22BBT0  2026-08-21T08:52:32Z  sha256:c8b0bb212e93  prod
snap_01M0HRB5GNVBJ1MYZXCPHA51VZ  2026-08-21T08:52:32Z  sha256:c8b0bb212e93  -

$ readproof replay run-a
Replaying manifest manifest_01M0HRB5KJMHPS0X0GGXF853CP (run run-a), 1 entry

[0] readproof://demo/policies/refunds
    materialization: mat_01M0HRB5KJHXZ6DHBAX3XN74NC
    content_hash (recorded):  sha256:c8b0bb212e93151d720746e36ff3b7076727cb577614feafa0d61f168965aedb
    content_hash (replayed):  sha256:c8b0bb212e93151d720746e36ff3b7076727cb577614feafa0d61f168965aedb
    match: OK

--- content ---
Products can be refunded within 30 days.

Replay verified: SHA256 match for 1/1 entries.

$ readproof run --id run-c readproof://demo/policies/refunds@prod
Mounted readproof://demo/policies/refunds@prod -> snapshot snap_01M0HRB5KJ2HNJBYH2TV22BBT0 (position 0)
Committed manifest manifest_01M0HRB6SEQQ37906N5HJ4EP1Z for run run-c (1 entry)

$ readproof manifest run-c
POS  URI                          REF   SNAPSHOT                         CONTENT_HASH
0    readproof://demo/policies/refunds  prod  snap_01M0HRB5KJ2HNJBYH2TV22BBT0  sha256:c8b0bb212e93…

$ readproof evidence export run-a --with-content --out bundle.json
evidence bundle written to bundle.json: 1 entry, merkle root 8482a7671b1d7ba1081f4a7b3a3e7400a3ab58dc3a3687430bae6fa316adbd68
$ readproof evidence verify bundle.json
evidence verified: 1 entry, merkle root 8482a7671b1d7ba1081f4a7b3a3e7400a3ab58dc3a3687430bae6fa316adbd68, embedded content 1/1 re-hashed, replay match 1/1

$ readproof inspect readproof://demo/policies/refunds
Resource:  readproof://demo/policies/refunds
Namespace: demo
Path:      policies/refunds
Source:   type: filesystem  path: policies/refunds.md
Policy:   strategy: require_fresh  max_age: n/a
Tags:     prod -> snap_01M0HRB5KJ2HNJBYH2TV22BBT0 (updated 2026-08-21T08:52:33Z)
Current snapshot: snap_01M0HRB68RV3WBGM6JE7QR19TA  observed 2026-08-21T08:52:33Z  source_revision sha256:8f4b00474456

$ readproof version
readproof 0.2.0
```
Strict replay: any hash mismatch or missing blob exits non zero.
Unknown run on `run commit`/`run mount` → error (HTTP 404); committing twice → error (HTTP 409).

### 5.4 Install and modes
```
git clone <repo> readproof && cd readproof
go build -o readproof ./cmd/readproof           # Go 1.26+
# embedded mode: everything in ./.readproof (SQLite + blobs), no services

# client/server mode
docker compose up -d --build        # Postgres + MinIO + readproofd + OTel collector (dev credentials are placeholders; override via .env)
curl http://localhost:8080/healthz  # ok
export READPROOF_SERVER_URL=http://localhost:8080
# containerized readproofd cannot see the host filesystem: use GitHub/HTTP sources there, or run readproofd on the host:
go build -o readproofd ./cmd/readproofd && ./readproofd --addr :8080 --data-dir ~/.readproof
# readproofd flags (also READPROOFD_* env): --addr, --data-dir | --postgres-dsn --s3-endpoint --s3-access-key --s3-secret-key --s3-bucket --s3-use-ssl, --api-key, --version
```

### 5.5 HTTP API (readproofd)
```
POST /v1/resources · GET /v1/resources · GET /v1/resources/get?uri= · GET /v1/resources/history?uri=
GET  /v1/snapshots?id=
PUT  /v1/tags {uri, tag, snapshot_id} · GET /v1/tags?uri= · DELETE /v1/tags?uri=&tag=
POST /v1/resolve {uri}            (uri may carry @tag)
POST /v1/runs · POST /v1/runs/mount · POST /v1/runs/commit
GET  /v1/manifests?target= · GET /v1/diff?a=&b= · GET /v1/replay?target=
GET  /healthz
Auth: optional `Authorization: Bearer <key>` when readproofd runs with --api-key.
```

### 5.6 TypeScript SDK (`@readproof/sdk`, Node 18+, zero deps; not on a registry yet: build from `sdk/typescript`)
```ts
import { Readproof, buildEvidence, encodeEvidence } from "@readproof/sdk";
const readproof = new Readproof({ endpoint: "http://localhost:8080", apiKey: process.env.READPROOF_API_KEY });
const policy = await readproof.resolve("readproof://acme/policies/refunds");     // policy.content, policy.snapshot.id, policy.snapshot.content_hash
const run = readproof.run({ id: "run_9182" });                               // starts lazily on first mount
await run.mount("readproof://acme/policies/refunds@prod");                   // pinned by tag: freshness.status === "use_tag", resource.ref === "prod"
await run.mount(`readproof://acme/customers/${customerId}`);
const manifest = await run.commit();                                   // manifest.manifest_id
await readproof.setTag("readproof://acme/policies/refunds", "prod", policy.snapshot.id);
await readproof.listTags("readproof://acme/policies/refunds"); await readproof.deleteTag("readproof://acme/policies/refunds", "prod");
const diff = await readproof.diff("run-a", "run-b");                         // entries[].status, unified_diff, source_revision_a/_b, observed_at_a/_b, ref_a/_b
const replay = await readproof.replay("run-a");                              // entries.every(e => e.match)
const bundle = await buildEvidence(readproof, "run-a", { withContent: true }); // same Merkle root as `readproof evidence export`
await fs.writeFile("bundle.json", encodeEvidence(bundle));
```
Errors are `ReadproofError` with HTTP status and message.

### 5.7 MCP (`readproof mcp`, stdio)
```
claude mcp add readproof -- /abs/path/to/readproof mcp --data-dir /abs/path/to/.readproof
claude mcp add readproof --env READPROOF_API_KEY=sk-... -- /abs/path/to/readproof mcp --server https://readproofd.internal
```
Claude Desktop / Cursor: `{"mcpServers":{"readproof":{"command":"/abs/path/to/readproof","args":["mcp","--data-dir","/abs/path/to/.readproof"]}}}`
Resources: registered docs as `readproof://` resources via template `readproof://{namespace}/{+path}`; `@tag` honored; each read carries `_meta`
`{uri, ref, snapshot_id, content_hash, source_revision, observed_at, decision, materialization_id, content_type, bytes}`.
Tools (13): readproof_resources_list, readproof_resolve, readproof_history, readproof_run_start, readproof_run_mount, readproof_run_commit, readproof_manifest, readproof_diff,
readproof_replay, readproof_tag_set, readproof_tag_list, readproof_tag_delete, readproof_evidence_export. Inline content capped at 1 MiB with a truncation marker.
Stdio = local trust; `--server` mode inherits readproofd's API key auth. Resolve/mount have side effects (may create snapshots).

### 5.8 LangGraph example (`examples/langgraph-ts`)
A `load_context` node does `readproof.run({id: "langgraph-<thread_id>"}).mount(uri)` for each URI, `commit()`s, and stores
`readproof_manifest_id` (plus the mounted bytes) in graph state, which lands in the checkpoint (`graph.getState(config)`).
`answer_question` prompts the model with exactly those bytes. `npm run replay` replays the checkpointed manifest and
shows the original bytes even after the source file changed (it flags the live source as CHANGED).
Run: build the SDK, start `readproofd` on the host in embedded mode, `npm ci && npm run build && npm run start && npm run replay`.
Default model is a fake in-memory model; a real one is used if `ANTHROPIC_API_KEY` is set.

### 5.9 Observability
Set `OTEL_EXPORTER_OTLP_ENDPOINT` to export; unset = no op. Spans: `readproof.run.start`, `readproof.run.mount` (parents `readproof.resolve`
and `readproof.manifest.append`), `readproof.run.commit` (`readproof.manifest.id`, `readproof.manifest.entries`, `readproof.manifest.merkle_root` — equal to
the evidence bundle's subject digest), `readproof.resolve` (`readproof.resource.uri`, `readproof.resource.ref`, `readproof.snapshot.id`,
`readproof.snapshot.content_hash`, `readproof.snapshot.source_revision`, `readproof.snapshot.observed_at`, `readproof.policy.strategy`,
`readproof.policy.decision`, `readproof.source.type`, `readproof.materialization.bytes`, `gen_ai.data_source.id` = `readproof://<namespace>`),
`readproof.resource.lookup`, `readproof.policy.evaluate`, `readproof.tag.lookup`, `readproof.cache.lookup`, `readproof.source.fetch`, `readproof.snapshot.create`,
`readproof.materialize`, `readproof.manifest.append`. Content is never attached to spans or metrics. Metrics: readproof_resolve_total/_duration_seconds/
_errors_total, readproof_cache_hit_total/_miss_total, readproof_source_fetch_*, readproof_snapshot_created_total, readproof_manifest_created_total,
readproof_run_committed_total, readproof_tag_resolve_total.

### 5.10a DeepSeek Harness plugin (`integrations/deepseek-harness/`)
Native DSH bundle `dsh-plugin-readproof` (Cordis plugin: `name = 'readproof'`, `inject = ['tools']`, Schemastery `Config`) registering the same 13 tools
as `readproof_*` via `defineTool`, recording one Readproof run per DSH session (`dsh-<sessionId>`, committed on session end or lazily), config
`endpoint/apiKey/spawn/readproofdPath/dataDir/addr/sessionRuns/toolPrefix/systemPromptSection/maxInlineBytes`. Install:
`dsh plugin --profile web add ./integrations/deepseek-harness/dsh-plugin-readproof && dsh web`; dev: `dsh web --patch ./integrations/deepseek-harness/readproof-plugin.cordis.yml`;
zero code: `readproof-mcp.cordis.yml` (tools `mcp__readproof__readproof_*`). 26 tests; booted on `@deepseek-ai/dsh@0.1.1-rc.1`; not yet exercised with a live model.

### 5.10b Support agent example (`examples/support-agent/`)
TypeScript agent on `@readproof/sdk` + `ollama` (open models, no API key): three policies (`refunds` require_fresh, `shipping` allow_stale 1h,
`tone` mounted `@prod`), one run per ticket, answer + manifest id in `data/tickets.jsonl`; commands `setup · ask · show · replay · diff · evidence · promote · history`;
`npm run scenario` runs the whole story (also `SUPPORT_FAKE_MODEL=1`); 7 e2e tests; a self contained guide page at `examples/support-agent/guide.html`
(hosted on the site at `/examples/support-agent/`). Real transcript used `deepseek-v4-flash:cloud` via Ollama Cloud.

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
`#overview #how #install #sources #runs #tags #evidence #server #sdk #mcp #langgraph #dsh #support-agent #otel #cli #faq`.
Content to cover under each: overview/thesis; mechanism + the six concepts (+ tags, evidence); install & first resolve
(verbatim output); sources (filesystem/GitHub/HTTP with the header env var pattern) and policies table; runs/manifest/diff/replay
walkthrough with verbatim output; tags workflow; evidence export/verify + bundle shape + what it proves/doesn't + Art. 12/SOC 2
note (not legal advice); server mode, readproofd flags, endpoint table, a curl example; SDK examples; MCP setup for Claude Code /
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
