# LangGraph.js + Ctx

A two-node [LangGraph.js](https://github.com/langchain-ai/langgraphjs) agent
that mounts `ctx://` resources inside a graph node, records the resulting
**manifest id in the checkpoint**, and replays that manifest byte-for-byte
from a later process — even after the underlying documents have changed.

```
START ──▶ load_context ──▶ answer_question ──▶ END
             │                    │
             │ ctx.run(id)        │ model sees exactly the bytes
             │   .mount(uri)…     │ the manifest recorded
             │   .commit()        │
             ▼                    ▼
     ctx_manifest_id  ────────────────────▶  checkpoint (MemorySaver)
                                                    │
                                      graph.getState(config)
                                                    │
                                                    ▼
                                          ctx.replay(manifest_id)
```

## What it proves

1. **Context is a run artifact, not a side effect.** Every document the
   model saw is an ordered entry of one immutable manifest, hashed.
2. **The checkpoint remembers what the agent read.** Not "we called a
   retriever at 14:02" — a manifest id that pins the exact bytes.
3. **Replay is source-independent.** Edit the policy document, replay the
   old manifest: you get the original bytes back, verified by SHA256, with
   no fetch from the live source. A fresh run picks up the new bytes. Both
   at once, in one output.

## Prerequisites

- Node 18+ and Go 1.22+.
- **`ctxd` running on the host, in embedded mode.** The demo resources are
  `filesystem` sources pointing into this repo, so `ctxd` has to be able to
  see those paths — a Compose/Docker `ctxd` cannot (see the root README).

## Run it

From the repository root:

```bash
# 1. Build the SDK this example consumes (a local file: dependency —
#    node_modules/@ctx/sdk symlinks to sdk/typescript, so dist/ must exist).
cd sdk/typescript && npm ci && npm run build && cd ../..

# 2. Start ctxd on the host, embedded mode, its own data dir.
go build -o ctxd ./cmd/ctxd
./ctxd --addr :8080 --data-dir /tmp/ctx-langgraph &

# 3. Build and run the graph.
cd examples/langgraph-ts
npm ci && npm run build
npm run start
npm run replay
```

`npm run start` accepts a question (`npm run start -- "…"`), and both
scripts honour `CTX_SERVER_URL` (default `http://localhost:8080`) and
`CTX_API_KEY` (only if `ctxd` was started with `--api-key`).

`npm run start` prints the mounted entries with their content hashes, the
answer, and the `ctx_manifest_id` it read back out of the checkpoint, then
writes `last-run.json` (`{thread_id, manifest_id, …}`) — the handoff to the
replay script, which runs as a separate process.

## The after-edit proof

`npm run replay` already re-resolves each URI live and reports whether the
source still matches what was recorded. So change the source and look
again — this is the same edit the [refund agent
walkthrough](../refund-agent/README.md) makes:

```bash
printf 'Products can be refunded within 14 days.\n' > ../refund-agent/policies/refunds.md

npm run replay     # replayed bytes: "…within 30 days." + "live source: CHANGED"
npm run start      # a fresh run, fresh manifest, answer: "…within 14 days."

printf 'Products can be refunded within 30 days.\n' > ../refund-agent/policies/refunds.md
```

`npm run replay -- <manifest-or-run-id>` replays any older manifest, so the
first run's manifest keeps returning "30 days" for as long as it exists.
The replay script exits non-zero if any entry's replayed hash differs from
the hash the manifest recorded.

## How it works

`src/graph.ts` — the graph.

- `load_context` opens one Ctx run per LangGraph thread
  (`ctx.run({ id: "langgraph-<thread_id>" })`), `mount()`s each `ctx://`
  URI, and `commit()`s. `mount()` resolves the resource *and* records it as
  the next manifest entry; `commit()` freezes them.
- `ctx_manifest_id` is a **state channel**, not checkpoint metadata: the id
  doesn't exist until the node has run, while metadata is fixed when the
  graph is invoked. State channels live in the same checkpoint record, so
  `graph.getState(config)` reads the id straight back out of it.
- `answer_question` prompts the model with exactly the bytes `load_context`
  recorded — not a second, possibly different read of the same URIs.

`src/run.ts` registers the two demo resources against `ctxd` if it doesn't
know them yet (`ctx://demo/policies/refunds` → the repo's refund-agent
fixture, `ctx://demo/policies/tone` → `context/tone.md`, both `filesystem`
sources with `require_fresh`), then invokes the graph and reads the
manifest id from the checkpoint.

`src/replay.ts` calls `ctx.replay(manifest_id)`, asserts every entry's
`recorded_hash == replayed_hash`, prints the replayed bytes, and re-resolves
each URI live to show whether the world has moved on.

**On the checkpointer:** this uses `MemorySaver`, which is per-process — so
the cross-process handoff here is `last-run.json`. The mechanism is the
part that generalises: swap in `SqliteSaver`/`PostgresSaver` and the same
`getState(config)` call returns the same manifest id from any process, days
later. Nothing else in the example changes.

## Using a real model

The default model is `FakeListChatModel` from `@langchain/core` — no API
key, no network beyond `ctxd`. Its canned reply is derived from the mounted
policy text, which is what makes the run-vs-replay difference visible in
the output.

To use a real one:

```bash
npm install @langchain/anthropic
export ANTHROPIC_API_KEY=sk-ant-...
npm run start
```

`src/graph.ts` picks it up when both the key is set and the (optional,
deliberately un-pinned and un-declared) package is importable; otherwise it
falls back to the fake and says so. Override the model id with
`CTX_ANTHROPIC_MODEL` (default `claude-opus-5`). Everything else — the
graph, the mounts, the manifest, the replay — is identical either way,
which is the point.

## Pinned versions

Exact, no ranges: `@langchain/langgraph` 1.4.12, `@langchain/core` 1.2.9,
`zod` 4.4.3 (a LangGraph peer dependency), `typescript` 5.9.3,
`@types/node` 22.20.1. `@ctx/sdk` is consumed as `file:../../sdk/typescript`.
