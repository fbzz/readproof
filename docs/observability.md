# Observability

Readproof is instrumented with [OpenTelemetry](https://opentelemetry.io)
so that the question an agent trace usually cannot answer — **which bytes
did the model actually get, and where did they come from?** — is
answerable from the trace alone, without opening the store.

Two rules shape everything below:

1. **Spans carry the identity of content, never the content.** Hashes,
   snapshot ids, source revisions, byte counts, observation times: yes.
   Resolved bytes: never, in any attribute, event, or status. Traces get
   copied into third-party backends; the bytes stay in Readproof.
   ([`internal/run/run_telemetry_test.go`](../internal/run/run_telemetry_test.go)
   and
   [`internal/resolver/resolver_telemetry_test.go`](../internal/resolver/resolver_telemetry_test.go)
   scan every recorded attribute and event for the fixture's bytes and
   fail if they appear.)
2. **No collector, no cost.** With `OTEL_EXPORTER_OTLP_ENDPOINT` unset,
   `telemetry.Init` is a no-op and every instrumentation call in the
   codebase resolves to a no-op implementation. Instrumentation never
   requires infrastructure to be running.

Source: [`internal/telemetry`](../internal/telemetry),
[`internal/resolver/resolver.go`](../internal/resolver/resolver.go),
[`internal/run/run.go`](../internal/run/run.go).

## Enabling export

Both binaries call `telemetry.Init` at startup and read the standard OTLP
environment variables (the `otlptracehttp` / `otlpmetrichttp` exporters
parse them directly):

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
readproof run --id r readproof://demo/policies/refunds@prod
```

`service.name` is `readproof` for the CLI and `readproofd` for the daemon.

`docker compose up -d` already runs a collector (`otel-collector`, OTLP on
`4317`/`4318`) and points `readproofd` at it. Its `debug` exporter prints
everything it receives, which is enough to eyeball a trace:

```bash
docker compose up -d
docker compose logs -f otel-collector          # readproofd's spans and metrics
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 \
  readproof run --id r readproof://demo/policies/refunds   # the CLI's, via the mapped port
```

To send to a real backend instead, point
[`otel-collector-config.yaml`](../otel-collector-config.yaml) at an OTLP
exporter of your own, or set `OTEL_EXPORTER_OTLP_ENDPOINT` directly to the
backend's OTLP endpoint.

## Spans

| Span | When | Attributes |
| --- | --- | --- |
| `readproof.resolve` | Every resolve (`readproof get`, a run mount, the API). Root of the resolve tree. | `readproof.resource.uri`, `readproof.resource.ref` (tagged resolves only) at start; on success `readproof.snapshot.id`, `readproof.snapshot.content_hash`, `readproof.snapshot.source_revision`, `readproof.snapshot.observed_at`, `readproof.materialization.id`, `readproof.materialization.bytes`, `readproof.source.type`, `readproof.policy.strategy`, `readproof.policy.decision`, `readproof.freshness.status`, `gen_ai.data_source.id` |
| `readproof.resource.lookup` | Loading the resource definition. | — |
| `readproof.policy.evaluate` | Freshness evaluation. **Absent for tag resolves** — a tag names one exact snapshot, so policy is never consulted. | `readproof.policy.strategy`, `readproof.freshness.status` |
| `readproof.cache.lookup` | Serving stored bytes (pinned, current, or tagged snapshot). | `readproof.cache.hit` (always `true`: the span is only started on a hit; a miss appears as a `readproof.source.fetch`) |
| `readproof.source.fetch` | Contacting the source. Only on `decision=fetch`. | `readproof.source.type` |
| `readproof.snapshot.create` | Storing the blob and the new snapshot row. | `readproof.snapshot.id` |
| `readproof.materialize` | Get-or-create of the raw materialization. | `readproof.materialization.cached`; `readproof.materialization.id` only when a new one is created (on a cache hit the id is on `readproof.resolve`) |
| `readproof.tag.lookup` | Resolving `@<tag>` to a snapshot id. | `readproof.resource.uri`, `readproof.resource.ref` |
| `readproof.manifest.append` | Staging a resolved entry into an open run. | `readproof.resource.uri`, `readproof.snapshot.id`, `readproof.manifest.position` |
| `readproof.run.start` | `run.Builder.Start`. | `readproof.run.id` |
| `readproof.run.mount` | `run.Builder.Mount`. Parents that mount's `readproof.resolve` and `readproof.manifest.append`. | `readproof.run.id`, `readproof.resource.uri`, `readproof.resource.ref` (tagged mounts only), `readproof.manifest.position` |
| `readproof.run.commit` | `run.Builder.Commit`. | `readproof.run.id`, `readproof.manifest.id`, `readproof.manifest.entries`, `readproof.manifest.merkle_root` |

Every span records failures the same way: `RecordError` (an `exception`
event) plus status `Error`. A failed `readproof.resolve` sets none of the
result attributes — a span never claims a snapshot it did not produce.

## Attributes

| Attribute | Type | Meaning |
| --- | --- | --- |
| `readproof.resource.uri` | string | The bare `readproof://<namespace>/<path>`, never the combined `uri@ref` form. |
| `readproof.resource.ref` | string | The tag the reference was pinned to. Set only when one was given, so its presence alone distinguishes a tagged resolve. |
| `readproof.snapshot.id` | string | Identity of the observation that was served. |
| `readproof.snapshot.content_hash` | string | `sha256:<hex>` of the resolved bytes — the join key between a trace, a manifest entry, and an evidence bundle. |
| `readproof.snapshot.source_revision` | string | Whatever the source called its version (a git sha, an ETag, a content digest for filesystem sources). |
| `readproof.snapshot.observed_at` | string | RFC 3339 UTC. When Readproof *observed* the content, which is not when the span ran — a cached or tagged resolve can serve bytes observed weeks earlier, and that gap is the point. |
| `readproof.materialization.id` | string | Identity of the exact byte form handed to the caller. |
| `readproof.materialization.bytes` | int64 | Size of that byte form. A count, not the content. |
| `readproof.source.type` | string | `filesystem`, `github`, `http`. On `readproof.resolve` this is the resource's configured source even when nothing was fetched. |
| `readproof.policy.strategy` | string | `require_fresh`, `allow_stale`, `pinned` — what the resource *declares*. On a tag resolve it was not consulted; `readproof.policy.decision=use_tag` says so. |
| `readproof.policy.decision` | string | `fetch`, `use_current`, `use_pinned`, `use_tag`. **Canonical name going forward.** |
| `readproof.freshness.status` | string | Identical value to `readproof.policy.decision`, kept because v0.1 documented it and dashboards may query it. It will not change meaning; prefer `readproof.policy.decision` in new queries. |
| `readproof.cache.hit` | bool | Bytes came from storage rather than the source. |
| `readproof.materialization.cached` | bool | An existing materialization was reused rather than recomputed. |
| `readproof.run.id` | string | Caller-supplied run id. The only handle that joins `readproof.run.start` / `readproof.run.mount` / `readproof.run.commit` when they arrive in separate traces (see below). |
| `readproof.manifest.id` | string | The committed manifest. What `readproof replay`, `readproof diff`, and `readproof evidence export` take as their argument. |
| `readproof.manifest.position` | int | 0-based mount order. Order is a hard Readproof invariant — it changes effective model input — so it is committed to, not sorted away. |
| `readproof.manifest.entries` | int | Number of entries in the committed manifest. |
| `readproof.manifest.merkle_root` | string | Hex Merkle root over the committed entries; see [Correlating a trace with an evidence bundle](#correlating-a-trace-with-an-evidence-bundle). |
| `gen_ai.data_source.id` | string | OTel GenAI semantic convention. Readproof emits `readproof://<namespace>`; see [GenAI mapping](#genai-mapping). |

## Metrics

All counters are monotonic `Int64Counter`s; durations are `Float64Histogram`s
in seconds. Labels are deliberately low-cardinality — run ids, tag names,
snapshot ids and content hashes stay on spans, where cardinality is not a
cost.

| Metric | Kind | Labels | Meaning |
| --- | --- | --- | --- |
| `readproof_resolve_total` | counter | `readproof.resource.uri` | Resolve calls. |
| `readproof_resolve_duration_seconds` | histogram | `readproof.resource.uri` | End-to-end resolve latency. |
| `readproof_resolve_errors_total` | counter | `readproof.resource.uri` | Resolves that returned an error. |
| `readproof_cache_hit_total` | counter | — | Resolves served from a stored snapshot. |
| `readproof_cache_miss_total` | counter | — | Resolves that required a source fetch. |
| `readproof_source_fetch_total` | counter | `readproof.source.type` | Source fetch attempts. |
| `readproof_source_fetch_duration_seconds` | histogram | `readproof.source.type` | Source fetch latency. |
| `readproof_source_fetch_errors_total` | counter | `readproof.source.type` | Failed source fetches. |
| `readproof_snapshot_created_total` | counter | — | Snapshot rows created. |
| `readproof_materialization_created_total` | counter | — | Materialization rows created. |
| `readproof_manifest_created_total` | counter | — | Manifests created. |
| `readproof_run_committed_total` | counter | — | Runs that reached a committed manifest. |
| `readproof_tag_resolve_total` | counter | — | Resolves served from a tag ref. |

`readproof_manifest_created_total` and `readproof_run_committed_total`
move together in normal operation; a gap between them means manifests were
created by a path that never marked its run committed, which is worth an
alert.

## A worked trace

```bash
readproof run --id r readproof://demo/policies/refunds@prod
```

```
readproof.run.start
  readproof.run.id=r

readproof.run.mount
  readproof.run.id=r  readproof.resource.uri=readproof://demo/policies/refunds
  readproof.resource.ref=prod  readproof.manifest.position=0
  ├── readproof.resolve
  │     readproof.resource.uri=readproof://demo/policies/refunds  readproof.resource.ref=prod
  │     readproof.policy.decision=use_tag   readproof.freshness.status=use_tag
  │     readproof.policy.strategy=require_fresh          (declared, not consulted)
  │     readproof.source.type=filesystem
  │     readproof.snapshot.id=snap_01J...  readproof.snapshot.content_hash=sha256:9f2c…
  │     readproof.snapshot.source_revision=sha256:4b1e0a7c9d33
  │     readproof.snapshot.observed_at=2026-08-14T09:12:44Z
  │     readproof.materialization.id=mat_01J...  readproof.materialization.bytes=41
  │     gen_ai.data_source.id=readproof://demo
  │     ├── readproof.resource.lookup
  │     ├── readproof.tag.lookup       readproof.resource.uri=…  readproof.resource.ref=prod
  │     ├── readproof.cache.lookup     readproof.cache.hit=true
  │     └── readproof.materialize      readproof.materialization.cached=true
  └── readproof.manifest.append
        readproof.resource.uri=…  readproof.snapshot.id=snap_01J…  readproof.manifest.position=0

readproof.run.commit
  readproof.run.id=r  readproof.manifest.id=manifest_01J...
  readproof.manifest.entries=1
  readproof.manifest.merkle_root=518f2505…a92c
```

Read across it: the run mounted one resource, by the `prod` tag; policy
was bypassed (`use_tag`) so the source was never contacted (no
`readproof.source.fetch`); the bytes it got were observed on 14 August
even though the run happened later; and the committed manifest digests to
`518f2505…a92c`.

The same run without the tag replaces the tag/cache pair with the fetch
path — `readproof.policy.evaluate` (`readproof.freshness.status=fetch`) →
`readproof.source.fetch` → `readproof.snapshot.create` →
`readproof.materialize` — and `readproof.policy.decision` on
`readproof.resolve` becomes `fetch`.

**`readproof.run.start`, `readproof.run.mount` and `readproof.run.commit`
are three separate traces here**, joined by `readproof.run.id`. That is
not an accident of the CLI: a run legitimately spans processes and
machines (start it in one step, mount from a worker, commit hours later),
so no ambient parent span exists to hang them off. Callers that *do* have
one — the embedded API, an SDK inside an agent framework — pass a
`context.Context` carrying their span and get the whole run nested under
it. Query by `readproof.run.id` rather than assuming one trace per run.

In client/server mode the `readproof.run.*` and `readproof.resolve` spans
are emitted by `readproofd` (`service.name=readproofd`), not by the CLI:
the daemon does the work. Trace context is **not** propagated over the
HTTP API yet, so a CLI span and the `readproofd` spans it caused are
correlated by `readproof.run.id` and `readproof.resource.uri`, not by
trace id.

## Correlating a trace with an evidence bundle

`readproof.manifest.merkle_root` on `readproof.run.commit` is
byte-identical to the `subject[0].digest.sha256` of the [evidence
bundle](evidence.md) exported for that manifest — both come from
[`internal/merkle`](../internal/merkle), which exists precisely so there
is one implementation of the rule. So:

- given a trace, you can tell whether a bundle you were handed describes
  that exact run;
- given a bundle, you can find the run that produced it without trusting
  any id an operator typed.

The leaf is
`sha256(position_be_uint32 || 0x00 || uri || 0x00 || content_hash)`, and the
root is a standard binary tree over the leaves in position order.
`internal/evidence/merkle_test.go` holds fixed vectors that pin the output;
they are the contract, not the implementation's opinion.

## GenAI mapping

OpenTelemetry's GenAI semantic conventions describe retrieval steps in an
agent, but they identify a *data source*, not a *version of a document*.
Readproof emits the one attribute that maps cleanly today:

| GenAI attribute | Readproof value | Why |
| --- | --- | --- |
| `gen_ai.data_source.id` | `readproof://<namespace>` | The namespace is the coarsest stable identifier of the corpus an agent read from — the analogue of a vector-store collection id. It is stable across snapshots, so grouping by it in a GenAI-aware backend puts Readproof retrievals next to vector-store ones. |

Per-document identity deliberately stays on the `readproof.*` attributes
(`readproof.resource.uri`, `readproof.snapshot.content_hash`,
`readproof.snapshot.source_revision`, `readproof.snapshot.observed_at`):
those are per-resolve values, and stuffing them into a data-source-level
attribute would make them mean something they do not.

### Proposal: carry content identity on retrieved documents

The gap this leaves is real and not Readproof-specific. Today a GenAI
trace can say *which store* a document came from and *what it said*, but
not *which version of it* — so an incident review can see the text the
model got and still not prove it was the text the source held at the time,
and two runs that read the same corpus at different revisions look
identical in the trace.

Readproof already computes exactly the missing fields. We propose they
become conventional per-document attributes:

| Proposed attribute | Readproof source | Example |
| --- | --- | --- |
| `gen_ai.retrieval.documents.<i>.id` | `readproof.resource.uri` | `readproof://demo/policies/refunds` |
| `gen_ai.retrieval.documents.<i>.content_hash` | `readproof.snapshot.content_hash` | `sha256:9f2c…` |
| `gen_ai.retrieval.documents.<i>.source_revision` | `readproof.snapshot.source_revision` | `sha256:4b1e0a7c9d33` |
| `gen_ai.retrieval.documents.<i>.observed_at` | `readproof.snapshot.observed_at` | `2026-08-14T09:12:44Z` |

The same four fields fit
[OpenInference](https://github.com/Arize-ai/openinference)'s existing
`document.metadata` on retrieval spans, with no spec change needed:

```jsonc
// retrieval.documents.0.document.metadata
{
  "uri":             "readproof://demo/policies/refunds",
  "content_hash":    "sha256:9f2c…",
  "source_revision": "sha256:4b1e0a7c9d33",
  "observed_at":     "2026-08-14T09:12:44Z",
  "snapshot_id":     "snap_01J…",
  "manifest_id":     "manifest_01J…"
}
```

Three properties make this cheap to adopt: the values are short, bounded
strings; none of them is content, so the privacy posture of a trace does
not change; and a backend that ignores them is no worse off than today.

Until something like the above is conventional, the mapping a backend can
apply right now — using only what Readproof emits and what each product
already indexes — is:

| Backend | Where the Readproof attributes land | What it buys |
| --- | --- | --- |
| **Arize Phoenix** (OpenInference) | Copy `readproof.snapshot.content_hash` / `readproof.snapshot.source_revision` into `document.metadata` on the retrieval span; keep `gen_ai.data_source.id` as-is. | Group evaluation runs by the exact document version; diff two evals that disagree. |
| **Langfuse** | `readproof.run.id` → trace/session id; `readproof.manifest.id`, `readproof.manifest.merkle_root` → trace metadata; `readproof.snapshot.content_hash` → observation metadata. | A Langfuse trace links to the manifest that reproduces it byte-for-byte. |
| **LangSmith** | `readproof.manifest.id` and `readproof.snapshot.content_hash` as run metadata / tags on the retriever run. | Filter datasets to runs that saw one specific policy version. |
| **Datadog LLM Observability** | Span tags pass through unchanged; facet `readproof.policy.decision`, `readproof.source.type`, `readproof.freshness.status`. | Alert on a spike in `use_current` (staleness) or `fetch` (cache thrash) per resource. |
| **Honeycomb** | Attributes are already columns; no mapping needed. | `HEATMAP(readproof.materialization.bytes)` by `readproof.resource.uri`, or `COUNT` grouped by `readproof.policy.decision`. |

Anything that stores OTLP attributes verbatim needs no adapter at all: the
`readproof.*` names are stable, and the two that could have been ambiguous
(`readproof.policy.decision` vs. `readproof.freshness.status`) are
documented above as the same value under two names, with the canonical one
named.
