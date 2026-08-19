# @ctx/sdk

TypeScript SDK for [Ctx](../../README.md) — talks to a running `ctxd` over
HTTP. Every method maps 1:1 to one of `ctxd`'s API endpoints; see the root
README's "HTTP API" section for the full list.

## Install

Not yet published to a registry. Install locally from this repo:

```bash
cd sdk/typescript
npm install
npm run build
npm link              # or: npm pack, then npm install /path/to/ctx-sdk-*.tgz
```

## Usage

```ts
import { Ctx } from "@ctx/sdk";

const ctx = new Ctx({ endpoint: "http://localhost:8080" });

const policy = await ctx.resolve("ctx://acme/policies/refunds");
console.log(policy.content, policy.snapshot.id, policy.snapshot.content_hash);
```

### Manifest-aware resolution

Mounting a resource inside a `run` resolves it *and* records it as the next
ordered entry in the manifest `commit()` produces — the same effect as the
CLI's `ctx run --id <run-id> <uri>`:

```ts
const run = ctx.run({ id: "run_9182" });

const refundPolicy = await run.mount("ctx://acme/policies/refunds");
const customer = await run.mount(`ctx://acme/customers/${customerId}`);

const manifest = await run.commit();
```

The run starts lazily on the first `mount()` — no separate setup call
needed.

### Registering a resource

```ts
await ctx.registerResource({
  uri: "ctx://acme/policies/refunds",
  source: { kind: "github", github: { owner: "acme", repo: "company-docs", path: "policies/refunds.md", ref: "main" } },
  policy: { strategy: "require_fresh" },
});
```

### Diff and replay

```ts
const diff = await ctx.diff("run-a", "run-b");
for (const entry of diff.entries) {
  if (entry.status === "changed") console.log(entry.unified_diff);
}

const replay = await ctx.replay("run-a");
console.log(replay.entries.every((e) => e.match)); // -> true
```

## Development

```bash
npm install
npm run build     # tsc -> dist/
npm test          # build, then run node --test against dist/test/
npm run example   # build, then run examples/resolve.ts against $CTX_ENDPOINT (default http://localhost:8080)
```

`npm run example` needs a running `ctxd` — from the repo root:
`docker compose up -d --build`.

## Notes

- No runtime dependencies — uses Node's global `fetch` (Node 18+).
- `content` fields (resolve, replay) are decoded to UTF-8 text by the SDK;
  the wire format is base64, matching how binary-safe `[]byte` fields
  serialize on the Go side.
- Consumer/dependency tracking (spec's `consumer` field on a run) isn't
  implemented server-side yet, so it isn't exposed here either — only
  `run_id` threading, which `ctxd` does support.
