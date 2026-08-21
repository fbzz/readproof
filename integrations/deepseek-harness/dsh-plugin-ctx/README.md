# dsh-plugin-ctx

Ctx as a [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness)
plugin: the documents your agent reads get a stable `ctx://` identity, a
freshness policy, content-addressed snapshots, per-run manifests, diff,
byte-exact replay, and portable evidence bundles — all as model-callable
tools.

The difference that buys: instead of "the model read some file", you get
*this run read exactly these bytes, from this source revision, at this
time* — a manifest id you can cite, diff against another run, replay
byte-for-byte after the sources changed, and export as a tamper-evident
in-toto bundle.

The tool names, descriptions, and result shapes mirror Ctx's MCP server
([`docs/mcp.md`](../../../docs/mcp.md), [`internal/mcp`](../../../internal/mcp))
so the two surfaces cannot drift.

## Tools

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
| `ctx_evidence_export` | Build an in-toto evidence bundle for a run. `with_content: true` embeds the bytes. |

Every tool returns two text blocks: a one-line summary a human can read at a
glance, and the full JSON payload. Failures come back as tool *error
results* with a readable message (`resolve ctx://demo/nope: ctxd: resource
not found`), never as a crash, so the model can correct itself.

**Reading has side effects.** `ctx_resolve` and `ctx_run_mount` may fetch
from the source and record a new snapshot — that is how Ctx observes what
the model saw, and the tool descriptions say so.

`toolPrefix` renames the whole set: `toolPrefix: 'context_'` gives
`context_resolve`, `context_run_mount`, … and the descriptions
cross-reference the renamed tools correctly.

## Prerequisites

A reachable `ctxd`. Either:

- **you already run one** — set `endpoint` (and `CTX_API_KEY` if it was
  started with `--api-key`); or
- **let the plugin start one** — set `spawn: true` with `ctxd` on `PATH`.
  Build it from this repository:

  ```sh
  go build -o /usr/local/bin/ctxd ./cmd/ctxd
  go build -o /usr/local/bin/ctx  ./cmd/ctx     # the CLI, for `ctx evidence verify`
  ```

Register the documents you want the agent to read before it asks for them:

```sh
ctx --server http://127.0.0.1:8080 resource add ctx://demo/policies/refunds \
  --source-type filesystem --path "$PWD/examples/refund-agent/policies/refunds.md" \
  --policy require_fresh
```

(Drop `--server` and pass `--data-dir` instead to register against a local
data directory — including the one a `spawn: true` plugin uses.)

## Install

### A. As a bundle (the normal path)

```sh
cd integrations/deepseek-harness/dsh-plugin-ctx && npm install && npm run build && cd -
dsh plugin --profile web add ./integrations/deepseek-harness/dsh-plugin-ctx
dsh web
```

`dsh plugin add` links the package and appends `dsh-plugin-ctx` to the
profile's `dsh.profile.bundles`, which activates
[`cordis.patch.yml`](./cordis.patch.yml) — one `insert` row with `id: ctx`.
Check it landed without booting:

```sh
dsh --profile web --dump-config      # shows a "# == dsh-plugin-ctx" layer
```

Any profile name works (`--profile ctx`, `--profile tui`, …), but note that
`dsh plugin` initializes a brand-new profile from `@deepseek-ai/dsh-base`
alone, and base composes no runnable app: `dsh --profile ctx` on a profile
holding only base and this plugin has nothing to boot. Add the plugin to a
profile that already has a surface — `web` and `tui` are the built-in ones —
or add a surface bundle to yours. (`dsh web` is an alias for
`dsh --profile web`; `web` does **not** accept the launcher's `--profile`,
so `dsh --profile ctx web` is an error.)

Override any of the defaults in the profile's own
`$DSH_HOME/profiles/web/cordis.patch.yml`. A patch replaces a row's whole
`config`, so restate every key you need, not just the changed one:

```yaml
- insert:
    - id: ctx
      name: dsh-plugin-ctx
      config:
        endpoint: https://ctxd.internal
        sessionRuns: true
        toolPrefix: ctx_
```

Remove it with `dsh plugin --profile web remove dsh-plugin-ctx`.

The package is built TypeScript: `npm run build` must have produced `dist/`
before `dsh plugin add`, because a local (or git) install runs no build
script of ours. Publishing to npm with `dist/` built at pack time removes
that step for users.

### B. Local development overlay

No install and no profile — mount the checkout directly. Run this from the
repository root:

```sh
cd integrations/deepseek-harness/dsh-plugin-ctx && npm install && npm run build && cd -
sed "s#__CTX_REPO__#$PWD#" integrations/deepseek-harness/ctx-plugin.cordis.yml > /tmp/ctx-plugin.cordis.yml
dsh web --patch /tmp/ctx-plugin.cordis.yml --no-open
```

The `sed` is not decoration. **A plugin path in a patch overlay must be
absolute**: a patch contributes configuration but does not change the
profile directory the loader resolves module paths from, and only a row's
`config` is `!!js`-interpolated, so the path cannot be computed at load
time either. [`ctx-plugin.cordis.yml`](../ctx-plugin.cordis.yml) therefore
carries a `__CTX_REPO__` placeholder.

It defaults to `spawn: true`, so it needs `ctxd` on `PATH` (or `CTXD_BIN`
set) and nothing else. Keep `npx tsc -w` running: the loader hot-replaces
the plugin when the built file changes, and because every registration is a
reversible Cordis effect, the old tools are unregistered first.

### C. Zero-code MCP overlay

If you would rather not run TypeScript at all, Ctx's own MCP server works
through `@deepseek-ai/dsh-mcp-client`:

```sh
dsh web --patch "$PWD/integrations/deepseek-harness/ctx-mcp.cordis.yml"
```

This one needs no absolute-path surgery: the row names
`@deepseek-ai/dsh-mcp-client` (a package, resolved from the dsh
installation) and computes the `ctx` binary and data directory in `!!js`
config expressions — `CTX_BIN` and `CTX_HOME` override them. The overlay
also carries a commented `--server` variant that passes `CTX_API_KEY`
explicitly, because the stdio bridge scrubs credential-looking ambient
variables before launching the child.

The tools then appear under the MCP client's server-qualified names —
`mcp__ctx__ctx_resolve`, `mcp__ctx__ctx_run_mount`,
`mcp__ctx__ctx_evidence_export`, and so on.

What you give up on that path: **session runs** (this plugin's own feature),
the `toolPrefix`, the system-prompt section, and Ctx's MCP *resources*
(`resources/list` / `resources/read`) — DSH's MCP client bridges tools only.
What you gain: no build step, and one fewer process model to reason about.

## Configuration

| Field | Type | Default | What it does |
| --- | --- | --- | --- |
| `endpoint` | string | `http://127.0.0.1:8080` | Base URL of a running `ctxd`. Ignored when `spawn` is true. |
| `apiKey` | string | `''` | Bearer token. **Leave it empty and set `CTX_API_KEY`** — a patch file is usually in version control. |
| `spawn` | boolean | `false` | Start a private `ctxd` child instead of using `endpoint`. |
| `ctxdPath` | string | `ctxd` | Executable used when `spawn` is true. |
| `dataDir` | string | `~/.ctx` | `--data-dir` for the spawned `ctxd`. `~` is expanded. |
| `addr` | string | `127.0.0.1:18080` | `--addr` for the spawned `ctxd`; also determines the endpoint. |
| `spawnTimeoutMs` | number | `10000` | How long to wait for the spawned `ctxd` to answer `/healthz`. |
| `sessionRuns` | boolean | `true` | Mirror every model-driven `ctx_resolve` into a run keyed by the DSH session. |
| `toolPrefix` | string | `ctx_` | Prefix for every registered tool name. |
| `systemPromptSection` | boolean | `true` | Contribute a short "how to use Ctx" section to the system prompt (order 150, the tool-guidance band). |
| `maxInlineBytes` | number | `1048576` | Cap on inline content per result. Past it text is cut on a UTF-8 boundary and a marker naming the content hash is appended. |

With `spawn: true` the plugin does not finish loading until the child
answers `/healthz`, and the plugin's disposer kills it — an HMR reload or an
unload takes the child with it rather than leaving a listener on the port.

## Session runs

With `sessionRuns: true` (the default), **every `ctx_resolve` a model makes
is also recorded in a Ctx run of its own**, keyed by the DSH session. You do
not have to ask the model to drive `ctx_run_start` / `ctx_run_mount`, and
you still get a replayable, diffable, exportable manifest for the session.

- The run id is `dsh-<session id>`, and `ctx_resolve`'s result carries
  `session_run: { run_id, position }` so the model can cite it.
- The mount *is* the read: one fetch, and the run records precisely the
  bytes the model received.
- The run is committed when the session ends — the plugin listens to both
  `agent/disposed` and `session/disposed`, because which of them a
  deployment emits depends on its composition.
- It is also committed **lazily** the moment anything asks for its manifest:
  `ctx_manifest`, `ctx_run_commit`, `ctx_diff`, `ctx_replay`, or
  `ctx_evidence_export` on that run id commits it first, so those tools work
  mid-session rather than failing with "manifest not found".
- A committed run refuses further mounts, so a session that keeps reading
  after its run was committed continues in the next *epoch*:
  `dsh-<session>-2`, `-3`, … Each epoch is its own manifest.
- Unloading the plugin commits every open run before shutting down.

The session id comes from `exec.agent.id` inside the tool body — `Agent.id`
is documented as "the single identity shared with `session`"
(`@deepseek-ai/dsh-agent`). A tool call with **no** agent (a direct
`ctx.tools.execute` from another plugin, or a test) has no session, so it is
resolved normally and not mirrored; `session_run` is simply absent from the
result.

Set `sessionRuns: false` to turn all of this off and leave the run lifecycle
entirely to the model.

## Try it

With `ctx://demo/policies/refunds` registered:

1. *"List the Ctx resources you can read."* → `ctx_resources_list`
2. *"Read `ctx://demo/policies/refunds` and tell me the refund window."* →
   `ctx_resolve`, which records a snapshot (and, with session runs on, a
   manifest entry).
3. *"Tag that snapshot as `prod`, then read
   `ctx://demo/policies/refunds@prod`."* → `ctx_tag_set`, then a pinned
   read with `decision: "use_tag"`.
4. Edit the document, then: *"Read the resource again — did it change? What
   does `@prod` say now?"* The plain read returns the new bytes; `@prod`
   still returns the old ones.
5. *"Start a run called `demo-1`, mount `ctx://demo/policies/refunds`,
   commit it, and tell me the manifest id."* → the `ctx_run_*` trio.
6. *"Diff `demo-1` against `demo-2` and explain why the answer changed."* →
   `ctx_diff`, including the source-revision and observed-at "why".
7. *"Replay `demo-1` and export an evidence bundle for it."* → `ctx_replay`,
   then `ctx_evidence_export`.

Everything the model did is reproducible from the CLI against the same
deployment: `ctx --server <endpoint> manifest demo-1`, `… replay demo-1`,
`… evidence export demo-1`.

## Tests

```sh
npm install && npm test
```

The tests are not mocked. They build `ctxd` and `ctx` from this repository,
start a real `ctxd` on a free port over a scratch data directory, register
the refund-agent policy, compose a real Cordis app (`@deepseek-ai/dsh-system-prompt`
+ `@deepseek-ai/dsh-tools` + this plugin) and drive the tools through
`ctx.tools.execute` — the same pipeline the agent loop drives. No model is
involved. They cover the run trio, diff across a real source edit, replay
returning the pre-edit bytes, tag pinning, session runs, `spawn: true`
including the disposer killing the child, and an evidence bundle whose
merkle root matches the SDK's `merkleRoot` *and* which the Go
`ctx evidence verify` accepts.

Requires Go and Node on `PATH`, and network access on first `npm install`.

## Publishing

- `npm publish` with `dist/` built at pack time, so
  `dsh plugin --profile <name> add dsh-plugin-ctx` installs prebuilt code
  and needs no build permission from the user.
- Tag the GitHub repository with the **`dsh-plugin`** topic so it is
  discoverable, and keep the `dsh-plugin` keyword in `package.json`.
- Installing from git instead (`dsh plugin add github:you/repo#<sha>`)
  requires a `prepare` script here *and* a pnpm `allowBuilds` entry from the
  user — which is permission to execute this package's code at install
  time. Prefer npm or a `pnpm pack` tarball.

`@deepseek-ai/cordis` and `@deepseek-ai/dsh-tools` are **peer** dependencies
(with dev copies for building and testing), matching what the harness's own
packages do — a plugin must use the profile's single copy of the tool
registry, not a nested duplicate. `@deepseek-ai/schemastery` is an ordinary
dependency, the same choice `@deepseek-ai/dsh-tools` makes for it.

## Limitations

- **Text only.** The Ctx TypeScript SDK decodes a resolve's bytes to a UTF-8
  string before this plugin sees them, so unlike `ctx mcp` there is no
  base64 branch: a genuinely binary resource comes back mangled. Register
  text documents; use `ctx_replay`/`ctx_evidence_export --with-content` and
  the CLI for anything else.
- **Ctx's MCP resources are not bridged.** This plugin exposes tools only.
  A model reads a document with `ctx_resolve`, not through a resource
  listing.
- **Session-end commit depends on the composition.** `agent/disposed` and
  `session/disposed` are both observed, but a host that tears its process
  down without emitting either leaves the run open until something asks for
  its manifest (or the plugin unloads). The lazy commit exists precisely
  because that guarantee is not universal.
- **Session runs record `ctx_resolve` only.** A `ctx_run_mount` the model
  drives itself goes into the run *it* names, which is the point of that
  tool; the two are deliberately separate.
- **No approval hook.** Resolving can reach out to an `http` or `github`
  source. This plugin registers no `tools/pre-execute` policy, so it
  inherits whatever the deployment's own guards decide.
- **Run start is probed, not queried.** `ctxd` reports a duplicate run id as
  a 500, so opening a session run treats a 500 as "id taken" and tries the
  next epoch (up to 16 times). A different 500 would be misread as a
  collision and eventually surface as "no unused run id".
- **One `ctxd` per plugin instance.** Mounting the plugin twice with
  different endpoints would register conflicting tool names; use one row.
