# Observability

Ctx is instrumented with [OpenTelemetry](https://opentelemetry.io) so that
the question an agent trace usually cannot answer — **which bytes did the
model actually get, and where did they come from?** — is answerable from
the trace alone, without opening the store.

Two rules shape everything below:

1. **Spans carry the identity of content, never the content.** Hashes,
   snapshot ids, source revisions, byte counts, observation times: yes.
   Resolved bytes: never, in any attribute, event, or status. Traces get
   copied into third-party backends; the bytes stay in Ctx.
   ([`internal/run/run_telemetry_test.go`](../internal/run/run_telemetry_test.go)
   and
   [`internal/resolver/resolver_telemetry_test.go`](../internal/resolver/resolver_telemetry_test.go)
   scan every recorded attribute and event for the fixture's bytes and fail
   if they appear.)
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
ctx run --id r ctx://demo/policies/refunds@prod
```

`service.name` is `ctx` for the CLI and `ctxd` for the daemon.

`docker compose up -d` already runs a collector (`otel-collector`, OTLP on
`4317`/`4318`) and points `ctxd` at it. Its `debug` exporter prints
everything it receives, which is enough to eyeball a trace:

```bash
docker compose up -d
docker compose logs -f otel-collector          # ctxd's spans and metrics
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 \
  ctx run --id r ctx://demo/policies/refunds   # the CLI's, via the mapped port
```

To send to a real backend instead, point
[`otel-collector-config.yaml`](../otel-collector-config.yaml) at an OTLP
exporter of your own, or set `OTEL_EXPORTER_OTLP_ENDPOINT` directly to the
backend's OTLP endpoint.

## Spans

| Span | When | Attributes |
| --- | --- | --- |
| `ctx.resolve` | Every resolve (`ctx get`, a run mount, the API). Root of the resolve tree. | `ctx.resource.uri`, `ctx.resource.ref` (tagged resolves only) at start; on success `ctx.snapshot.id`, `ctx.snapshot.content_hash`, `ctx.snapshot.source_revision`, `ctx.snapshot.observed_at`, `ctx.materialization.id`, `ctx.materialization.bytes`, `ctx.source.type`, `ctx.policy.strategy`, `ctx.policy.decision`, `ctx.freshness.status`, `gen_ai.data_source.id` |
| `ctx.resource.lookup` | Loading the resource definition. | — |
| `ctx.policy.evaluate` | Freshness evaluation. **Absent for tag resolves** — a tag names one exact snapshot, so policy is never consulted. | `ctx.policy.strategy`, `ctx.freshness.status` |
| `ctx.cache.lookup` | Serving stored bytes (pinned, current, or tagged snapshot). | `ctx.cache.hit` (always `true`: the span is only started on a hit; a miss appears as a `ctx.source.fetch`) |
| `ctx.source.fetch` | Contacting the source. Only on `decision=fetch`. | `ctx.source.type` |
| `ctx.snapshot.create` | Storing the blob and the new snapshot row. | `ctx.snapshot.id` |
| `ctx.materialize` | Get-or-create of the raw materialization. | `ctx.materialization.cached`; `ctx.materialization.id` only when a new one is created (on a cache hit the id is on `ctx.resolve`) |
| `ctx.tag.lookup` | Resolving `@<tag>` to a snapshot id. | `ctx.resource.uri`, `ctx.resource.ref` |
| `ctx.manifest.append` | Staging a resolved entry into an open run. | `ctx.resource.uri`, `ctx.snapshot.id`, `ctx.manifest.position` |
| `ctx.run.start` | `run.Builder.Start`. | `ctx.run.id` |
| `ctx.run.mount` | `run.Builder.Mount`. Parents that mount's `ctx.resolve` and `ctx.manifest.append`. | `ctx.run.id`, `ctx.resource.uri`, `ctx.resource.ref` (tagged mounts only), `ctx.manifest.position` |
| `ctx.run.commit` | `run.Builder.Commit`. | `ctx.run.id`, `ctx.manifest.id`, `ctx.manifest.entries`, `ctx.manifest.merkle_root` |

Every span records failures the same way: `RecordError` (an `exception`
event) plus status `Error`. A failed `ctx.resolve` sets none of the result
attributes — a span never claims a snapshot it did not produce.

## Attributes

| Attribute | Type | Meaning |
| --- | --- | --- |
| `ctx.resource.uri` | string | The bare `ctx://<namespace>/<path>`, never the combined `uri@ref` form. |
| `ctx.resource.ref` | string | The tag the reference was pinned to. Set only when one was given, so its presence alone distinguishes a tagged resolve. |
| `ctx.snapshot.id` | string | Identity of the observation that was served. |
| `ctx.snapshot.content_hash` | string | `sha256:<hex>` of the resolved bytes — the join key between a trace, a manifest entry, and an evidence bundle. |
| `ctx.snapshot.source_revision` | string | Whatever the source called its version (a git sha, an ETag, a content digest for filesystem sources). |
| `ctx.snapshot.observed_at` | string | RFC 3339 UTC. When Ctx *observed* the content, which is not when the span ran — a cached or tagged resolve can serve bytes observed weeks earlier, and that gap is the point. |
| `ctx.materialization.id` | string | Identity of the exact byte form handed to the caller. |
| `ctx.materialization.bytes` | int64 | Size of that byte form. A count, not the content. |
| `ctx.source.type` | string | `filesystem`, `github`, `http`. On `ctx.resolve` this is the resource's configured source even when nothing was fetched. |
| `ctx.policy.strategy` | string | `require_fresh`, `allow_stale`, `pinned` — what the resource *declares*. On a tag resolve it was not consulted; `ctx.policy.decision=use_tag` says so. |
| `ctx.policy.decision` | string | `fetch`, `use_current`, `use_pinned`, `use_tag`. **Canonical name going forward.** |
| `ctx.freshness.status` | string | Identical value to `ctx.policy.decision`, kept because v0.1 documented it and dashboards may query it. It will not change meaning; prefer `ctx.policy.decision` in new queries. |
| `ctx.cache.hit` | bool | Bytes came from storage rather than the source. |
| `ctx.materialization.cached` | bool | An existing materialization was reused rather than recomputed. |
| `ctx.run.id` | string | Caller-supplied run id. The only handle that joins `ctx.run.start` / `ctx.run.mount` / `ctx.run.commit` when they arrive in separate traces (see below). |
| `ctx.manifest.id` | string | The committed manifest. What `ctx replay`, `ctx diff`, and `ctx evidence export` take as their argument. |
| `ctx.manifest.position` | int | 0-based mount order. Order is a hard Ctx invariant — it changes effective model input — so it is committed to, not sorted away. |
| `ctx.manifest.entries` | int | Number of entries in the committed manifest. |
| `ctx.manifest.merkle_root` | string | Hex Merkle root over the committed entries; see [Correlating a trace with an evidence bundle](#correlating-a-trace-with-an-evidence-bundle). |
| `gen_ai.data_source.id` | string | OTel GenAI semantic convention. Ctx emits `ctx://<namespace>`; see [GenAI mapping](#genai-mapping). |

## Metrics

All counters are monotonic `Int64Counter`s; durations are `Float64Histogram`s
in seconds. Labels are deliberately low-cardinality — run ids, tag names,
snapshot ids and content hashes stay on spans, where cardinality is not a
cost.

| Metric | Kind | Labels | Meaning |
| --- | --- | --- | --- |
| `ctx_resolve_total` | counter | `ctx.resource.uri` | Resolve calls. |
| `ctx_resolve_duration_seconds` | histogram | `ctx.resource.uri` | End-to-end resolve latency. |
| `ctx_resolve_errors_total` | counter | `ctx.resource.uri` | Resolves that returned an error. |
| `ctx_cache_hit_total` | counter | — | Resolves served from a stored snapshot. |
| `ctx_cache_miss_total` | counter | — | Resolves that required a source fetch. |
| `ctx_source_fetch_total` | counter | `ctx.source.type` | Source fetch attempts. |
| `ctx_source_fetch_duration_seconds` | histogram | `ctx.source.type` | Source fetch latency. |
| `ctx_source_fetch_errors_total` | counter | `ctx.source.type` | Failed source fetches. |
| `ctx_snapshot_created_total` | counter | — | Snapshot rows created. |
| `ctx_materialization_created_total` | counter | — | Materialization rows created. |
| `ctx_manifest_created_total` | counter | — | Manifests created. |
| `ctx_run_committed_total` | counter | — | Runs that reached a committed manifest. |
| `ctx_tag_resolve_total` | counter | — | Resolves served from a tag ref. |

`ctx_manifest_created_total` and `ctx_run_committed_total` move together in
normal operation; a gap between them means manifests were created by a path
that never marked its run committed, which is worth an alert.

## A worked trace

```bash
ctx run --id r ctx://demo/policies/refunds@prod
```

```
ctx.run.start
  ctx.run.id=r

ctx.run.mount
  ctx.run.id=r  ctx.resource.uri=ctx://demo/policies/refunds
  ctx.resource.ref=prod  ctx.manifest.position=0
  ├── ctx.resolve
  │     ctx.resource.uri=ctx://demo/policies/refunds  ctx.resource.ref=prod
  │     ctx.policy.decision=use_tag   ctx.freshness.status=use_tag
  │     ctx.policy.strategy=require_fresh          (declared, not consulted)
  │     ctx.source.type=filesystem
  │     ctx.snapshot.id=snap_01J...  ctx.snapshot.content_hash=sha256:9f2c…
  │     ctx.snapshot.source_revision=sha256:4b1e0a7c9d33
  │     ctx.snapshot.observed_at=2026-08-14T09:12:44Z
  │     ctx.materialization.id=mat_01J...  ctx.materialization.bytes=41
  │     gen_ai.data_source.id=ctx://demo
  │     ├── ctx.resource.lookup
  │     ├── ctx.tag.lookup       ctx.resource.uri=…  ctx.resource.ref=prod
  │     ├── ctx.cache.lookup     ctx.cache.hit=true
  │     └── ctx.materialize      ctx.materialization.cached=true
  └── ctx.manifest.append
        ctx.resource.uri=…  ctx.snapshot.id=snap_01J…  ctx.manifest.position=0

ctx.run.commit
  ctx.run.id=r  ctx.manifest.id=manifest_01J...
  ctx.manifest.entries=1
  ctx.manifest.merkle_root=518f2505…a92c
```

Read across it: the run mounted one resource, by the `prod` tag; policy was
bypassed (`use_tag`) so the source was never contacted (no
`ctx.source.fetch`); the bytes it got were observed on 14 August even
though the run happened later; and the committed manifest digests to
`518f2505…a92c`.

The same run without the tag replaces the tag/cache pair with the fetch
path — `ctx.policy.evaluate` (`ctx.freshness.status=fetch`) →
`ctx.source.fetch` → `ctx.snapshot.create` → `ctx.materialize` — and
`ctx.policy.decision` on `ctx.resolve` becomes `fetch`.

**`ctx.run.start`, `ctx.run.mount` and `ctx.run.commit` are three separate
traces here**, joined by `ctx.run.id`. That is not an accident of the CLI:
a run legitimately spans processes and machines (start it in one step,
mount from a worker, commit hours later), so no ambient parent span exists
to hang them off. Callers that *do* have one — the embedded API, an SDK
inside an agent framework — pass a `context.Context` carrying their span and
get the whole run nested under it. Query by `ctx.run.id` rather than
assuming one trace per run.

In client/server mode the `ctx.run.*` and `ctx.resolve` spans are emitted by
`ctxd` (`service.name=ctxd`), not by the CLI: the daemon does the work.
Trace context is **not** propagated over the HTTP API yet, so a CLI span and
the `ctxd` spans it caused are correlated by `ctx.run.id` and
`ctx.resource.uri`, not by trace id.

## Correlating a trace with an evidence bundle

`ctx.manifest.merkle_root` on `ctx.run.commit` is byte-identical to the
`subject[0].digest.sha256` of the [evidence bundle](evidence.md) exported
for that manifest — both come from
[`internal/merkle`](../internal/merkle), which exists precisely so there is
one implementation of the rule. So:

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
Ctx emits the one attribute that maps cleanly today:

| GenAI attribute | Ctx value | Why |
| --- | --- | --- |
| `gen_ai.data_source.id` | `ctx://<namespace>` | The namespace is the coarsest stable identifier of the corpus an agent read from — the analogue of a vector-store collection id. It is stable across snapshots, so grouping by it in a GenAI-aware backend puts Ctx retrievals next to vector-store ones. |

Per-document identity deliberately stays on the `ctx.*` attributes
(`ctx.resource.uri`, `ctx.snapshot.content_hash`,
`ctx.snapshot.source_revision`, `ctx.snapshot.observed_at`): those are
per-resolve values, and stuffing them into a data-source-level attribute
would make them mean something they do not.

### Proposal: carry content identity on retrieved documents

The gap this leaves is real and not Ctx-specific. Today a GenAI trace can
say *which store* a document came from and *what it said*, but not *which
version of it* — so an incident review can see the text the model got and
still not prove it was the text the source held at the time, and two runs
that read the same corpus at different revisions look identical in the
trace.

Ctx already computes exactly the missing fields. We propose they become
conventional per-document attributes:

| Proposed attribute | Ctx source | Example |
| --- | --- | --- |
| `gen_ai.retrieval.documents.<i>.id` | `ctx.resource.uri` | `ctx://demo/policies/refunds` |
| `gen_ai.retrieval.documents.<i>.content_hash` | `ctx.snapshot.content_hash` | `sha256:9f2c…` |
| `gen_ai.retrieval.documents.<i>.source_revision` | `ctx.snapshot.source_revision` | `sha256:4b1e0a7c9d33` |
| `gen_ai.retrieval.documents.<i>.observed_at` | `ctx.snapshot.observed_at` | `2026-08-14T09:12:44Z` |

The same four fields fit
[OpenInference](https://github.com/Arize-ai/openinference)'s existing
`document.metadata` on retrieval spans, with no spec change needed:

```jsonc
// retrieval.documents.0.document.metadata
{
  "uri":             "ctx://demo/policies/refunds",
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
apply right now — using only what Ctx emits and what each product already
indexes — is:

| Backend | Where the Ctx attributes land | What it buys |
| --- | --- | --- |
| **Arize Phoenix** (OpenInference) | Copy `ctx.snapshot.content_hash` / `ctx.snapshot.source_revision` into `document.metadata` on the retrieval span; keep `gen_ai.data_source.id` as-is. | Group evaluation runs by the exact document version; diff two evals that disagree. |
| **Langfuse** | `ctx.run.id` → trace/session id; `ctx.manifest.id`, `ctx.manifest.merkle_root` → trace metadata; `ctx.snapshot.content_hash` → observation metadata. | A Langfuse trace links to the manifest that reproduces it byte-for-byte. |
| **LangSmith** | `ctx.manifest.id` and `ctx.snapshot.content_hash` as run metadata / tags on the retriever run. | Filter datasets to runs that saw one specific policy version. |
| **Datadog LLM Observability** | Span tags pass through unchanged; facet `ctx.policy.decision`, `ctx.source.type`, `ctx.freshness.status`. | Alert on a spike in `use_current` (staleness) or `fetch` (cache thrash) per resource. |
| **Honeycomb** | Attributes are already columns; no mapping needed. | `HEATMAP(ctx.materialization.bytes)` by `ctx.resource.uri`, or `COUNT` grouped by `ctx.policy.decision`. |

Anything that stores OTLP attributes verbatim needs no adapter at all: the
`ctx.*` names are stable, and the two that could have been ambiguous
(`ctx.policy.decision` vs. `ctx.freshness.status`) are documented above as
the same value under two names, with the canonical one named.
