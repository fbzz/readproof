// Replays the manifest a previous graph run checkpointed, and proves two
// things at once:
//
//   1. every entry replays byte-for-byte (recorded hash == replayed hash),
//      reconstructed from Ctx's store without touching the live source;
//   2. if the live source has since changed, the replay still returns the
//      ORIGINAL bytes — the live re-resolve below is what shows the drift.
//
//   npm run replay                    # the manifest the last run checkpointed
//   npm run replay -- <manifest-id>   # any older manifest or run id

import { Ctx } from "@ctx/sdk";

import { CTX_API_KEY, CTX_ENDPOINT, readRunRecord } from "./config.js";

function indent(text: string): string {
  return text.trimEnd().split("\n").map((line) => `      | ${line}`).join("\n");
}

async function main(): Promise<void> {
  const override = process.argv[2];
  const record = readRunRecord();
  const target = override ?? record.manifest_id;
  const ctx = new Ctx({ endpoint: CTX_ENDPOINT, apiKey: CTX_API_KEY });

  if (override) {
    console.log(`manifest: ${target}   (from the command line)`);
  } else {
    console.log(`thread:   ${record.thread_id}`);
    console.log(`manifest: ${target}   (checkpointed ${record.recorded_at})`);
  }

  const replay = await ctx.replay(target);

  let mismatches = 0;
  let drifted = 0;

  for (const entry of replay.entries) {
    const ok = entry.match && entry.recorded_hash === entry.replayed_hash;
    if (!ok) mismatches += 1;

    console.log(`\n  [${entry.position}] ${entry.uri}`);
    console.log(`        recorded ${entry.recorded_hash}`);
    console.log(`        replayed ${entry.replayed_hash}   ${ok ? "MATCH" : "MISMATCH"}`);
    console.log(indent(entry.content));

    // Re-resolve the live resource. Same URI, today's bytes — which is
    // exactly what the replay above refuses to be affected by.
    const live = await ctx.resolve(entry.uri);
    if (live.snapshot.content_hash === entry.recorded_hash) {
      console.log("        live source: unchanged since the run");
    } else {
      drifted += 1;
      console.log(`        live source: CHANGED since the run -> ${live.snapshot.content_hash}`);
      console.log(indent(live.content));
    }
  }

  console.log(
    `\nReplay verified: ${replay.entries.length - mismatches}/${replay.entries.length} entries match their recorded hash.`,
  );
  if (drifted > 0) {
    console.log(
      `${drifted} of them no longer match the live source — the manifest, not the source, is what the replay reads.`,
    );
  }

  if (mismatches > 0) {
    throw new Error(`${mismatches} entr${mismatches === 1 ? "y" : "ies"} failed the SHA256 replay invariant`);
  }
}

main().catch((err: unknown) => {
  console.error(err);
  process.exitCode = 1;
});
