# Security

## Reporting a vulnerability

Please report security issues privately through GitHub's
[private vulnerability reporting](https://github.com/fbzz/readproof/security/advisories/new)
for this repository — not as a public issue. You will get an acknowledgement
within a few days and a fix or mitigation plan before any public disclosure.

## Scope and current baseline

Readproof 0.3.x is a security **baseline**, not enterprise IAM:

- **No plaintext credentials at rest.** `GITHUB_TOKEN` and HTTP source
  headers of the form `"${VAR_NAME}"` are resolved from the `readproofd`
  process environment at fetch time and never stored. Raw header values are
  masked by `internal/redact` in every API response, in `readproof inspect`,
  and in evidence bundles — including in embedded mode.
- **Optional API auth.** `readproofd --api-key` (`READPROOFD_API_KEY`)
  requires `Authorization: Bearer <key>` on every request except `/healthz`.
  Off by default.
- **Dev-only credentials are labeled.** `docker-compose.yml` ships
  placeholder Postgres/MinIO credentials for local development; override via
  `.env` (`.env.example` documents every variable; `.env` is gitignored).
- **Dependency scanning.** `govulncheck ./...` and `npm audit` are run; one
  transitive advisory ([GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932),
  `golang.org/x/crypto/openpgp`) is not imported by any package this module
  builds and is accepted.
- **SSRF.** The HTTP source adapter enforces an http/https scheme
  allow-list, a 64 MiB response cap and a 30s timeout, but has no target-IP
  restrictions. That is acceptable while resources are registered only by
  the operator running `readproof`/`readproofd`; an allow-list is on the
  roadmap before `readproofd` accepts registrations from less-trusted
  callers.
- **Evidence bundles are not signed yet.** `readproof evidence verify` proves
  integrity against the Merkle root and, unless `--offline`, the store;
  signing (cosign / in-toto attestation) is on the roadmap. An `--offline`
  verify cannot detect a forgery whose root was recomputed — see
  [`docs/evidence.md`](docs/evidence.md).

## Known limitations

From the August 2026 pre-launch audit
([`docs/security-audit-2026-08.md`](docs/security-audit-2026-08.md)), which
lists every finding, its status, and a concrete fix design.

**Registering a resource is a privileged action.** Treat it as equivalent
to shell access on the `readproofd` host, and never expose `readproofd`
without `--api-key` plus a network boundary:

- **Filesystem sources read any file the server can read.** There is no
  allow-list root. A resource registered with
  `--path /etc/passwd` — or pointed at the SQLite database, or a `.env`
  beside the binary — resolves normally and returns the bytes.
- **HTTP source headers can read the server's environment.** A `"${VAR}"`
  header value is resolved from `readproofd`'s environment and sent to
  whatever URL that resource names. `readproofd`'s *own* credentials
  (`READPROOFD_API_KEY`, `READPROOF_API_KEY`, `READPROOFD_POSTGRES_DSN`,
  `READPROOFD_S3_ACCESS_KEY`, `READPROOFD_S3_SECRET_KEY`) are refused, and
  setting `READPROOF_HTTP_HEADER_ENV_ALLOWLIST` to a comma-separated list
  of variable names restricts expansion to exactly those — **recommended
  for any deployment whose registrations are not fully trusted.** Every
  other variable is readable by default.
- **No SSRF address restriction.** A resource can reach loopback,
  link-local and cloud metadata addresses, and redirects into them are
  followed.

Operational gaps, all tracked in the report:

- **No TLS and no rate limiting.** `readproofd` speaks plaintext HTTP, so
  run it behind a TLS-terminating reverse proxy — otherwise the bearer key
  crosses the network in the clear.
- **`--api-key` on the command line is visible in process listings.**
  Prefer `READPROOFD_API_KEY` / `READPROOF_API_KEY`.
- **500 responses include internal error text**, which can name absolute
  paths on the server.
- **Data directory and blobs are created world-readable** (`0755`/`0644`);
  restrict them yourself on a shared host.
- **The container image runs as root.**

## Supported versions

Only the latest tagged release receives fixes.
