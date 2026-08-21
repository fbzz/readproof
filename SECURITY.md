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
- **SSRF.** The HTTP source adapter has no target-IP restrictions. That is
  acceptable while resources are registered only by the operator running
  `readproof`/`readproofd`; an allow-list is on the roadmap before
  `readproofd` accepts registrations from less-trusted callers.
- **Evidence bundles are not signed yet.** `readproof evidence verify` proves
  integrity against the Merkle root and, unless `--offline`, the store;
  signing (cosign / in-toto attestation) is on the roadmap.

## Supported versions

Only the latest tagged release receives fixes.
