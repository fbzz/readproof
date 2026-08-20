// Runs the graph once: register the demo resources if ctxd doesn't know
// them yet, invoke the graph on a fresh thread, read the manifest id back
// out of the checkpoint, and leave a record for `npm run replay`.
//
//   npm run start ["your question"]

import { randomUUID } from "node:crypto";

import { Ctx, CtxError } from "@ctx/sdk";
import type { StateSnapshot } from "@langchain/langgraph";

import {
  CONTEXT_RESOURCES,
  CTX_API_KEY,
  CTX_ENDPOINT,
  RUN_RECORD_FILE,
  writeRunRecord,
} from "./config.js";
import { buildGraph, MANIFEST_ID_KEY } from "./graph.js";

const DEFAULT_QUESTION = "A customer bought something 20 days ago and wants a refund. Can they get one?";

async function ensureResources(ctx: Ctx): Promise<void> {
  for (const resource of CONTEXT_RESOURCES) {
    try {
      const existing = await ctx.getResource(resource.uri);
      const registeredPath = existing.source.filesystem?.path;
      if (registeredPath !== resource.path) {
        console.warn(
          `warning: ${resource.uri} is already registered against ${registeredPath ?? "a non-filesystem source"}, ` +
            `not ${resource.path}. Start ctxd with a fresh --data-dir if that's not what you want.`,
        );
      }
      continue;
    } catch (err: unknown) {
      // Anything other than "not registered here yet" is a real problem.
      if (!(err instanceof CtxError) || err.status !== 404) throw err;
    }
    console.log(`registering ${resource.uri} -> ${resource.path}`);
    await ctx.registerResource({
      uri: resource.uri,
      source: { kind: "filesystem", filesystem: { path: resource.path } },
      policy: { strategy: "require_fresh" },
    });
  }
}

/**
 * Pulls the manifest id out of a checkpoint. Deliberately not read from
 * invoke()'s return value: the claim this example makes is that the id
 * survives in the checkpoint, so that's where it has to come from.
 */
function manifestIdFromCheckpoint(snapshot: StateSnapshot): string {
  const value: unknown = snapshot.values[MANIFEST_ID_KEY];
  if (typeof value !== "string" || value === "") {
    throw new Error(`checkpoint has no ${MANIFEST_ID_KEY}`);
  }
  return value;
}

async function main(): Promise<void> {
  const question = process.argv[2] ?? DEFAULT_QUESTION;
  const ctx = new Ctx({ endpoint: CTX_ENDPOINT, apiKey: CTX_API_KEY });
  await ensureResources(ctx);

  const threadId = randomUUID();
  const config = { configurable: { thread_id: threadId } };
  const graph = buildGraph();

  console.log(`ctxd:     ${CTX_ENDPOINT}`);
  console.log(`thread:   ${threadId}`);
  console.log(`question: ${question}`);

  const final = await graph.invoke({ question }, config);

  // The checkpointer, not the invoke() result, is the source of truth here.
  const snapshot = await graph.getState(config);
  const manifestId = manifestIdFromCheckpoint(snapshot);
  if (manifestId !== final.ctx_manifest_id) {
    throw new Error("checkpointed manifest id disagrees with the graph's final state");
  }

  console.log("\n--- context mounted by load_context ---");
  for (const [position, entry] of final.ctx_entries.entries()) {
    console.log(`  [${position}] ${entry.uri}`);
    console.log(`        snapshot ${entry.snapshot_id}`);
    console.log(`        hash     ${entry.content_hash}`);
  }

  console.log("\n--- answer ---");
  console.log(final.answer);

  console.log("\n--- checkpoint ---");
  console.log(`  ${MANIFEST_ID_KEY} = ${manifestId}`);
  console.log(`  read back from graph.getState({ thread_id: ${threadId} })`);

  writeRunRecord({
    thread_id: threadId,
    manifest_id: manifestId,
    endpoint: CTX_ENDPOINT,
    recorded_at: new Date().toISOString(),
  });
  console.log(`\nwrote ${RUN_RECORD_FILE}`);
  console.log("now run: npm run replay");
}

main().catch((err: unknown) => {
  console.error(err);
  process.exitCode = 1;
});
