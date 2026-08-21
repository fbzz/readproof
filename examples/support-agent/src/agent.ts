// The agent itself: one Readproof run per ticket.
//
//   run.mount(uri)  resolves a policy AND records it as the next ordered
//                   entry of the run
//   answer(...)     sees exactly those bytes, nothing else
//   run.commit()    freezes the entries into an immutable manifest
//
// The ticket record then stores the manifest id next to the answer, which
// is the whole trick: months later `replay <ticket>` reconstructs the exact
// policy text that produced that reply, even if every source file has moved
// on since.

import fs from "node:fs";
import path from "node:path";

import { Readproof, ReadproofError } from "@readproof/sdk";
import type { Tag } from "@readproof/sdk";

import {
  READPROOF_API_KEY,
  READPROOF_ENDPOINT,
  DATA_DIR,
  POLICY_RESOURCES,
  PROD_TAG,
  TICKETS_FILE,
  mountSpecs,
  policyPath,
  policySource,
} from "./config.js";
import { answer } from "./model.js";
import type { AnswerOptions, ContextEntry } from "./model.js";

/** One manifest entry as the ticket log stores it. */
export interface TicketEntry {
  uri: string;
  /** The "@<tag>" it was mounted by; absent for a plain URI. */
  ref?: string;
  snapshot_id: string;
  content_hash: string;
}

/** One answered ticket — a line of data/tickets.jsonl. */
export interface TicketRecord {
  ticket: string;
  question: string;
  answer: string;
  model: string;
  manifest_id: string;
  run_id: string;
  entries: TicketEntry[];
  at: string;
}

export function readproofClient(): Readproof {
  return new Readproof({ endpoint: READPROOF_ENDPOINT, apiKey: READPROOF_API_KEY });
}

/** Run id for a ticket. Deterministic, so `show`/`replay` need no lookup table. */
export function runIdFor(ticketId: string): string {
  return `ticket-${ticketId}`;
}

export type Logger = (line: string) => void;

export interface SetupResult {
  registered: string[];
  alreadyRegistered: string[];
  /** URIs whose registered path is not the one this checkout would use. */
  mismatched: string[];
  /** The tone tag, whether it was just created or already existed. */
  toneTag: Tag;
  toneTagCreated: boolean;
}

/**
 * Make readproofd ready to answer tickets: reachable, all three policies
 * registered, and the tone policy carrying a `prod` tag for the agent to
 * mount by. Idempotent — running it twice changes nothing.
 */
export async function setup(log: Logger = () => {}): Promise<SetupResult> {
  await checkHealth(log);

  const rp = readproofClient();
  const result: SetupResult = {
    registered: [],
    alreadyRegistered: [],
    mismatched: [],
    toneTag: { uri: "", tag: "", snapshot_id: "", updated_at: "" },
    toneTagCreated: false,
  };

  for (const resource of POLICY_RESOURCES) {
    const wantPath = policyPath(resource);
    const existing = await getResourceOrNull(rp, resource.uri);

    if (existing) {
      const havePath = existing.source.filesystem?.path;
      if (havePath !== wantPath) {
        result.mismatched.push(resource.uri);
        log(`warning: ${resource.uri} is registered against a different path`);
        log(`  registered: ${havePath ?? "(not a filesystem source)"}`);
        log(`  this checkout: ${wantPath}`);
        log("  start readproofd with a fresh --data-dir if that is not what you want");
      } else {
        log(`ok       ${resource.uri} (already registered)`);
      }
      result.alreadyRegistered.push(resource.uri);
      continue;
    }

    await rp.registerResource({
      uri: resource.uri,
      source: policySource(resource),
      policy: resource.policy,
    });
    result.registered.push(resource.uri);
    log(`register   ${resource.uri} -> ${wantPath} (${describePolicy(resource.uri)})`);
  }

  // The agent mounts tone@prod, so a `prod` tag has to exist before the
  // first ticket. Resolving once creates the snapshot to point it at.
  const tone = POLICY_RESOURCES.find((r) => r.mountRef === PROD_TAG);
  if (!tone) {
    throw new Error("no policy is configured to be mounted by tag — check src/config.ts");
  }

  const tags = await rp.listTags(tone.uri);
  const existingTag = tags.find((t) => t.tag === PROD_TAG);
  if (existingTag) {
    result.toneTag = existingTag;
    log(`ok       ${tone.uri}@${PROD_TAG} -> ${existingTag.snapshot_id} (already tagged)`);
    return result;
  }

  const resolved = await rp.resolve(tone.uri);
  result.toneTag = await rp.setTag(tone.uri, PROD_TAG, resolved.snapshot.id);
  result.toneTagCreated = true;
  log(`tag        ${tone.uri}@${PROD_TAG} -> ${resolved.snapshot.id}`);
  return result;
}

export interface AskResult extends TicketRecord {
  /** The bytes the model saw, kept out of the persisted record. */
  mounted: ContextEntry[];
}

/**
 * Answer one ticket. Mounts every policy into a single run, calls the model
 * with exactly those bytes, commits the manifest, and appends the result to
 * the ticket log.
 */
export async function ask(
  ticketId: string,
  question: string,
  opts: AnswerOptions = {},
): Promise<AskResult> {
  const rp = readproofClient();
  const runId = runIdFor(ticketId);
  const run = rp.run({ id: runId });

  const mounted: ContextEntry[] = [];
  for (const spec of mountSpecs()) {
    const resolved = await run.mount(spec);
    mounted.push({
      // resource.uri is always the bare readproof://ns/path; the tag, if any, is
      // reported separately as resource.ref — same split the manifest uses.
      uri: resolved.resource.uri,
      ...(resolved.resource.ref ? { ref: resolved.resource.ref } : {}),
      snapshot_id: resolved.snapshot.id,
      content_hash: resolved.snapshot.content_hash,
      content: resolved.content,
    });
  }

  const { text, model } = await answer(question, mounted, opts);
  const manifest = await run.commit();

  const record: TicketRecord = {
    ticket: ticketId,
    question,
    answer: text,
    model,
    manifest_id: manifest.manifest_id,
    run_id: manifest.run_id,
    entries: mounted.map((e) => ({
      uri: e.uri,
      ...(e.ref ? { ref: e.ref } : {}),
      snapshot_id: e.snapshot_id,
      content_hash: e.content_hash,
    })),
    at: new Date().toISOString(),
  };

  appendTicket(record);
  return { ...record, mounted };
}

/** The most recent record for a ticket. Throws if the ticket is unknown. */
export function loadTicket(ticketId: string): TicketRecord {
  if (!fs.existsSync(TICKETS_FILE)) {
    throw new Error(`no ticket log at ${TICKETS_FILE} — answer a ticket first: npm run agent -- ask <id> "<question>"`);
  }

  let found: TicketRecord | undefined;
  for (const line of fs.readFileSync(TICKETS_FILE, "utf-8").split("\n")) {
    if (line.trim() === "") continue;
    const parsed: unknown = JSON.parse(line);
    // The log is append-only and written by another process run; its shape
    // is an assumption until checked.
    if (isTicketRecord(parsed) && parsed.ticket === ticketId) {
      found = parsed;
    }
  }

  if (!found) {
    throw new Error(`ticket ${ticketId} is not in ${TICKETS_FILE}`);
  }
  return found;
}

export function appendTicket(record: TicketRecord): void {
  fs.mkdirSync(DATA_DIR, { recursive: true });
  fs.appendFileSync(TICKETS_FILE, `${JSON.stringify(record)}\n`, "utf-8");
}

/**
 * /healthz is the one route that never requires auth, and the SDK has no
 * method for it (it is not part of the data API), so this is a plain fetch.
 */
async function checkHealth(log: Logger): Promise<void> {
  let response: Response;
  try {
    response = await fetch(`${READPROOF_ENDPOINT.replace(/\/+$/, "")}/healthz`);
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : String(err);
    throw new Error(
      `cannot reach readproofd at ${READPROOF_ENDPOINT}: ${message} — start one with: go run ./cmd/readproofd --data-dir .readproof`,
    );
  }
  if (!response.ok) {
    throw new Error(`readproofd at ${READPROOF_ENDPOINT} answered /healthz with ${response.status}`);
  }
  log(`readproofd ${READPROOF_ENDPOINT} ok`);
}

async function getResourceOrNull(rp: Readproof, uri: string) {
  try {
    return await rp.getResource(uri);
  } catch (err: unknown) {
    // Anything but "not registered here yet" is a real problem.
    if (err instanceof ReadproofError && err.status === 404) {
      return null;
    }
    throw err;
  }
}

function describePolicy(uri: string): string {
  const resource = POLICY_RESOURCES.find((r) => r.uri === uri);
  if (!resource) return "";
  const { strategy, max_age_seconds } = resource.policy;
  return max_age_seconds ? `${strategy}, max age ${max_age_seconds}s` : strategy;
}

function isTicketRecord(value: unknown): value is TicketRecord {
  if (typeof value !== "object" || value === null) return false;
  const record = value as Record<string, unknown>;
  return (
    typeof record["ticket"] === "string" &&
    typeof record["question"] === "string" &&
    typeof record["answer"] === "string" &&
    typeof record["manifest_id"] === "string" &&
    Array.isArray(record["entries"])
  );
}

/** Where the ticket log lives, for messages that want to name it. */
export const ticketsFile = TICKETS_FILE;
export const ticketsDir = path.dirname(TICKETS_FILE);
