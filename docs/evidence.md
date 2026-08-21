# Evidence bundles (`readproof evidence`)

An evidence bundle is a single JSON file that answers one question about
one agent run: **what context was actually delivered, and can we still
prove it?**

It is an [in-toto Statement
v1](https://github.com/in-toto/attestation/blob/main/spec/v1/statement.md)
whose subject digest is a Merkle root over the run's manifest entries, so
it can be signed, stored, and verified by existing supply-chain tooling
(cosign, in-toto verifiers) without those tools knowing anything about
Readproof.

Bundles are built entirely from calls Readproof already answers —
manifest, snapshots, resources, replay — so `readproof evidence` behaves
identically in embedded mode and against a `readproofd`, and the
TypeScript SDK produces the same bytes client-side.

Source: [`internal/evidence`](../internal/evidence),
[`cmd/readproof/evidence.go`](../cmd/readproof/evidence.go),
[`sdk/typescript/src/evidence.ts`](../sdk/typescript/src/evidence.ts).

## CLI

```bash
# metadata only, to stdout
readproof evidence export run-audit-1

# with the delivered bytes embedded, to a file
readproof evidence export run-audit-1 --with-content --out bundle.json

# verify: recompute the root, re-hash embedded bytes, cross-check the store
readproof evidence verify bundle.json

# verify the file on its own, with no store reachable
readproof evidence verify bundle.json --offline
```

Both commands accept a manifest id or a run id, and both work with
`--server https://readproofd.internal` exactly as they do embedded.

`verify` prints one line on success and exits `0`:

```
evidence verified: 2 entries, merkle root 518f2505…a92c, embedded content 2/2 re-hashed, replay match 2/2
```

On failure it prints every check — passing and failing — and exits
non-zero, because *which* checks failed is the whole diagnosis:

```
  ok    merkle_root            518f2505…a92c (2 entries)
  FAIL  content[0]             readproof://demo/policies/refunds: embedded content hashes to sha256:b3689d…, entry records sha256:c8b0bb…
  ok    store_replay[0]        readproof://demo/policies/refunds: sha256:c8b0bb…
Error: evidence verification failed: 1 of 11 checks failed
```

That combination says the *file* was edited: the store still replays the
recorded hash, and the root (which commits to hashes, not bytes) still
verifies. The opposite pattern — every offline check passing but
`store_replay[i]` failing — says the bundle is internally consistent but
no longer matches what Readproof holds.

## TypeScript SDK

```ts
import { Readproof, buildEvidence, encodeEvidence } from "@readproof/sdk";

const rp = new Readproof({ endpoint: "http://localhost:8080" });
const bundle = await buildEvidence(rp, "run-audit-1", { withContent: true });

console.log(bundle.subject[0].digest.sha256);   // the merkle root
await fs.writeFile("bundle.json", encodeEvidence(bundle));
```

`buildEvidence` is composed from `getManifest` / `getSnapshot` /
`getResource` / `replay` and uses Node's `crypto` — no dependencies. For
the same manifest it produces a bundle byte-identical to `readproof
evidence export` apart from `generated_at` / `verified_at`, and `readproof
evidence verify` accepts it.

One limitation: the SDK's `replay()` hands back decoded text, so
`content_b64` is re-encoded from UTF-8. Readproof payloads are text
(markdown, JSON, YAML), but for a genuinely binary source use the Go
exporter, which carries the raw bytes through.

`merkleRoot(entries)` and `merkleLeaf(entry)` are exported too, if you
want to recompute a root without pulling in the whole bundle.

## The JSON shape

```json
{
  "_type": "https://in-toto.io/Statement/v1",
  "subject": [
    { "name": "manifest_01M0…SRZ", "digest": { "sha256": "518f2505…a92c" } }
  ],
  "predicateType": "urn:readproof:evidence:v0.3",
  "predicate": {
    "run_id": "run-audit-1",
    "manifest_id": "manifest_01M0…SRZ",
    "manifest_created_at": "2026-08-20T22:42:21.761651Z",
    "generated_at": "2026-08-20T22:42:27.389302Z",
    "exporter": { "name": "readproof", "version": "0.3.2" },
    "merkle": {
      "algorithm": "sha256",
      "leaf": "sha256(position_be_uint32 || 0x00 || uri || 0x00 || content_hash)",
      "root": "518f2505…a92c"
    },
    "entries": [
      {
        "position": 0,
        "uri": "readproof://demo/policies/refunds",
        "snapshot_id": "snap_01M0…PQJ",
        "materialization_id": "mat_01M0…126",
        "content_hash": "sha256:c8b0bb21…aedb",
        "source_revision": "sha256:c8b0bb212e93",
        "observed_at": "2026-08-20T22:42:21.599166Z",
        "content_type": "text/markdown",
        "bytes": 41,
        "provenance": { "path": "/srv/policies/refunds.md", "source_type": "filesystem" },
        "content_b64": "UHJvZHVjdHMgY2FuIGJl…"
      }
    ],
    "resources": [
      {
        "uri": "readproof://demo/policies/refunds",
        "namespace": "demo",
        "path": "policies/refunds",
        "source": {
          "kind": "http",
          "config": { "http": { "url": "https://policies.internal/refunds", "headers": { "Authorization": "[REDACTED]" } } }
        },
        "policy": { "strategy": "allow_stale", "max_age_seconds": 3600 }
      }
    ],
    "replay": {
      "verified_at": "2026-08-20T22:42:27.389302Z",
      "all_match": true,
      "entries": [
        { "position": 0, "match": true, "expected_hash": "sha256:c8b0bb21…aedb", "actual_hash": "sha256:c8b0bb21…aedb" }
      ]
    }
  }
}
```

Notes on the fields:

- **`predicateType` is still provisional.** `urn:readproof:evidence:v0.3`
  will change again if the predicate schema does. It lives in exactly one
  const per implementation (`evidence.PredicateType`,
  `EVIDENCE_PREDICATE_TYPE`) so a bump is a one-line change. It changed
  from `urn:ctx:evidence:v0.2` in v0.3.0, when the project was renamed —
  verifiers pinned to the old URN must be updated.
- **`content_b64` appears only with `--with-content`.** Without it the
  bundle is metadata-only: it names what the agent read and proves the
  hashes, without reproducing content an auditor may not be cleared to
  see.
- **Source config is always redacted**, through the same
  [`internal/redact`](../internal/redact) rules the API responses use,
  including in embedded mode where the raw values never crossed a wire. A
  bundle is built to be exported; it must never carry a credential.
- **`resources[i].missing: true`** records a URI whose resource
  definition was deregistered after the run. The manifest is still
  replayable, so this is recorded rather than fatal.
- **`replay.error`** is set when replay could not run at all (a blob is
  gone, the store is unreachable). The export still succeeds — an
  un-replayable manifest is exactly the thing worth having a record of.
- Timestamps are RFC 3339. Entry order is manifest position order and is
  never sorted; map keys (`provenance`, `headers`) are sorted so both
  exporters emit identical bytes.

## The Merkle rule

Leaf, for each entry:

```
leaf = sha256(position_be_uint32 || 0x00 || uri || 0x00 || content_hash)
```

`position` is a fixed-width big-endian `uint32`; `uri` and
`content_hash` are UTF-8, `0x00`-separated so no two distinct entries can
serialize to the same bytes. `content_hash` is hashed as the recorded
string, `"sha256:"` prefix included.

Root, over the leaves **in position order**:

- **zero entries** → `sha256` of the empty input
  (`e3b0c442…b855`)
- **one entry** → the root is that entry's leaf
- **odd number of nodes at a level** → the last node is duplicated and
  paired with itself (the Bitcoin rule), then
  `parent = sha256(left || right)`

Only `position`, `uri` and `content_hash` feed the root. Descriptive
fields — `observed_at`, `bytes`, `provenance`, `content_b64` — do not, so
two exports of the same manifest always agree.

Entry order is deliberately part of the digest: in Readproof, the order
context was mounted in can change what a model does with it, so the same
two resources in the other order is a different context and digests
differently.

The duplicate-last rule admits CVE-2012-2459-style collisions between
differently shaped trees. That is acceptable here because a bundle always
carries its full entry list: a verifier recomputes the root from a known
entry count rather than trusting a bare root.

## What `verify` proves — and what it does not

`readproof evidence verify` runs these checks:

| Check | What it establishes |
| --- | --- |
| `statement_type`, `predicate_type` | The file is a bundle this verifier understands |
| `subject`, `merkle_root`, `predicate_merkle_root` | The signed digest is the Merkle root of exactly these entries, in this order |
| `entry_order` | Positions are `0..n-1` in order — the invariant the leaves commit to |
| `content[i]` | Embedded bytes hash to the `content_hash` recorded for that entry |
| `store_replay[i]`, `store_replay_count` | The Readproof store, replayed *now*, still reconstructs the same hashes (skipped with `--offline`) |

**It proves**: these exact bytes, in this order, were resolved and
recorded by Readproof for this run; the record has not been edited since
it was exported; and (without `--offline`) the store still reconstructs
the same content from its own blobs, independently of whether the original
source is still reachable or still says the same thing.

**It does not prove**:

- **that the model used them.** Readproof records what was delivered to the
  agent, not what the agent attended to, or what it put in a prompt.
- **that the source was authoritative or correct.** A bundle proves the
  bytes came from the configured source at `observed_at`, not that the
  source held the right answer.
- **who exported it.** A bundle is unsigned. Sign it — it is a valid
  in-toto Statement — if you need authorship or non-repudiation.
- **that nothing else reached the model.** Context resolved outside Readproof
  (hardcoded prompts, tool output, retrieval the agent did itself) is
  invisible here, by construction.
- **anything at all, offline, about a re-rooted forgery.** Someone who
  edits an entry *and* recomputes the root produces an internally
  consistent file. Only the store cross-check (or an external signature)
  catches that, which is why `--offline` is opt-in rather than the
  default.

## Why this shape: audit and compliance framing

> **This is not legal advice.** Nothing here is a compliance
> certification, and no artifact Readproof produces makes a system compliant
> with anything. Regulatory obligations depend on your system, your role,
> and your jurisdiction — take the framing below as engineering context
> for *why* the bundle records what it records, and talk to counsel about
> what you actually owe.

Two recurring asks shaped this format:

**EU AI Act, Article 12 (record-keeping).** High-risk AI systems are
expected to technically allow for the automatic recording of events over
their lifetime, with traceability of the system's functioning that is
appropriate to its purpose. For a context-driven agent, a large part of
"functioning" is which documents, at which versions, were in front of the
model for a given decision. A bundle pins that per run: content-addressed
hashes, the resolution policy in force, and a reconstruction check —
rather than a log line asserting that a document was read.

**SOC 2 (and internal audit) — "what did the agent see?"** The evidence
an auditor asks for is usually not "show me your logs" but "for this
customer's disputed decision on this date, show me the policy text the
system used, and show me it hasn't been edited since." Content hashes
plus the replay check answer that with an artifact that can be attached
to a ticket, mailed out of the building, and re-verified later by someone
without access to your database. `--with-content` decides whether the
recipient gets the text itself or only the proof that a specific text was
used.

Two practical consequences of that framing:

- **Export at decision time, not at audit time.** A bundle is a snapshot
  of what Readproof could prove when it was written; exporting only after a
  dispute means the store must still be intact.
- **Sign bundles you intend to rely on.** Verification proves internal
  consistency and agreement with the store. It says nothing about who
  produced the file, and in-toto tooling handles that part.
