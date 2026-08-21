// Resolves a resource against a running readproofd and prints content, snapshot
// id, and content hash — run against the repo's docker-compose stack:
//
//   docker compose up -d --build
//   npm --prefix sdk/typescript run example
//
// Override the target with READPROOF_ENDPOINT / READPROOF_DEMO_URI env vars.

import { Readproof } from "../src/index.js";

async function main(): Promise<void> {
  const endpoint = process.env.READPROOF_ENDPOINT ?? "http://localhost:8080";
  const uri = process.env.READPROOF_DEMO_URI ?? "readproof://demo/policies/refunds-ts-sdk";
  const sourceUrl =
    process.env.READPROOF_DEMO_SOURCE_URL ??
    "https://raw.githubusercontent.com/octocat/Hello-World/master/README";

  const rp = new Readproof({ endpoint });

  try {
    await rp.getResource(uri);
  } catch {
    console.log(`Registering ${uri} (source: ${sourceUrl})...`);
    await rp.registerResource({
      uri,
      source: { kind: "http", http: { url: sourceUrl } },
      policy: { strategy: "require_fresh" },
    });
  }

  const result = await rp.resolve(uri);

  console.log(`uri:          ${uri}`);
  console.log(`snapshot:     ${result.snapshot.id}`);
  console.log(`content_hash: ${result.snapshot.content_hash}`);
  console.log(`freshness:    ${result.freshness.status}`);
  console.log(`bytes:        ${result.snapshot.bytes}`);
  console.log();
  console.log("--- content ---");
  console.log(result.content);

  // Manifest-aware resolve: mounting within a run records an ordered
  // manifest entry, the same way `readproof run --id <id> <uri>` does on the CLI.
  const run = rp.run({ id: `ts-sdk-example-${Date.now()}` });
  await run.mount(uri);
  const manifest = await run.commit();
  console.log();
  console.log(`Committed manifest ${manifest.manifest_id} (run ${manifest.run_id}), ${manifest.entries.length} entr${manifest.entries.length === 1 ? "y" : "ies"}`);
}

main().catch((err: unknown) => {
  console.error(err);
  process.exitCode = 1;
});
