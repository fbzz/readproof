# @readproof/sdk

TypeScript SDK for [Readproof](../../README.md) — talks to a running `readproofd` over
HTTP. Every method maps 1:1 to one of `readproofd`'s API endpoints; see the root
README's "HTTP API" section for the full list. `buildEvidence` is the one
exception: it composes an evidence bundle client-side out of those calls.

## Install

```bash
npm install @readproof/sdk
```

Node 18+; no runtime dependencies. The package version tracks the Readproof
release it was cut from, so `@readproof/sdk@0.3.2` speaks to a `readproofd`
0.3.x.

Working inside this repository instead — the examples and the DeepSeek
Harness plugin all consume the SDK as a `file:` dependency, so the built
`dist/` has to exist before anything that depends on it will resolve:

```bash
cd sdk/typescript
npm install
npm run build
npm link              # or: npm pack, then npm install /path/to/readproof-sdk-*.tgz
```

## Usage

```ts
import { Readproof } from "@readproof/sdk";

const rp = new Readproof({ endpoint: "http://localhost:8080" });

const policy = await rp.resolve("readproof://acme/policies/refunds");
console.log(policy.content, policy.snapshot.id, policy.snapshot.content_hash);
```

`ReadproofOptions`:

| Option | Default | What it does |
| --- | --- | --- |
| `endpoint` | — | Base URL of a running `readproofd`. Validated in the constructor: must be an absolute `http:`/`https:` URL. |
| `apiKey` | — | Sent as `Authorization: Bearer …` when `readproofd` runs with `--api-key`. |
| `timeoutMs` | `30000` | Aborts a request that has not answered. `fetch` has no timeout of its own, so without this a hung `readproofd` stalls the turn waiting on it. `0` waits indefinitely. |
| `maxResponseBytes` | `16777216` | Refuses (does not truncate) a response body over the cap, before parsing. |
| `fetch` | global `fetch` | Override, e.g. for tests. |

Errors are `ReadproofError` with the HTTP `status` where there was one; any
server text they quote is truncated to ~512 characters, since these messages
reach the model in the agent-tool paths built on this SDK.

### Manifest-aware resolution

Mounting a resource inside a `run` resolves it *and* records it as the next
ordered entry in the manifest `commit()` produces — the same effect as the
CLI's `readproof run --id <run-id> <uri>`:

```ts
const run = rp.run({ id: "run_9182" });

const refundPolicy = await run.mount("readproof://acme/policies/refunds");
const customer = await run.mount(`readproof://acme/customers/${customerId}`);

const manifest = await run.commit();
```

The run starts lazily on the first `mount()` — no separate setup call
needed.

### Tags and `@tag` refs

A tag is a named, movable pointer from a resource to one of its snapshots
(`(uri, tag) → snapshot_id`). Names must match
`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`.

```ts
await rp.setTag("readproof://acme/policies/refunds", "prod", policy.snapshot.id);
await rp.listTags("readproof://acme/policies/refunds");  // Tag[], sorted by name
await rp.deleteTag("readproof://acme/policies/refunds", "prod");
```

`resolve()` and `run().mount()` both accept a trailing `@<tag>`, which
delivers exactly that snapshot: **no source fetch, and the resource's
freshness policy is not consulted.**

```ts
const pinned = await rp.resolve("readproof://acme/policies/refunds@prod");
pinned.freshness.status;   // "use_tag"
pinned.resource.ref;       // "prod"

await run.mount("readproof://acme/policies/refunds@prod");
```

An unknown tag throws a `ReadproofError` naming both the URI and the tag.
Manifest entries record the bare `uri` plus the `ref` they were mounted by,
so moving a tag afterwards can never change what a committed manifest
replays.

### Diff and replay

Diff entries carry per-side provenance — the *why* behind a change:
`source_revision_a`/`_b`, `observed_at_a`/`_b` (RFC 3339), and
`ref_a`/`_b` when a side was mounted by tag. Each field is present only for
a side whose manifest contains that URI.

```ts
const diff = await rp.diff("run-a", "run-b");
for (const entry of diff.entries) {
  if (entry.status !== "changed") continue;
  console.log(entry.uri, entry.source_revision_a, "->", entry.source_revision_b);
  console.log(entry.unified_diff);
}

const replay = await rp.replay("run-a");
console.log(replay.entries.every((e) => e.match)); // -> true
```

### Evidence bundles

`buildEvidence()` assembles an [in-toto
Statement](../../docs/evidence.md) for a manifest id or run id, using only
public SDK calls (`getManifest` / `getSnapshot` / `getResource` /
`replay`). For the same manifest it produces the same Merkle root — and
byte-identical JSON apart from `generated_at` / `verified_at` — as `rp
evidence export`, and `readproof evidence verify` accepts it.

```ts
import { Readproof, buildEvidence, encodeEvidence } from "@readproof/sdk";

const bundle = await buildEvidence(rp, "run-a", { withContent: true });
console.log(bundle.subject[0].digest.sha256);   // the merkle root
await fs.writeFile("bundle.json", encodeEvidence(bundle));
```

`withContent` embeds each entry's bytes as base64 in `content_b64`; without
it the bundle is metadata-only. `merkleRoot(entries)` and
`merkleLeaf(entry)` are exported separately if you only want to recompute a
root. `replay()` hands back decoded text, so `content_b64` is re-encoded
from UTF-8 — for a genuinely binary source, use the Go exporter, which
carries raw bytes through.

## Development

```bash
npm install
npm run build     # tsc -> dist/
npm test          # build, then run node --test against dist/test/
npm run example   # build, then run examples/resolve.ts against $READPROOF_ENDPOINT (default http://localhost:8080)
```

`npm run example` needs a running `readproofd` — from the repo root:
`docker compose up -d --build`.

## Notes

- No runtime dependencies — uses Node's global `fetch` (Node 18+).
  `buildEvidence` additionally uses Node's built-in `node:crypto`.
- `content` fields (resolve, replay) are decoded to UTF-8 text by the SDK;
  the wire format is base64, matching how binary-safe `[]byte` fields
  serialize on the Go side.
- Consumer/dependency tracking (spec's `consumer` field on a run) isn't
  implemented server-side yet, so it isn't exposed here either — only
  `run_id` threading, which `readproofd` does support.
