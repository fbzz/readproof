# Roadmap

What comes after the v0.2 MVP ([`mvp.md`](mvp.md)), roughly in priority
order. Each line is a real gap, not a wish — most are things the current
code notes as a limitation. Nothing here is a commitment or a date.

1. **Rename, LICENSE, public repo.** "Ctx" is a placeholder; the evidence
   `predicateType` URN and the `ctx://` scheme both bake it in, so the
   rename has to land before anyone depends on either. LICENSE is an owner
   decision that blocks publishing at all.
2. **Python SDK.** Most agent code that would mount `ctx://` URIs is
   Python; a TypeScript-only SDK excludes the majority of the users this is
   for.
3. **Trace-context propagation across the HTTP API.** Today a CLI/SDK span
   and the `ctxd` spans it caused are correlated by `ctx.run.id` and
   `ctx.resource.uri`, not by trace id — a documented gap in
   [`observability.md`](observability.md). W3C `traceparent` on the wire
   closes it and makes one run one trace.
4. **MCP over HTTP.** `ctx mcp` is stdio-only, which means one Ctx per
   client machine. A streamable-HTTP transport lets a team share one
   `ctxd`-backed MCP endpoint.
5. **Policy file.** A declarative allow-list of sources (with an SSRF
   allow-list for HTTP targets) plus integrity and prompt-injection
   scanning of fetched content. Registration is currently trusted
   implicitly, which stops being acceptable the moment `ctxd` accepts
   resources from anyone but its operator.
6. **Signed evidence bundles and OCI export.** A bundle is a valid in-toto
   Statement but is unsigned, so it proves consistency and not authorship;
   cosign / in-toto attestation fixes that, and pushing bundles to a
   registry with ORAS puts them where supply-chain tooling already looks.
7. **Tag promotion workflow.** `ctx tag promote staging→prod` as one
   audited step, instead of reading a snapshot id out of one command and
   pasting it into another — the error-prone part of using tags today.
8. **More source adapters.** S3, Confluence/Notion, and a generic git
   adapter (not just the GitHub API) cover where policy documents actually
   live in the companies most likely to need a manifest.
9. **Durable-execution helpers.** Thin Temporal / Restate activity wrappers
   for `run.start` / `mount` / `commit`, because a run legitimately spans
   processes and retries and those frameworks are where that already
   happens.
10. **Auth beyond a single API key.** One shared bearer token has no
    identity, no scoping, and no rotation story; per-caller keys or OIDC
    are the minimum for a shared `ctxd`.
11. **Operator UI.** A read-only web view of resources, tags, runs, diffs
    and bundles — the fastest way to answer "what did the agent see?"
    for someone who will never run the CLI.
