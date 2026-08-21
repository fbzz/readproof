# `readproofd` HTTP API reference

All bodies are JSON. `Content` fields are base64-encoded (Go's
`encoding/json` does this automatically for `[]byte`); the CLI and
TypeScript SDK both decode it to text transparently. If `readproofd
--api-key` is set, every route below except `/healthz` requires
`Authorization: Bearer <key>`. Errors are `{"error": "<message>"}` with a
4xx/5xx status — 404 for "not found", 400 for a bad request (including a
source definition the server's policy refuses), 401 for a missing/wrong API
key, 409 for a conflict with existing state, 500 otherwise.

A 4xx carries the reason, because the caller needs it to fix the request. A
**500 carries only a request id** (`internal server error (request id
req_…)`): the detail — a host path, a driver message — is written to the
`readproofd` log under that same id, so an operator can join the two without
the server describing its insides to an unauthenticated peer.

Source: [`internal/wire/wire.go`](../internal/wire/wire.go) (types) and
[`internal/api/api.go`](../internal/api/api.go) (handlers).

## Deploying it

`readproofd` speaks plaintext HTTP and has no rate limiting of its own. The
supported deployment is **behind a reverse proxy** (nginx, Caddy, Traefik, an
ingress controller, a cloud load balancer) that terminates TLS and limits
request rates — otherwise the bearer key crosses the network in the clear and
the write endpoints have nothing but the key between them and a script.

Set the key through the environment, not the command line:
`READPROOFD_API_KEY` for the server, `READPROOF_API_KEY` for the CLI and the
SDK. A flag value is visible to every user on the host in `ps`, so both
binaries print a warning when the key arrives that way. Off by default means
unauthenticated: fine on a laptop, never on a reachable port.

## `GET /healthz`

No auth required, always. Returns `200 ok` (plain text) when the process
is up — doesn't check the storage backend.

## `POST /v1/resources`

Register a resource.

**Request**
```json
{
  "uri": "readproof://acme/policies/refunds",
  "source": {
    "kind": "github",
    "github": { "owner": "acme", "repo": "company-docs", "path": "policies/refunds.md", "ref": "main" }
  },
  "policy": { "strategy": "require_fresh" }
}
```
`source.kind` is `"filesystem"` (`source.filesystem.path`), `"github"`
(`source.github.{owner,repo,path,ref}`), or `"http"`
(`source.http.{url,headers}` — header values of the exact form
`"${VAR_NAME}"`, or containing it, are resolved from `readproofd`'s own
environment at fetch time; see the README's Security section).
`policy.strategy` is `"require_fresh"`, `"allow_stale"` (with optional
`max_age_seconds`), or `"pinned"` (with `pinned_snapshot_id`).

**Registering a resource is a privileged action, and `readproofd` refuses
by default what it cannot vouch for.** A resource definition tells the
server which file to read, which address to connect to, and which of its
own environment variables to send. All three default to deny, and each is
opened by one explicit flag:

| Source | Default on `readproofd` | Opt in with |
| --- | --- | --- |
| `filesystem` | refused — no path is readable | `--filesystem-root <dir>` (repeatable; env `READPROOFD_FILESYSTEM_ROOTS`, `,`- or path-separator-separated). Reads are confined to files inside a root, with symlinks resolved before the check |
| `http` header `"${VAR}"` | refused — no variable expands | `--header-env-allow <NAME>` (repeatable; env `READPROOFD_HEADER_ENV_ALLOWLIST`, comma-separated) |
| `http` private target | refused — loopback, link-local (incl. `169.254.169.254`), RFC1918, CGNAT, unique-local | `--allow-private-sources` (env `READPROOFD_ALLOW_PRIVATE_SOURCES=1`) |

A refused definition is a `400` naming the flag that would allow it, both
here and on `POST /v1/resolve` (the adapter, not this handler, is the
enforcement point — a row registered under a wider policy is still refused
at fetch time). The embedded `readproof` CLI has none of these
restrictions: it reads the operator's own files, with the operator's own
environment, as the operator.

**Response** `201` — the registered resource, with any sensitive HTTP
header values in `source.http.headers` replaced by `"[REDACTED]"`:
```json
{
  "uri": "readproof://acme/policies/refunds",
  "namespace": "acme",
  "path": "policies/refunds",
  "source": { "kind": "github", "github": { "owner": "acme", "repo": "company-docs", "path": "policies/refunds.md", "ref": "main" } },
  "policy": { "strategy": "require_fresh" },
  "created_at": "2026-08-19T16:05:30Z",
  "updated_at": "2026-08-19T16:05:30Z"
}
```

## `GET /v1/resources`

List every registered resource. **Response** `200`:
```json
{ "resources": [ /* ResourceWire, as above, one per registered resource */ ] }
```

## `GET /v1/resources/get?uri=<uri>`

One resource, same shape as the register response. `404` if unregistered.

## `GET /v1/resources/history?uri=<uri>`

Snapshot history, newest first. **Response** `200`:
```json
{
  "snapshots": [
    {
      "id": "snap_01M0DCAH6EBHMGYD09ATZ2GEF3",
      "resource_uri": "readproof://acme/policies/refunds",
      "source_revision": "8af92d1",
      "content_hash": "sha256:c8b0bb212e93151d720746e36ff3b7076727cb577614feafa0d61f168965aedb",
      "observed_at": "2026-08-19T16:05:30Z",
      "created_at": "2026-08-19T16:05:30Z",
      "content_type": "text/markdown",
      "bytes": 41,
      "provenance": { "source_type": "github", "owner": "acme", "repo": "company-docs", "path": "policies/refunds.md", "ref": "main" }
    }
  ]
}
```

## `GET /v1/snapshots?id=<snapshot-id>`

One snapshot (same shape as an entry of the history array above). `404`
if it doesn't exist.

## `PUT /v1/tags`

Point a tag at a snapshot — an upsert, so setting an existing tag moves it.
A tag is a named, movable pointer `(resource_uri, tag) -> snapshot_id`;
the snapshot must belong to that resource. Tag names must match
`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`.

**Request**
```json
{ "uri": "readproof://acme/policies/refunds", "tag": "prod", "snapshot_id": "snap_01M0DCAH6EBHMGYD09ATZ2GEF3" }
```

**Response** `200` — the stored tag:
```json
{
  "uri": "readproof://acme/policies/refunds",
  "tag": "prod",
  "snapshot_id": "snap_01M0DCAH6EBHMGYD09ATZ2GEF3",
  "updated_at": "2026-08-20T09:00:00Z"
}
```
`400` for a malformed tag name or a snapshot belonging to a different
resource; `404` if the snapshot doesn't exist.

## `GET /v1/tags?uri=<uri>`

A resource's tags, sorted by name. **Response** `200`:
```json
{ "tags": [ { "uri": "readproof://acme/policies/refunds", "tag": "prod", "snapshot_id": "snap_...", "updated_at": "2026-08-20T09:00:00Z" } ] }
```

## `DELETE /v1/tags?uri=<uri>&tag=<tag>`

Delete a tag; the snapshot it pointed at is untouched. **Response** `204`,
no body. `404` if the tag doesn't exist.

## `POST /v1/resolve`

Resolve a resource — the primary operation.

**Request**
```json
{ "uri": "readproof://acme/policies/refunds" }
```

`uri` may carry a trailing `@<tag>`
(`"readproof://acme/policies/refunds@prod"`), which resolves to exactly
that tagged snapshot: **no source fetch, and the resource's freshness
policy is not consulted**. The response then reports `freshness.status:
"use_tag"` and echoes the tag as `resource.ref`. An unknown tag is a `404`
naming both the URI and the tag.

**Response** `200`:
```json
{
  "resource": {
    "uri": "readproof://acme/policies/refunds",
    "policy": { "strategy": "require_fresh" }
  },
  "snapshot": { "...": "SnapshotWire, as in /v1/resources/history" },
  "materialization": {
    "id": "mat_01M0DCAH6E4ADEJFDKTFBZ5MS2",
    "snapshot_id": "snap_01M0DCAH6EBHMGYD09ATZ2GEF3",
    "strategy": "raw",
    "content_hash": "sha256:c8b0bb212e93151d720746e36ff3b7076727cb577614feafa0d61f168965aedb",
    "bytes": 41,
    "created_at": "2026-08-19T16:05:30Z"
  },
  "freshness": { "status": "fetch", "age_seconds": 0.02 },
  "content": "UHJvZHVjdHMgY2FuIGJlIHJlZnVuZGVkIHdpdGhpbiAzMCBkYXlzLgo="
}
```
`freshness.status` is `"fetch"` (freshly resolved from the source),
`"use_current"` (served from the cached snapshot), `"use_pinned"` (policy
`pinned`), or `"use_tag"` (resolved via `@<tag>`). `resource.ref` is
present only for `"use_tag"`.

## `POST /v1/runs`

Start a run. **Request** `{ "run_id": "run-a" }`. **Response** `204`, no
body.

## `POST /v1/runs/mount`

Resolve a URI and stage it as the next entry in a run. Start the run with
`/v1/runs` first (the CLI's `readproof run mount` and the TS SDK's
`Run.mount()` both do).

**Request** `{ "run_id": "run-a", "uri":
"readproof://acme/policies/refunds" }`

`uri` may carry a trailing `@<tag>`, exactly as in `/v1/resolve`; the
manifest entry then records the bare URI plus that tag as `ref`.

**Response** `200`:
```json
{
  "position": 0,
  "resolve": { "...": "ResolveResponse, as in /v1/resolve" }
}
```

## `POST /v1/runs/commit`

Commit every mounted resource into an immutable manifest, entry order
preserved. **Request** `{ "run_id": "run-a" }`. **Response** `200`:
```json
{
  "manifest_id": "manifest_01M0DCAH6FQG8CT2Y99S6SW40X",
  "run_id": "run-a",
  "created_at": "2026-08-19T16:05:30Z",
  "entries": [
    { "position": 0, "uri": "readproof://acme/policies/refunds", "ref": "prod", "snapshot_id": "snap_...", "materialization_id": "mat_...", "content_hash": "sha256:..." }
  ]
}
```
`uri` is always the bare `readproof://<namespace>/<path>`; `ref` is the
`@<tag>` that entry was mounted by and is omitted for a plain URI. `ref`
is informational — replay and diff key off `snapshot_id`/`content_hash`,
so moving a tag afterwards can never change what a committed manifest
replays.

A run has exactly one manifest, and only a run that `POST /v1/runs`
started can be committed: `404` if `run_id` was never started, `409` if it
was already committed (the message names the manifest it already has).
Committing a started run with no mounts is legitimate and yields a
manifest with an empty `entries` array — "this run read nothing" is a
claim only a real run gets to make.

## `GET /v1/manifests?target=<manifest-id-or-run-id>`

A manifest, looked up by manifest ID first, falling back to run ID. Same
shape as the commit response above. `404` if neither matches.

## `GET /v1/diff?a=<target-a>&b=<target-b>`

Diff two manifests (each identified by manifest ID or run ID).

**Response** `200`:
```json
{
  "manifest_a": { "...": "ManifestWire" },
  "manifest_b": { "...": "ManifestWire" },
  "entries": [
    {
      "uri": "readproof://acme/policies/refunds",
      "status": "changed",
      "snapshot_id_a": "snap_01M0DCAH6EBHMGYD09ATZ2GEF3",
      "snapshot_id_b": "snap_01M0DCARRQ91XA135AHG6W64H0",
      "source_revision_a": "8af92d1",
      "source_revision_b": "c31be07",
      "observed_at_a": "2026-08-19T16:05:30Z",
      "observed_at_b": "2026-08-20T09:00:00Z",
      "ref_b": "prod",
      "unified_diff": "--- a/readproof://acme/policies/refunds\n+++ b/readproof://acme/policies/refunds\n@@ -1,2 +1,2 @@\n-Products can be refunded within 30 days.\n+Products can be refunded within 14 days.\n \n"
    }
  ]
}
```
`status` is `"changed"`, `"added"`, `"removed"`, or `"unchanged"`;
`unified_diff` is only present for `"changed"`. The `*_a`/`*_b` fields
carry each side's provenance — the source's own revision marker, when
Readproof observed it, and the `@<tag>` it was mounted by — and are
present only for a side whose manifest actually contains that URI. This is
what `readproof diff` prints as its per-entry `why:` line.

## `GET /v1/replay?target=<manifest-id-or-run-id>`

Reconstruct a manifest's exact delivered content — never re-fetches from
the live source.

**Response** `200`:
```json
{
  "manifest": { "...": "ManifestWire" },
  "entries": [
    {
      "position": 0,
      "uri": "readproof://acme/policies/refunds",
      "materialization_id": "mat_01M0DCAH6E4ADEJFDKTFBZ5MS2",
      "recorded_hash": "sha256:c8b0bb212e93151d720746e36ff3b7076727cb577614feafa0d61f168965aedb",
      "replayed_hash": "sha256:c8b0bb212e93151d720746e36ff3b7076727cb577614feafa0d61f168965aedb",
      "content": "UHJvZHVjdHMgY2FuIGJlIHJlZnVuZGVkIHdpdGhpbiAzMCBkYXlzLgo=",
      "match": true
    }
  ]
}
```
`recorded_hash` (from the manifest entry) equals `replayed_hash`
(recomputed from the actually-retrieved bytes) iff `match` is `true` —
this is the product's core invariant:
`SHA256(original) == SHA256(replay)`.

Replay is strict everywhere it surfaces: a missing blob or materialization
is an error response here, and `readproof replay` exits non-zero on any
`"match": false` entry rather than reporting it and continuing.
