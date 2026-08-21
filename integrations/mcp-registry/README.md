# MCP registry entry

[`server.json`](./server.json) is Readproof's entry for the [official MCP
registry](https://registry.modelcontextprotocol.io) — the discovery index
agent hosts read to find servers. It describes `readproof mcp`, the stdio
MCP server documented in [`docs/mcp.md`](../../docs/mcp.md).

The registry stores **metadata only**, never artifacts. It points at
packages that already exist somewhere else.

Schema: `https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json`
(verified against that schema; bump the `$schema` URL when the registry
publishes a newer dated revision).

## Publishing

Two things must be true first:

1. **The repository is public.** `mcp-publisher login github` authorises a
   namespace by proving you control `github.com/fbzz`, and the registry
   fetches the repository to validate the entry.
2. **The version matches a real release.** `version` in `server.json` is
   the Readproof version being announced; keep it in step with
   `internal/version` and the tag, exactly as
   [`docs/releasing.md`](../../docs/releasing.md) describes.

Then, from this directory:

```bash
# 1. Install the publisher CLI
brew install mcp-publisher
#   …or grab the release binary:
#   curl -L "https://github.com/modelcontextprotocol/registry/releases/latest/download/mcp-publisher_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz" \
#     | tar xz mcp-publisher && sudo mv mcp-publisher /usr/local/bin/

# 2. Check the file before touching the network
mcp-publisher validate

# 3. Authenticate. Device flow; the GitHub account must own github.com/fbzz,
#    because the namespace io.github.fbzz/ is derived from it.
mcp-publisher login github

# 4. Publish
mcp-publisher publish

# 5. Confirm
curl "https://registry.modelcontextprotocol.io/v0.1/servers?search=io.github.fbzz/readproof"
```

`mcp-publisher publish` reads `server.json` from the working directory, so
run it here rather than from the repository root.

Re-publishing the same `version` is rejected; bump `version` and publish
again. [`.github/workflows`](../../.github/workflows) does **not** automate
this — it is a deliberate manual step, because a registry entry is a public
claim about a release that already exists.

## Why there is no `packages` entry yet — TODO

The registry only accepts packages from registries whose ownership it can
verify. As of the 2025-12-11 schema those are:

| `registryType` | Artifact |
| --- | --- |
| `npm` | npmjs.org |
| `pypi` | pypi.org |
| `nuget` | api.nuget.org |
| `cargo` | crates.io |
| `oci` | Docker Hub, GHCR, Quay, … |
| `mcpb` | an `.mcpb` bundle attached to a GitHub or GitLab release |

**A Go binary published as a GitHub release archive is none of them.** The
closest is `mcpb`, and Readproof does not satisfy it today on two counts:
the identifier URL must contain the string `mcp` (ours is
`readproof_<version>_<os>_<arch>.tar.gz`), and an `.mcpb` is a specific
bundle format with a manifest, not a tarball of binaries. `fileSha256` is
also mandatory and is per-platform, so the file could not be filled in until
after the release exists.

So the entry ships with the install paths recorded under `_meta`
(`io.modelcontextprotocol.registry/publisher-provided`) and no `packages`
array, which the schema permits. Users install `readproof` with `go
install`, Homebrew, or a release archive, then point their client at it —
[`docs/mcp.md`](../../docs/mcp.md) has the Claude Code / Claude Desktop /
Cursor configuration.

Closing this properly means picking one of:

- **an OCI image** containing `readproof`, pushed to GHCR from the release
  workflow — `registryType: oci`, verified by an image label. The most
  natural fit for a Go binary, and reuses the existing `Dockerfile`.
- **a real `.mcpb` bundle**, built and attached per release, with the
  server.json regenerated afterwards to carry each `fileSha256`. Needs a
  post-release step, since the hashes only exist once the artifacts do.
- **an npm launcher package** (e.g. `@readproof/mcp`) that resolves the
  right release binary at install time — `registryType: npm`, verified by
  the `mcpName` field in its `package.json`. Cheapest to publish, most
  moving parts at runtime.

Until then this entry is discoverable and documented, just not
one-click-installable.
