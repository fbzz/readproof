# MCP server (`ctx mcp`)

`ctx mcp` serves a Ctx deployment over the [Model Context
Protocol](https://modelcontextprotocol.io) on stdio, so an agent host —
Claude Code, Claude Desktop, Cursor — can read governed documents through
Ctx instead of fetching files and URLs directly.

The difference that buys: every read the model performs is resolved
through a freshness policy, recorded as an immutable snapshot with a
content hash and a source revision, and can be pinned to a tag, grouped
into a run, replayed byte-for-byte, diffed against another run, and
exported as an evidence bundle. The model gets documents; you get a record
of exactly which bytes it saw.

The server is built on the same `client.Client` every CLI command uses, so
it honors the global flags: `--data-dir` runs it embedded over a local
data directory, `--server` / `--api-key` runs it against a `ctxd`. Nothing
about the MCP surface changes between the two.

Source: [`internal/mcp`](../internal/mcp), [`cmd/ctx/mcp.go`](../cmd/ctx/mcp.go).

## Setup

`ctx mcp` is launched *by* the MCP client as a subprocess and speaks
JSON-RPC on stdin/stdout — you never run it by hand. Every path in the
configuration must be **absolute**: the client chooses the working
directory, so a relative `--data-dir` will not resolve to what you expect.

### Claude Code

```bash
# embedded: one local data directory
claude mcp add ctx -- /abs/path/to/ctx mcp --data-dir /abs/path/to/.ctx

# against a running ctxd (API key from the environment, not the command line)
claude mcp add ctx --env CTX_API_KEY=sk-... -- /abs/path/to/ctx mcp --server https://ctxd.internal
```

Check it came up with `claude mcp list`, and remove it with
`claude mcp remove ctx`.

`--api-key` also works as a flag, but a flag is visible in the process
list to every user on the machine; `CTX_API_KEY` is not.

### Claude Desktop

`claude_desktop_config.json` (macOS:
`~/Library/Application Support/Claude/claude_desktop_config.json`;
Windows: `%APPDATA%\Claude\claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "ctx": {
      "command": "/abs/path/to/ctx",
      "args": ["mcp", "--data-dir", "/abs/path/to/.ctx"]
    }
  }
}
```

Against a `ctxd`:

```json
{
  "mcpServers": {
    "ctx": {
      "command": "/abs/path/to/ctx",
      "args": ["mcp", "--server", "https://ctxd.internal"],
      "env": { "CTX_API_KEY": "sk-..." }
    }
  }
}
```

Restart Claude Desktop after editing the file.

### Cursor

`.cursor/mcp.json` in the project (or `~/.cursor/mcp.json` globally):

```json
{
  "mcpServers": {
    "ctx": {
      "command": "/abs/path/to/ctx",
      "args": ["mcp", "--data-dir", "/abs/path/to/.ctx"]
    }
  }
}
```

## What the server exposes

### Instructions

The MCP `initialize` response carries an `instructions` paragraph that
tells the model what Ctx is before it calls anything: resources are
versioned and policy-governed, `@<tag>` pins an exact snapshot, and a run
(`ctx_run_start` → `ctx_run_mount` → `ctx_run_commit`) produces a manifest
id that can later be inspected, diffed, replayed, and exported.

### Resources

`resources/list` returns every registered resource, live from the store —
a resource registered by another process while the server is running shows
up on the next list, no restart needed.

| Field | Value |
| --- | --- |
| `uri` | the Ctx URI, `ctx://<namespace>/<path>` |
| `name` | the resource path, e.g. `policies/refunds` |
| `title` | the full URI |
| `description` | source kind, freshness policy, and origin — e.g. `github · require_fresh — acme/company-docs:policies/refunds.md@main` |
| `mimeType` | the current snapshot's content type, once one exists |
| `size` | the current snapshot's byte count |
| `_meta` | `namespace`, `path`, `source` (**redacted**), `policy`, `current_snapshot_id` |

`resources/templates/list` returns one template, `ctx://{namespace}/{+path}`.
It is what makes tagged reads possible: no static listing can enumerate
every `@<tag>`, so **`ctx://<namespace>/<path>@<tag>` is readable via
`resources/read` even though it never appears in `resources/list`.**

`resources/read` resolves the URI exactly as `ctx get` does — the
resource's freshness policy decides whether the source is re-fetched, and
a trailing `@<tag>` bypasses the policy to deliver one exact snapshot.
Text-like content (`text/*`, `application/json`, `application/*+json`,
`application/xml`, `application/yaml`, and unknown types whose bytes are
valid UTF-8) comes back as text; everything else comes back as a base64
blob.

Each content block carries the provenance in its own `_meta`:

```json
{
  "uri": "ctx://demo/policies/refunds",
  "mimeType": "text/markdown",
  "text": "Products can be refunded within 30 days.\n",
  "_meta": {
    "uri": "ctx://demo/policies/refunds",
    "ref": "",
    "snapshot_id": "snap_01M0GQE8MCRQHJC8E1CY9AQGZT",
    "content_hash": "sha256:c8b0bb212e93151d720746e36ff3b7076727cb577614feafa0d61f168965aedb",
    "source_revision": "sha256:c8b0bb212e93",
    "observed_at": "2026-08-20T23:17:30Z",
    "decision": "fetch",
    "materialization_id": "mat_01M0GQE8MCAYRNZ8RNJ26YZ9GA",
    "content_type": "text/markdown",
    "bytes": 41
  }
}
```

`decision` is `fetch`, `use_current`, `use_pinned`, or `use_tag` — why
these bytes and not others. `ref` is the tag the read was pinned to, `""`
for a plain URI. Reading an unregistered URI, or one whose tag doesn't
exist, returns a proper MCP resource-not-found error.

### Tools

Every tool returns both a JSON text block and `structuredContent`, with an
input schema derived from the handler's argument type. Failures come back
as tool *error results* (`isError: true`) with a readable message, not as
protocol errors, so the model can correct itself.

| Tool | What it does |
| --- | --- |
| `ctx_resources_list` | List every registered document with its source and policy. The discovery call. |
| `ctx_resolve` | Read one document; returns the bytes plus snapshot id, content hash, and source revision. |
| `ctx_history` | List a resource's snapshots, newest first, with the tags pointing at each. |
| `ctx_run_start` | Open a run — the container that records what this piece of work reads. |
| `ctx_run_mount` | Read a document *and* record it in the open run, at the next position. |
| `ctx_run_commit` | Freeze the run into an immutable manifest; returns the **manifest id**. |
| `ctx_manifest` | Show a committed manifest, by manifest id or run id. |
| `ctx_diff` | Compare two runs: added/removed/changed, the unified diff, and each side's source revision, observation time, and tag. |
| `ctx_replay` | Reconstruct a manifest's bytes from storage alone and re-hash them. `include_content: true` returns the bytes. |
| `ctx_tag_set` | Point a named tag at a snapshot, so it can be read as `uri@tag`. |
| `ctx_tag_list` | List a resource's tags and the exact `uri@tag` reference for each. |
| `ctx_tag_delete` | Remove a tag. The snapshot survives, and manifests that mounted it still replay. |
| `ctx_evidence_export` | Build an in-toto evidence bundle for a run (see [`docs/evidence.md`](evidence.md)). `with_content: true` embeds the bytes. |

The `ctx_run_*` trio is the load-bearing one: mount → commit → manifest
id. `ctx_resolve` reads a document; `ctx_run_mount` reads it *and* records
it, which is what later makes `ctx_diff`, `ctx_replay`, and
`ctx_evidence_export` possible. A manifest id is the only handle those
three need.

**Reading has side effects.** `resources/read`, `ctx_resolve`, and
`ctx_run_mount` may fetch from the source and record a new snapshot — that
is how Ctx observes what the model saw, and the tool descriptions say so.
Everything else is read-only and annotated as such.

### Result size

Inline content is capped at 1 MiB. Past that, text is cut on a rune
boundary and a marker is appended naming the content hash of the complete
bytes:

```
[ctx: content truncated — 1048576 of 4210688 bytes shown. The full content is
unchanged and content-addressed as sha256:c8b0bb…; use ctx_replay or
ctx_evidence_export --with-content to obtain all of it.]
```

The `_meta` of a truncated read also carries `"truncated": true`, and tool
results carry `truncated` plus `total_bytes`.

## Try it

With the refund-agent demo registered (see the README), ask the model:

1. *"List the Ctx resources you can read."* → `ctx_resources_list`
2. *"Read `ctx://demo/policies/refunds` and tell me the refund window."* →
   `resources/read`, which records a snapshot.
3. *"Tag that snapshot as `prod`, then read `ctx://demo/policies/refunds@prod`."*
   → `ctx_tag_set`, then a pinned read.
4. Edit the underlying document, then: *"Read the resource again — did it
   change? What does `@prod` say now?"* The plain read returns the new
   bytes; `@prod` still returns the old ones, with `decision: "use_tag"`.
5. *"Start a run called `demo-1`, mount `ctx://demo/policies/refunds`,
   commit it, and tell me the manifest id."* → the `ctx_run_*` trio.
6. *"Diff `demo-1` against `demo-2` and explain why the answer changed."*
   → `ctx_diff`, including the source-revision and observed-at "why".
7. *"Replay `demo-1` and export an evidence bundle for it."* →
   `ctx_replay`, then `ctx_evidence_export`.

Everything the model did is reproducible from the CLI against the same
data directory: `ctx manifest demo-1`, `ctx replay demo-1`,
`ctx evidence export demo-1`.

## Security

- **stdio is a local trust boundary.** The server runs as a subprocess of
  the MCP client, with that user's privileges and no authentication of its
  own. In embedded mode it can read every resource in the data directory,
  and through the filesystem source adapter it can read any file a
  registered resource points at. Register only resources you are willing
  for the connected agent to read.
- **`--server` mode inherits `ctxd`'s auth.** The API key is passed
  straight through by the same client the CLI uses; the MCP layer adds no
  authorization of its own and removes none. Prefer `CTX_API_KEY` in the
  client's `env` block over `--api-key` on the command line, which is
  visible in the process list.
- **Credentials are redacted from everything the model can see.** Source
  configuration surfaced in resource listings and tool results runs
  through [`internal/redact`](../internal/redact), so HTTP header values
  that look like credentials come back as `[REDACTED]` — including in
  embedded mode, where they never crossed a wire.
- **Resolving has side effects.** `resources/read`, `ctx_resolve`, and
  `ctx_run_mount` may contact the configured source. For an `http` or
  `github` resource that means an outbound request initiated by the model.
- **stdout is the protocol channel.** `ctx mcp` writes diagnostics to
  stderr only, and stays quiet unless `--verbose` is passed.

## Not in the MVP

- **HTTP transport.** stdio only for now; the SDK also offers a streamable
  HTTP transport, which is the natural way to serve one shared Ctx
  deployment to many agent hosts. Deliberately deferred — it needs an
  authentication story of its own rather than inheriting `ctxd`'s.
- **Prompts.** No MCP prompts are registered.
- **Resource subscriptions.** No `resources/subscribe`; a client polls
  `resources/list` or re-reads.
