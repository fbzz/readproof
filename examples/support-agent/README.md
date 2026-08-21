# Support Agent — a real agent on Ctx, with an open model

> Prefer a web page? [`guide.html`](guide.html) explains everything on this
> page — architecture, every file and command, the real transcript, tests —
> as one self-contained HTML file.

A customer-support agent that answers tickets from three policy documents,
with an open model served by [Ollama](https://ollama.com) — no Claude, no
Anthropic SDK, no API key. Every ticket is one **Ctx run**: the agent mounts
`ctx://acme/policies/refunds`, `ctx://acme/policies/shipping` and
`ctx://acme/policies/tone@prod`, hands the model *exactly* those bytes,
commits a manifest, and stores the manifest id next to the answer. When the
refund policy changes from 30 days to 14 days a week later, `diff` says which
document moved and why, `replay` reconstructs the exact text the old answer
was based on, and `evidence` exports an in-toto bundle an auditor can verify
with the Go CLI — while the house-style document, mounted by tag, doesn't move
at all until somebody deliberately promotes it.

## Prerequisites

- **Go 1.26+** — the scenario builds `ctx` and `ctxd` from this repo.
- **Node 18+** — the agent is TypeScript, and uses the global `fetch`.
- **Ollama** with a chat model, for the real run:
  ```bash
  ollama serve            # if it isn't already running
  ollama pull llama3.2    # or any chat model you like
  ```
  `OLLAMA_HOST=https://…` points the example at a remote or cloud-backed
  Ollama instead. **Or skip Ollama entirely** with `SUPPORT_FAKE_MODEL=1`
  (see [Fake mode](#fake-mode)) — everything Ctx does is unchanged.

## Quickstart

```bash
cd examples/support-agent
npm install
npm run scenario                                       # needs Ollama
OLLAMA_MODEL=llama3.2 npm run scenario                 # pick the model
SUPPORT_FAKE_MODEL=1 npm run scenario                  # no Ollama at all
```

`scripts/scenario.sh` is self-contained: it builds `ctx` and `ctxd`, starts a
throwaway `ctxd` on `:18090` with its own data directory, runs the whole story
below, and restores the two policy fixtures it edits — always, including on
failure and on Ctrl-C.

## The story, step by step

Everything below is real output from
`OLLAMA_MODEL=deepseek-v4-flash:cloud bash scripts/scenario.sh`, with
snapshot ids and hashes trimmed. Run the individual commands yourself with
`npm run agent -- <command>` once a `ctxd` is up.

### 1. `setup` — put the policies under Ctx

```
$ npm run agent -- setup
ctxd     http://localhost:18090 ok
register ctx://acme/policies/refunds -> …/context/policies/refunds.md (require_fresh)
register ctx://acme/policies/shipping -> …/context/policies/shipping.md (allow_stale, max age 3600s)
register ctx://acme/policies/tone -> …/context/policies/tone.md (require_fresh)
tag      ctx://acme/policies/tone@prod -> snap_01M0HW18V0…

3 policies governed by Ctx, 3 registered just now.
```

Three documents, three deliberately different freshness contracts. `setup` is
idempotent: run it twice and it registers nothing and moves no tag.

### 2. `ask 1001` — answer a ticket while the policy says 30 days

```
$ npm run agent -- ask 1001 "I bought headphones 20 days ago. Can I still get a refund?"
ticket:   1001
question: I bought headphones 20 days ago. Can I still get a refund?

model: deepseek-v4-flash:cloud
Yes, you can still get a refund. Per the refund policy, products can be refunded
within 30 days of delivery, and your purchase is 20 days old, so you're within the
window. The refund will go to your original payment method within 5 business days.
To proceed, please start a return request through your account or reply with your
order number.

manifest: manifest_01M0HW1AQ9…
POS  URI@REF                           SNAPSHOT             HASH
0    ctx://acme/policies/refunds       snap_01M0HW194M…     sha256:72be2c034713
1    ctx://acme/policies/shipping      snap_01M0HW194S…     sha256:e8178eaf5ca5
2    ctx://acme/policies/tone@prod     snap_01M0HW18V0…     sha256:c04b1f6dbc3c
```

Tokens stream to stdout as the model produces them. The three entries are the
manifest — ordered, content-addressed, immutable.

### 3. Somebody edits the refund policy

```
$ printf '# Refund policy\n\nProducts can be refunded within 14 days of delivery. Refunds go to the original payment method within 5 business days.\n' \
    > context/policies/refunds.md
```

Nobody told the agent. Nothing was redeployed.

### 4. `ask 1002` — same question, different answer

```
$ npm run agent -- ask 1002 "I bought headphones 20 days ago. Can I still get a refund?"
model: deepseek-v4-flash:cloud
Per the refund policy, products can be refunded within 14 days of delivery. Since
your purchase was 20 days ago, it's past that window, so a refund isn't possible.
If you have a different issue, like a defect, please check the warranty or contact
support for options. Next step: reply with your order number if you'd like us to
review any other concerns.

manifest: manifest_01M0HW1CFV…
POS  URI@REF                           SNAPSHOT             HASH
0    ctx://acme/policies/refunds       snap_01M0HW1AZH…     sha256:3117512b66c3
1    ctx://acme/policies/shipping      snap_01M0HW194S…     sha256:e8178eaf5ca5
2    ctx://acme/policies/tone@prod     snap_01M0HW18V0…     sha256:c04b1f6dbc3c
```

The model flipped its decision. `require_fresh` on the refunds policy is why:
Ctx re-verified the source and delivered a new snapshot. Shipping and tone are
byte-identical to ticket 1001.

### 5. `diff 1001 1002` — which document moved, and why

```
$ npm run agent -- diff 1001 1002
--- ticket 1001 (manifest_01M0HW1AQ9…)
+++ ticket 1002 (manifest_01M0HW1CFV…)

~ ctx://acme/policies/refunds  (snap_01M0HW194M… -> snap_01M0HW1AZH…)
  why: source revision sha256:72be2c034713 -> sha256:3117512b66c3; observed 2026-08-21T09:57:02Z -> 2026-08-21T09:57:04Z
  --- a/ctx://acme/policies/refunds
  +++ b/ctx://acme/policies/refunds
  @@ -1,4 +1,4 @@
   # Refund policy
   
  -Products can be refunded within 30 days of delivery. Refunds go to the original payment method within 5 business days.
  +Products can be refunded within 14 days of delivery. Refunds go to the original payment method within 5 business days.

= ctx://acme/policies/shipping  (snap_01M0HW194S…)

= ctx://acme/policies/tone  (snap_01M0HW18V0…)

1 resource changed, 0 added, 0 removed, 2 unchanged
```

This is the question "why did the agent answer differently?", answered with a
document name, a revision, and a timestamp — not a hunch.

### 6. `replay 1001` — the bytes the old answer was actually based on

```
$ npm run agent -- replay 1001
ticket:   1001
manifest: manifest_01M0HW1AQ9…  (answered 2026-08-21T09:57:04Z)

  [0] ctx://acme/policies/refunds
        recorded sha256:72be2c034713…
        replayed sha256:72be2c034713…   MATCH
      | # Refund policy
      | 
      | Products can be refunded within 30 days of delivery. Refunds go to the original payment method within 5 business days.
        live source: CHANGED -> sha256:3117512b66c3…

  [1] ctx://acme/policies/shipping
        recorded sha256:e8178eaf5ca5…
        replayed sha256:e8178eaf5ca5…   MATCH
      | # Shipping policy
      | 
      | Orders ship within 2 business days. Standard delivery takes 3-5 business days, express 1-2. Tracking is emailed as soon as the label is created.
        live source: unchanged

  [2] ctx://acme/policies/tone
        recorded sha256:c04b1f6dbc3c…
        replayed sha256:c04b1f6dbc3c…   MATCH
      | # House style for support replies
      | 
      | Keep replies under 120 words. Use plain language and no jargon. Name the policy you relied on, in the form "per the refund policy". Close with one concrete next step the customer can take.
        live source: unchanged

Replay verified: 3/3 entries match.
1 of them no longer match the live source — the manifest, not the source, is what a replay reads.
```

The refund policy on disk says 14 days. The replay says 30 days, because
that is what the agent read at 09:57:04 — reconstructed from Ctx's store,
with no fetch. Replay is strict: any hash mismatch exits non-zero.

### 7. `evidence 1001` — hand it to someone who doesn't trust you

```
$ npm run agent -- evidence 1001 --out ticket-1001.bundle.json --with-content
evidence bundle written to …/ticket-1001.bundle.json
  entries:     3 (with embedded content)
  merkle root: e8d8572c0d97…
  replay:      all entries match

verify it with the Go CLI:
  ctx --server http://localhost:18090 evidence verify …/ticket-1001.bundle.json
```

```
$ ctx --server http://localhost:18090 evidence verify ticket-1001.bundle.json
evidence verified: 3 entries, merkle root e8d8572c0d97…, embedded content 3/3 re-hashed, replay match 3/3
```

The bundle is an [in-toto Statement](../../docs/evidence.md) built by the
TypeScript SDK's `buildEvidence()`; the Go verifier recomputes the Merkle
root, re-hashes the embedded bytes, and cross-checks the store by replay.
Flip one base64 character and it exits non-zero — `test/agent.test.ts` asserts
exactly that.

### 8. Edit the house style — and watch nothing happen

```
$ printf '\nAlways open with a one-line summary of the decision.\n' >> context/policies/tone.md
$ npm run agent -- ask 1003 "I bought headphones 20 days ago. Can I still get a refund?"
model: deepseek-v4-flash:cloud
Per the refund policy, products can be refunded within 14 days of delivery. Since
your headphones were delivered 20 days ago, the refund window has passed, so a
refund is not available. As a next step, please contact our support team if you
have any other questions or concerns.

manifest: manifest_01M0HW1GMK…
POS  URI@REF                           SNAPSHOT             HASH
0    ctx://acme/policies/refunds       snap_01M0HW1E27…     sha256:3117512b66c3
1    ctx://acme/policies/shipping      snap_01M0HW194S…     sha256:e8178eaf5ca5
2    ctx://acme/policies/tone@prod     snap_01M0HW18V0…     sha256:c04b1f6dbc3c
```

The tone entry is `snap_01M0HW18V0…` — the same snapshot ticket 1001 used,
three edits ago. The agent mounts `tone@prod`, and a tag delivers exactly the
snapshot it points at: **no source fetch, and the freshness policy is not
consulted**. Editing the file is not deploying it. Deploying it is:

```
$ npm run agent -- promote tone
ctx://acme/policies/tone@prod -> snap_01M0HW6DMN…

$ npm run agent -- history tone
ctx://acme/policies/tone
SNAPSHOT                         OBSERVED                      CONTENT_HASH          TAGS
snap_01M0HW6DMN…                 2026-08-21T09:59:51Z          sha256:8ed369c0dc9b   prod
snap_01M0HW6D8B…                 2026-08-21T09:59:50Z          sha256:c04b1f6dbc3c
```

`promote` with no snapshot id resolves the resource first — "current" has to
mean what the resource's own freshness policy says is current *now*, or
promoting right after an edit would re-promote the snapshot from before it.
The tag now points at the edited house style; the previous snapshot is still
there, and `promote tone <old-snapshot-id>` rolls back to it.

## Commands

| Command | What it does |
| --- | --- |
| `setup` | Check `ctxd`, register the three policies idempotently, tag `tone@prod` |
| `ask <ticket> <question…>` | One Ctx run: mount → model → commit → append to the ticket log |
| `show <ticket>` | The stored answer, plus the manifest read back from `ctxd` |
| `replay <ticket>` | Reconstruct the exact bytes that answer used; compare against the live source |
| `diff <a> <b>` | What changed in the context between two tickets, with provenance |
| `evidence <ticket> [--out <file>] [--with-content]` | Write an in-toto evidence bundle |
| `promote <policy> [snapshot-id]` | Move the `@prod` tag; with no id, resolves the resource and promotes what its policy says is current |
| `history <policy>` | Snapshots and tags for one policy |

`<policy>` is a `ctx://` URI or one of the short names `refunds`, `shipping`,
`tone`. `npm run agent -- --help` prints all of this.

## Environment

| Variable | Default | What it does |
| --- | --- | --- |
| `CTX_ENDPOINT` | `http://localhost:8080` | Base URL of the `ctxd` to talk to. `CTX_SERVER_URL` (the Go CLI's variable) is accepted as a fallback. |
| `CTX_API_KEY` | — | Bearer token, if `ctxd` was started with `--api-key`. |
| `OLLAMA_HOST` | `http://localhost:11434` | Where Ollama is. The `ollama` JS client does **not** read this itself (it hardcodes `127.0.0.1:11434`), so `src/config.ts` reads it and passes it to the client. |
| `OLLAMA_MODEL` | first non-embedding model Ollama lists | Which chat model answers. |
| `SUPPORT_FAKE_MODEL` | — | `1` = deterministic fake model, no Ollama. |
| `SUPPORT_CONTEXT_DIR` | `./context/policies` | Where the policy files live. The tests point this at a throwaway copy. |
| `SUPPORT_DATA_DIR` | `./data` | Where `tickets.jsonl` is written. |

### Model resolution

1. `OLLAMA_MODEL`, if set, wins.
2. Otherwise `ollama.list()` is called and the first model whose name does not
   contain `embed` is used — `ollama list` happily returns `nomic-embed-text`,
   which cannot chat.
3. If there is no such model: `set OLLAMA_MODEL or: ollama pull llama3.2`.

A connection failure prints a single line naming `OLLAMA_HOST` and asking
whether `ollama serve` is running.

## Fake mode

`SUPPORT_FAKE_MODEL=1` replaces the model call with a deterministic function
that answers out of the refund policy's first sentence. Everything else — the
run, the mounts, the manifest, the tag, the diff, the replay, the evidence
bundle — is exactly the same code path.

```bash
SUPPORT_FAKE_MODEL=1 npm run scenario
npm test                       # the test suite always runs this way
```

That is what CI runs, and it is the point: the guarantees this example
demonstrates are properties of Ctx, not of the model.

## Tests

```bash
npm test
```

`test/agent.test.ts` builds `ctx` and `ctxd` with `go build`, starts a
throwaway `ctxd` on a free port, **copies the three policy fixtures into a
temp directory and registers those**, then asserts:

1. `setup` registers three resources with their declared policies and creates
   `tone@prod` — and is idempotent.
2. `ask` commits a manifest with three entries in mount order, the third
   carrying `ref === "prod"`.
3. Editing `refunds.md` produces exactly one `changed` diff entry, with
   `source_revision_a !== source_revision_b` and both `observed_at` fields.
4. Replaying the first ticket still matches and returns the **old** bytes,
   while resolving the same URI live returns the new ones.
5. Editing `tone.md` leaves the tone entry's snapshot identical (pinned by the
   tag) while the refunds entry moves.
6. The evidence bundle's `subject[0].digest.sha256` equals `merkleRoot()`
   recomputed from its entries, `ctx evidence verify` exits 0 — and exits
   non-zero after one byte of `content_b64` is flipped.
7. The repository's own fixtures were never touched.

## What it proves

- **The bytes the model saw are recoverable.** Not a log line saying which
  document was retrieved — the document, at the revision it had, re-derived
  from content hashes and checked against them.
- **"Why did the answer change?" has a mechanical answer.** One resource, one
  revision, one timestamp, one unified diff.
- **A tag is a deployment boundary.** `tone@prod` means an edit to the file is
  inert until someone promotes it, and that promotion is a recorded, revertible
  act. Meanwhile `refunds` under `require_fresh` picks up changes immediately,
  because for refunds that is the correct behavior. Both contracts, same agent,
  declared per resource.
- **None of it depends on the model.** Swap `deepseek-v4-flash:cloud` for
  `llama3.2`, or for the fake function, and every guarantee above holds
  unchanged.

## How it maps to Ctx

| Ctx concept | Here |
| --- | --- |
| **Identity** | `ctx://acme/policies/{refunds,shipping,tone}` — stable names, independent of the files behind them |
| **Policy** | `require_fresh` on refunds (money), `allow_stale` 1h on shipping (rarely changes), `require_fresh` on tone (but mounted by tag, so it never fetches) |
| **Tag** | `tone@prod`, moved only by `promote` — an explicit deployment of house style |
| **Run** | One per ticket, id `ticket-<id>`; `mount()` resolves *and* records |
| **Manifest** | The three ordered entries `commit()` freezes; its id is stored with the answer in `data/tickets.jsonl` |
| **Diff** | `diff 1001 1002`, with per-side `source_revision` / `observed_at` / `ref` |
| **Replay** | `replay 1001` — strict SHA256 reconstruction, no source fetch |
| **Evidence** | `evidence 1001` — in-toto Statement, Merkle root over the entries, verified by the Go CLI |

## Files

```
src/config.ts     endpoints, model settings, the three resource definitions
src/model.ts      Ollama chat (streaming) + the deterministic fake model
src/agent.ts      setup(), ask() — one Ctx run per ticket — and loadTicket()
src/cli.ts        the commands above
scripts/scenario.sh   the whole story, self-contained
test/agent.test.ts    end-to-end against a real ctxd, fake model
context/policies/     the three governed documents
```

Closest neighbors in this repo: [`examples/refund-agent`](../refund-agent)
(the same invariant, driven from the shell) and
[`examples/langgraph-ts`](../langgraph-ts) (the manifest id living in a
LangGraph checkpoint).
