// Resolves a resource against a running ctxd and prints content, snapshot
// id, and content hash — run against the repo's docker-compose stack:
//
//   docker compose up -d --build
//   npm --prefix sdk/typescript run example
//
// Override the target with CTX_ENDPOINT / CTX_DEMO_URI env vars.

import { Ctx } from "../src/index.js";

async function main(): Promise<void> {
  const endpoint = process.env.CTX_ENDPOINT ?? "http://localhost:8080";
  const uri = process.env.CTX_DEMO_URI ?? "ctx://demo/policies/refunds-ts-sdk";
  const sourceUrl =
    process.env.CTX_DEMO_SOURCE_URL ??
    "https://raw.githubusercontent.com/octocat/Hello-World/master/README";

  const ctx = new Ctx({ endpoint });

  try {
    await ctx.getResource(uri);
  } catch {
    console.log(`Registering ${uri} (source: ${sourceUrl})...`);
    await ctx.registerResource({
      uri,
      source: { kind: "http", http: { url: sourceUrl } },
      policy: { strategy: "require_fresh" },
    });
  }

  const result = await ctx.resolve(uri);

  console.log(`uri:          ${uri}`);
  console.log(`snapshot:     ${result.snapshot.id}`);
  console.log(`content_hash: ${result.snapshot.content_hash}`);
  console.log(`freshness:    ${result.freshness.status}`);
  console.log(`bytes:        ${result.snapshot.bytes}`);
  console.log();
  console.log("--- content ---");
  console.log(result.content);

  // Manifest-aware resolve: mounting within a run records an ordered
  // manifest entry, the same way `ctx run --id <id> <uri>` does on the CLI.
  const run = ctx.run({ id: `ts-sdk-example-${Date.now()}` });
  await run.mount(uri);
  const manifest = await run.commit();
  console.log();
  console.log(`Committed manifest ${manifest.manifest_id} (run ${manifest.run_id}), ${manifest.entries.length} entr${manifest.entries.length === 1 ? "y" : "ies"}`);
}

main().catch((err: unknown) => {
  console.error(err);
  process.exitCode = 1;
});
