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
  process environment at fetch time and never stored — and on a server, only
  for variables named by `--header-env-allow`. Raw header values are masked
  by `internal/redact` in every API response, in `readproof inspect`, and in
  evidence bundles — including in embedded mode.
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
  allow-list, a 64 MiB response cap and a 30s timeout. On `readproofd` it
  additionally refuses targets that resolve to loopback, link-local
  (including `169.254.169.254`), private, CGNAT, unique-local, multicast or
  unspecified addresses. The check runs at **dial** time, on the address the
  resolver actually returned — so DNS rebinding does not get past it — and
  again on every redirect hop, with the chain capped at 5.
  `--allow-private-sources` (`READPROOFD_ALLOW_PRIVATE_SOURCES=1`) turns it
  off for a trusted network. The embedded CLI leaves private targets
  reachable, since developing against `localhost` is the ordinary case.
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

- **Filesystem sources are refused unless a root is allow-listed.**
  `readproofd --filesystem-root <dir>` (repeatable; env
  `READPROOFD_FILESYSTEM_ROOTS`, `,`- or path-separator-separated) is the
  only way a filesystem source resolves on the server. Reads are confined
  to files inside a root, with symlinks resolved *before* the containment
  check, so a link inside a root cannot serve a file outside it. With no
  root configured — the default — every filesystem source is refused, at
  registration (400) and at fetch. The embedded `readproof` CLI is
  deliberately unrestricted: it reads the operator's own files as the
  operator, and an allow-list there would restrict a user's access to their
  own documents and protect nobody. With `--server`, the server's policy is
  what applies.
- **`${VAR}` headers expand only what you allow-list.** A `"${VAR}"` header
  value is resolved from `readproofd`'s own environment and sent to whatever
  URL that resource names, so `readproofd` expands **nothing** by default:
  `--header-env-allow NAME` (repeatable; env
  `READPROOFD_HEADER_ENV_ALLOWLIST`, comma-separated) names the variables a
  source header may reference. Anything else is refused at registration
  (400, naming the variable and the flag) and at fetch. `readproofd`'s *own*
  credentials (`READPROOFD_API_KEY`, `READPROOF_API_KEY`,
  `READPROOFD_POSTGRES_DSN`, `READPROOFD_S3_ACCESS_KEY`,
  `READPROOFD_S3_SECRET_KEY`) stay refused even if allow-listed.
  `READPROOF_HTTP_HEADER_ENV_ALLOWLIST` is a second, process-level
  allow-list that narrows every Readproof process, the embedded CLI
  included — the CLI is otherwise permissive, because that environment
  belongs to the person typing the command.
- **Private network targets are refused unless you opt in.** See the SSRF
  entry above; `--allow-private-sources` is the opt-in, and
  `docker-compose.yml` sets it because that stack fetches from the host via
  `host.docker.internal`.

Operational gaps, all tracked in the report:

- **No TLS and no rate limiting.** `readproofd` speaks plaintext HTTP, so
  run it behind a TLS-terminating reverse proxy — otherwise the bearer key
  crosses the network in the clear.
- **`--api-key` on the command line is visible in process listings.**
  Prefer `READPROOFD_API_KEY` / `READPROOF_API_KEY`.
- **Bundles are unsigned.** An `--offline` verify cannot detect a forgery
  whose Merkle root was recomputed; signing is on the roadmap.

## Supported versions

Only the latest tagged release receives fixes.
