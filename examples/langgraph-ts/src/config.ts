// Settings shared by the graph, the runner and the replayer. Kept in one
// place so the three can never disagree about which readproofd they talk to,
// which resources they mount, or where the run record lives.

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

// Compiled output lives in dist/, so the example root is one level up.
const exampleDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = path.resolve(exampleDir, "../..");

/** readproofd base URL. Matches the env var the Go CLI uses (`readproof --server`). */
export const READPROOF_ENDPOINT = process.env.READPROOF_SERVER_URL ?? "http://localhost:8080";

/** Only needed if readproofd was started with --api-key. */
export const READPROOF_API_KEY = process.env.READPROOF_API_KEY;

export interface DemoResource {
  /** Logical, stable identity — what the graph mounts. */
  uri: string;
  /**
   * Absolute path on the machine running readproofd. Absolute, because readproofd
   * resolves a filesystem source relative to its own working directory,
   * which is not this example's.
   */
  path: string;
}

/**
 * The context this agent needs. The refunds policy is the repo's canonical
 * demo fixture (`examples/refund-agent/`) — the same file the CLI
 * walkthrough edits, so the "replay survives an edit" proof is the same
 * proof, driven from a graph instead of a shell.
 */
export const CONTEXT_RESOURCES: DemoResource[] = [
  {
    uri: "readproof://demo/policies/refunds",
    path: path.join(repoRoot, "examples", "refund-agent", "policies", "refunds.md"),
  },
  {
    uri: "readproof://demo/policies/tone",
    path: path.join(exampleDir, "context", "tone.md"),
  },
];

/** Handoff file: how `npm run replay` (a separate process) finds the run. */
export const RUN_RECORD_FILE = path.join(exampleDir, "last-run.json");

export interface RunRecord {
  /** LangGraph thread id — the checkpoint's key. */
  thread_id: string;
  /** Read back out of the checkpoint, not out of a local variable. */
  manifest_id: string;
  /** Informational: which readproofd produced it, and when. */
  endpoint: string;
  recorded_at: string;
}

export function writeRunRecord(record: RunRecord): void {
  fs.writeFileSync(RUN_RECORD_FILE, `${JSON.stringify(record, null, 2)}\n`, "utf-8");
}

export function readRunRecord(): RunRecord {
  if (!fs.existsSync(RUN_RECORD_FILE)) {
    throw new Error(`no run record at ${RUN_RECORD_FILE} — run \`npm run start\` first`);
  }
  const parsed: unknown = JSON.parse(fs.readFileSync(RUN_RECORD_FILE, "utf-8"));
  if (!isRunRecord(parsed)) {
    throw new Error(`malformed run record at ${RUN_RECORD_FILE}`);
  }
  return parsed;
}

// Validated rather than cast: the file is written by another process, so
// its shape is an assumption until proven.
function isRunRecord(value: unknown): value is RunRecord {
  if (typeof value !== "object" || value === null) return false;
  const record = value as Record<string, unknown>;
  return (
    typeof record["thread_id"] === "string" &&
    typeof record["manifest_id"] === "string" &&
    typeof record["endpoint"] === "string" &&
    typeof record["recorded_at"] === "string"
  );
}
