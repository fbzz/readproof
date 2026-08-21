// The support agent's command line.
//
//   npm run agent -- setup
//   npm run agent -- ask 1001 "I bought headphones 20 days ago. Refund?"
//   npm run agent -- show 1001
//   npm run agent -- replay 1001
//   npm run agent -- diff 1001 1002
//   npm run agent -- evidence 1001 --with-content
//   npm run agent -- promote refunds
//   npm run agent -- history refunds

import fs from "node:fs";
import path from "node:path";

import { buildEvidence, encodeEvidence } from "@readproof/sdk";
import type { DiffEntry, ManifestEntry } from "@readproof/sdk";

import { READPROOF_ENDPOINT, FAKE_MODEL, PROD_TAG, resolvePolicyURI } from "./config.js";
import { ask, readproofClient, loadTicket, requireTicketId, setup, ticketsFile } from "./agent.js";
import { shortHash } from "./model.js";

const USAGE = `support-agent — answer support tickets from Readproof-governed policy documents

usage:
  npm run agent -- <command> [args]        (or: node dist/src/cli.js <command>)

commands:
  setup                              check readproofd, register the three policies,
                                     tag the tone policy @${PROD_TAG}
  ask <ticket> <question...>         answer a ticket; commits one manifest
  show <ticket>                      the stored answer and its manifest entries
  replay <ticket>                    reconstruct the exact bytes that answer used
  diff <ticketA> <ticketB>           what changed in the context between two tickets
  evidence <ticket> [--out <file>] [--with-content]
                                     write an in-toto evidence bundle
  promote <policy> [snapshot-id]     move the @${PROD_TAG} tag (default: resolve, then
                                     promote whatever the policy says is current)
  history <policy>                   snapshots and tags for one policy

  <policy> is a readproof:// URI or a short name: refunds, shipping, tone

environment:
  READPROOF_ENDPOINT         readproofd base URL (default http://localhost:8080)
  READPROOF_API_KEY          bearer token, if readproofd was started with --api-key
  OLLAMA_HOST          Ollama base URL (default http://localhost:11434)
  OLLAMA_MODEL         chat model; default = first non-embedding model Ollama lists
  SUPPORT_FAKE_MODEL   1 = deterministic fake model, no Ollama needed
`;

async function main(argv: string[]): Promise<void> {
  const command = argv[0];

  if (command === undefined || command === "--help" || command === "-h" || command === "help") {
    process.stdout.write(USAGE);
    // No command at all is misuse, so it is worth an exit code; --help is not.
    process.exitCode = command === undefined ? 1 : 0;
    return;
  }

  const args = argv.slice(1);
  switch (command) {
    case "setup":
      return cmdSetup();
    case "ask":
      return cmdAsk(args);
    case "show":
      return cmdShow(args);
    case "replay":
      return cmdReplay(args);
    case "diff":
      return cmdDiff(args);
    case "evidence":
      return cmdEvidence(args);
    case "promote":
      return cmdPromote(args);
    case "history":
      return cmdHistory(args);
    default:
      throw new UsageError(`unknown command "${command}"`);
  }
}

async function cmdSetup(): Promise<void> {
  const result = await setup((line) => console.log(line));
  const total = result.registered.length + result.alreadyRegistered.length;
  console.log(`\n${total} policies governed by Readproof, ${result.registered.length} registered just now.`);
  if (result.mismatched.length > 0) {
    console.log(`${result.mismatched.length} registered against a different path — see the warnings above.`);
  }
}

async function cmdAsk(args: string[]): Promise<void> {
  const question = args.slice(1).join(" ").trim();
  if (!args[0] || question === "") {
    throw new UsageError('ask needs a ticket id and a question: ask 1001 "can I get a refund?"');
  }
  const ticket = requireTicket(args[0], "ask");

  console.log(`ticket:   ${ticket}`);
  console.log(`question: ${question}`);
  console.log("");

  const record = await ask(ticket, question);

  console.log("");
  console.log(`manifest: ${record.manifest_id}`);
  printEntries(record.entries);
  console.log(`\nlogged to ${ticketsFile}`);
}

async function cmdShow(args: string[]): Promise<void> {
  const ticket = requireTicket(args[0], "show");
  const record = loadTicket(ticket);

  console.log(`ticket:   ${record.ticket}`);
  console.log(`asked:    ${record.at}`);
  console.log(`question: ${record.question}`);
  console.log(`model:    ${record.model}`);
  console.log(`run:      ${record.run_id}`);
  console.log(`manifest: ${record.manifest_id}`);
  console.log("\n--- answer ---");
  console.log(record.answer);

  // Read the manifest back from readproofd rather than trusting the local log:
  // the manifest is the record of what happened, the log is a convenience.
  const manifest = await readproofClient().getManifest(record.manifest_id);
  console.log("\n--- manifest entries (from readproofd) ---");
  printEntries(manifest.entries);
}

async function cmdReplay(args: string[]): Promise<void> {
  const ticket = requireTicket(args[0], "replay");
  const record = loadTicket(ticket);
  const rp = readproofClient();

  console.log(`ticket:   ${record.ticket}`);
  console.log(`manifest: ${record.manifest_id}  (answered ${record.at})`);

  const replay = await rp.replay(record.manifest_id);
  let mismatches = 0;
  let drifted = 0;

  for (const entry of replay.entries) {
    const ok = entry.match && entry.recorded_hash === entry.replayed_hash;
    if (!ok) mismatches += 1;

    console.log(`\n  [${entry.position}] ${entry.uri}`);
    console.log(`        recorded ${entry.recorded_hash}`);
    console.log(`        replayed ${entry.replayed_hash}   ${ok ? "MATCH" : "MISMATCH"}`);
    console.log(indent(entry.content));

    // Same URI, no ref, today's bytes — which is precisely what the replay
    // above refuses to be affected by.
    const live = await rp.resolve(entry.uri);
    if (live.snapshot.content_hash === entry.recorded_hash) {
      console.log("        live source: unchanged");
    } else {
      drifted += 1;
      console.log(`        live source: CHANGED -> ${live.snapshot.content_hash}`);
    }
  }

  const matched = replay.entries.length - mismatches;
  console.log(`\nReplay verified: ${matched}/${replay.entries.length} entries match.`);
  if (drifted > 0) {
    console.log(
      `${drifted} of them no longer match the live source — the manifest, not the source, is what a replay reads.`,
    );
  }
  if (mismatches > 0) {
    throw new Error(`${mismatches} entr${mismatches === 1 ? "y" : "ies"} failed the SHA256 replay invariant`);
  }
}

async function cmdDiff(args: string[]): Promise<void> {
  const a = requireTicket(args[0], "diff");
  const b = requireTicket(args[1], "diff");
  const recordA = loadTicket(a);
  const recordB = loadTicket(b);

  const result = await readproofClient().diff(recordA.manifest_id, recordB.manifest_id);
  console.log(`--- ticket ${a} (${result.manifest_a.manifest_id})`);
  console.log(`+++ ticket ${b} (${result.manifest_b.manifest_id})`);

  const counts: Record<string, number> = { changed: 0, added: 0, removed: 0, unchanged: 0 };
  for (const entry of result.entries) {
    counts[entry.status] = (counts[entry.status] ?? 0) + 1;
    console.log("");
    console.log(`${marker(entry.status)} ${entry.uri}  ${snapshotRange(entry)}`);
    if (entry.status !== "changed") {
      continue;
    }
    const why = whyLine(entry);
    if (why) console.log(`  why: ${why}`);
    if (entry.unified_diff) console.log(indent(entry.unified_diff, "  "));
  }

  console.log(
    `\n${counts["changed"]} resource${counts["changed"] === 1 ? "" : "s"} changed, ` +
      `${counts["added"]} added, ${counts["removed"]} removed, ${counts["unchanged"]} unchanged`,
  );
}

async function cmdEvidence(args: string[]): Promise<void> {
  const positional = args.filter((a) => !a.startsWith("--"));
  const ticket = requireTicket(positional[0], "evidence");
  const withContent = args.includes("--with-content");

  const outIndex = args.indexOf("--out");
  if (outIndex !== -1 && args[outIndex + 1] === undefined) {
    throw new UsageError("--out needs a file path");
  }
  const outFile = path.resolve(outIndex === -1 ? `ticket-${ticket}.bundle.json` : (args[outIndex + 1] as string));

  const record = loadTicket(ticket);
  const bundle = await buildEvidence(readproofClient(), record.manifest_id, { withContent });
  fs.mkdirSync(path.dirname(outFile), { recursive: true });
  fs.writeFileSync(outFile, encodeEvidence(bundle), "utf-8");

  const root = bundle.subject[0]?.digest.sha256 ?? "";
  console.log(`evidence bundle written to ${outFile}`);
  console.log(`  entries:     ${bundle.predicate.entries.length}${withContent ? " (with embedded content)" : " (metadata only)"}`);
  console.log(`  merkle root: ${root}`);
  console.log(`  replay:      ${bundle.predicate.replay.all_match ? "all entries match" : "MISMATCH — see predicate.replay"}`);
  console.log("\nverify it with the Go CLI:");
  console.log(`  readproof --server ${READPROOF_ENDPOINT} evidence verify ${outFile}`);
}

async function cmdPromote(args: string[]): Promise<void> {
  const target = args[0];
  if (!target) {
    throw new UsageError("promote needs a policy: promote refunds [snapshot-id]");
  }
  const uri = resolvePolicyURI(target);
  const rp = readproofClient();

  let snapshotId = args[1];
  if (!snapshotId) {
    // "Current" has to mean whatever the resource's own freshness policy
    // says is current *now*, so resolve before reading it back. Skipping
    // this would silently re-promote the snapshot from before the edit the
    // operator is trying to promote.
    await rp.resolve(uri);
    const resource = await rp.getResource(uri);
    snapshotId = resource.current_snapshot_id ?? (await rp.history(uri))[0]?.id;
  }
  if (!snapshotId) {
    throw new Error(`${uri} has no snapshots yet — resolve it once first (npm run agent -- history ${target})`);
  }

  const tag = await rp.setTag(uri, PROD_TAG, snapshotId);
  console.log(`${uri}@${PROD_TAG} -> ${tag.snapshot_id}`);
}

async function cmdHistory(args: string[]): Promise<void> {
  const target = args[0];
  if (!target) {
    throw new UsageError("history needs a policy: history refunds");
  }
  const uri = resolvePolicyURI(target);
  const rp = readproofClient();

  const [snapshots, tags] = await Promise.all([rp.history(uri), rp.listTags(uri)]);
  const tagsBySnapshot = new Map<string, string[]>();
  for (const tag of tags) {
    tagsBySnapshot.set(tag.snapshot_id, [...(tagsBySnapshot.get(tag.snapshot_id) ?? []), tag.tag]);
  }

  console.log(uri);
  console.log(pad("SNAPSHOT", 33) + pad("OBSERVED", 30) + pad("CONTENT_HASH", 22) + "TAGS");
  for (const snapshot of snapshots) {
    console.log(
      pad(snapshot.id, 33) +
        pad(snapshot.observed_at, 30) +
        pad(shortHash(snapshot.content_hash), 22) +
        (tagsBySnapshot.get(snapshot.id) ?? []).join(","),
    );
  }
  if (snapshots.length === 0) {
    console.log("(no snapshots yet)");
  }
}

interface PrintableEntry {
  uri: string;
  ref?: string;
  snapshot_id: string;
  content_hash: string;
  position?: number;
}

function printEntries(entries: PrintableEntry[] | ManifestEntry[]): void {
  console.log(pad("POS", 5) + pad("URI@REF", 40) + pad("SNAPSHOT", 33) + "HASH");
  for (const [index, entry] of entries.entries()) {
    const position = entry.position ?? index;
    const label = entry.ref ? `${entry.uri}@${entry.ref}` : entry.uri;
    console.log(pad(String(position), 5) + pad(label, 40) + pad(entry.snapshot_id, 33) + shortHash(entry.content_hash));
  }
}

/** The provenance behind a change: only the fields both sides actually have. */
function whyLine(entry: DiffEntry): string {
  const parts: string[] = [];
  if (entry.source_revision_a && entry.source_revision_b) {
    parts.push(`source revision ${entry.source_revision_a} -> ${entry.source_revision_b}`);
  }
  if (entry.observed_at_a && entry.observed_at_b) {
    parts.push(`observed ${entry.observed_at_a} -> ${entry.observed_at_b}`);
  }
  if (entry.ref_a || entry.ref_b) {
    parts.push(`ref ${entry.ref_a ?? "-"} -> ${entry.ref_b ?? "-"}`);
  }
  return parts.join("; ");
}

function snapshotRange(entry: DiffEntry): string {
  if (entry.status === "unchanged") return `(${entry.snapshot_id_a ?? "?"})`;
  if (entry.status === "added") return `(-> ${entry.snapshot_id_b ?? "?"})`;
  if (entry.status === "removed") return `(${entry.snapshot_id_a ?? "?"} ->)`;
  return `(${entry.snapshot_id_a ?? "?"} -> ${entry.snapshot_id_b ?? "?"})`;
}

function marker(status: string): string {
  return { changed: "~", added: "+", removed: "-", unchanged: "=" }[status] ?? "?";
}

function indent(text: string, prefix = "      | "): string {
  return text.trimEnd().split("\n").map((line) => `${prefix}${line}`).join("\n");
}

function pad(value: string, width: number): string {
  return value.length >= width ? `${value}  ` : value.padEnd(width);
}

/**
 * A ticket id from argv, checked before it reaches a path or a run id.
 *
 * `evidence` resolves `ticket-${ticket}.bundle.json` and then mkdir -p's the
 * directory it lands in, so an unchecked `../../x` writes outside the example.
 * The one rule lives in agent.ts, next to the run ids it also shapes.
 */
function requireTicket(value: string | undefined, command: string): string {
  if (!value) {
    throw new UsageError(`${command} needs a ticket id`);
  }
  try {
    return requireTicketId(value);
  } catch (err) {
    throw new UsageError(err instanceof Error ? err.message : String(err));
  }
}

/** Misuse of the CLI, as opposed to something going wrong while running. */
class UsageError extends Error {}

main(process.argv.slice(2)).catch((err: unknown) => {
  const message = err instanceof Error ? err.message : String(err);
  console.error(`error: ${message}`);
  // Usage gets dumped for misuse only. A failed replay or an unreachable
  // readproofd is a runtime failure, and burying the one line that matters under
  // a wall of usage text helps nobody.
  if (err instanceof UsageError) {
    console.error("\nrun with --help for usage");
  }
  if (!FAKE_MODEL && /Ollama/.test(message)) {
    console.error("hint: SUPPORT_FAKE_MODEL=1 runs the whole example without Ollama");
  }
  process.exitCode = 1;
});
