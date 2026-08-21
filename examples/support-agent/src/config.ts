// Every knob the support agent has, in one place: which readproofd it talks to,
// which model answers, which policy documents are governed by Readproof, and
// where the ticket log lives. The CLI, the agent and the tests all read
// this module, so they can never disagree about any of it.

import path from "node:path";
import { fileURLToPath } from "node:url";

import type { Policy, SourceConfig } from "@readproof/sdk";

// ESM has no __dirname. Compiled output lives at dist/src/config.js, so the
// example root is two levels up from this file at runtime.
const exampleDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");

/**
 * Base URL of a running readproofd. `READPROOF_SERVER_URL` is accepted as a fallback
 * because that is the variable the Go CLI (`readproof --server`) already uses —
 * one export, and both halves of the demo point at the same server.
 */
export const READPROOF_ENDPOINT = process.env["READPROOF_ENDPOINT"] ?? process.env["READPROOF_SERVER_URL"] ?? "http://localhost:8080";

/** Only needed when readproofd was started with --api-key. */
export const READPROOF_API_KEY = process.env["READPROOF_API_KEY"];

/**
 * Chat model to answer with. When unset, `src/model.ts` asks Ollama what it
 * has and takes the first non-embedding model.
 */
export const OLLAMA_MODEL = process.env["OLLAMA_MODEL"];

/**
 * Where Ollama is listening. The `ollama` JS client (0.6.3) does NOT read
 * this variable itself — it hardcodes http://127.0.0.1:11434 — so the
 * example reads it and passes it to the client explicitly. Point it at a
 * remote or cloud-backed Ollama and nothing else in the example changes.
 */
export const OLLAMA_HOST = process.env["OLLAMA_HOST"] ?? "http://localhost:11434";

/**
 * Truthy -> answer with a deterministic fake model instead of calling
 * Ollama. The tests and CI run this way: the point of the example is what
 * Readproof pins, which must be provable without a GPU or a network.
 */
export const FAKE_MODEL = isTruthy(process.env["SUPPORT_FAKE_MODEL"]);

/**
 * Directory holding the three policy markdown files. Overridable so the
 * tests can register a throwaway copy and edit it freely — a test that
 * rewrites the repo's own fixtures leaves the working tree dirty when it
 * fails, which is exactly when you least want that.
 */
export const POLICY_DIR = process.env["SUPPORT_CONTEXT_DIR"]
  ? path.resolve(process.env["SUPPORT_CONTEXT_DIR"])
  : path.join(exampleDir, "context", "policies");

/** Where the ticket log lives. Overridable for the same reason as above. */
export const DATA_DIR = process.env["SUPPORT_DATA_DIR"]
  ? path.resolve(process.env["SUPPORT_DATA_DIR"])
  : path.join(exampleDir, "data");

/** One JSON object per line: the agent's own record of what it answered. */
export const TICKETS_FILE = path.join(DATA_DIR, "tickets.jsonl");

/** The tag the agent mounts the tone policy by. */
export const PROD_TAG = "prod";

export interface PolicyResource {
  /** Stable logical identity — what the agent mounts, and what a manifest records. */
  uri: string;
  /** Short name for the CLI (`history refunds` instead of the full URI). */
  name: string;
  /** File basename inside POLICY_DIR. */
  file: string;
  /** Freshness strategy readproofd applies when this resource is resolved bare. */
  policy: Policy;
  /**
   * When set, the agent mounts `uri@<mountRef>` instead of the bare URI:
   * exactly that tagged snapshot, no source fetch, policy not consulted.
   */
  mountRef?: string;
}

/**
 * The three documents this agent is allowed to answer from, and the
 * freshness contract each one gets. They are deliberately different:
 *
 *   refunds  — require_fresh: money is involved, always re-verify the source.
 *   shipping — allow_stale (1h): changes rarely, an hour-old copy is fine.
 *   tone     — require_fresh, but mounted @prod: house style only moves when
 *              someone promotes it, so editing the file changes nothing
 *              until `promote` says so.
 */
export const POLICY_RESOURCES: PolicyResource[] = [
  {
    uri: "readproof://acme/policies/refunds",
    name: "refunds",
    file: "refunds.md",
    policy: { strategy: "require_fresh" },
  },
  {
    uri: "readproof://acme/policies/shipping",
    name: "shipping",
    file: "shipping.md",
    // max_age_seconds is the API's spelling of "how stale is acceptable"
    // (docs/api.md, POST /v1/resources).
    policy: { strategy: "allow_stale", max_age_seconds: 3600 },
  },
  {
    uri: "readproof://acme/policies/tone",
    name: "tone",
    file: "tone.md",
    policy: { strategy: "require_fresh" },
    mountRef: PROD_TAG,
  },
];

/**
 * Absolute path to a policy file. Absolute because readproofd resolves a
 * filesystem source relative to its own working directory, which is never
 * this example's.
 */
export function policyPath(resource: PolicyResource): string {
  return path.join(POLICY_DIR, resource.file);
}

export function policySource(resource: PolicyResource): SourceConfig {
  return { kind: "filesystem", filesystem: { path: policyPath(resource) } };
}

/**
 * What the agent mounts, in order. Order is a hard Readproof invariant — it is
 * committed to the manifest and folded into the evidence Merkle root — so
 * it lives here rather than being rebuilt at each call site.
 */
export function mountSpecs(): string[] {
  return POLICY_RESOURCES.map((r) => (r.mountRef ? `${r.uri}@${r.mountRef}` : r.uri));
}

/** Accepts a full readproof:// URI or a short name ("refunds"). */
export function resolvePolicyURI(nameOrURI: string): string {
  if (nameOrURI.startsWith("readproof://")) {
    return nameOrURI;
  }
  const match = POLICY_RESOURCES.find((r) => r.name === nameOrURI);
  if (!match) {
    const names = POLICY_RESOURCES.map((r) => r.name).join(", ");
    throw new Error(`unknown policy "${nameOrURI}" — use a readproof:// URI or one of: ${names}`);
  }
  return match.uri;
}

function isTruthy(value: string | undefined): boolean {
  if (value === undefined) return false;
  return ["1", "true", "yes", "on"].includes(value.toLowerCase());
}
