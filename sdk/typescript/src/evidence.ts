// Evidence bundles: an in-toto Statement v1 describing one Readproof manifest,
// digested by a merkle root over its entries.
//
// This is the client-side twin of Go's internal/evidence. It is composed
// entirely from calls the SDK already makes (getManifest / getSnapshot /
// getResource / replay), so a bundle built here is byte-comparable with one
// exported by `readproof evidence export` against the same readproofd — most
// importantly, both produce the same merkle root for the same manifest.

import { createHash } from "node:crypto";

import type { Readproof } from "./client.js";
import { ReadproofError } from "./errors.js";
import type { FilesystemConfig, GitHubConfig, HTTPConfig, Manifest, SourceKind } from "./types.js";

/** in-toto Statement v1 type. Mirrors evidence.StatementType in Go. */
export const EVIDENCE_STATEMENT_TYPE = "https://in-toto.io/Statement/v1";

/**
 * PLACEHOLDER predicate type URN — Readproof has not settled its final
 * predicate schema. Kept as a single const (as in Go's
 * evidence.PredicateType) so a bump is a one-line change on each side.
 */
export const EVIDENCE_PREDICATE_TYPE = "urn:readproof:evidence:v0.3";

export const EVIDENCE_EXPORTER_NAME = "readproof";
export const EVIDENCE_EXPORTER_VERSION = "0.3.0";

export const EVIDENCE_MERKLE_ALGORITHM = "sha256";
export const EVIDENCE_MERKLE_LEAF_FORMULA =
  "sha256(position_be_uint32 || 0x00 || uri || 0x00 || content_hash)";

export interface EvidenceDigest {
  sha256: string;
}

export interface EvidenceSubject {
  name: string;
  digest: EvidenceDigest;
}

export interface EvidenceExporter {
  name: string;
  version: string;
}

export interface EvidenceMerkle {
  algorithm: string;
  leaf: string;
  root: string;
}

export interface EvidenceEntry {
  position: number;
  uri: string;
  /** The "@<tag>" the entry was mounted by; absent for a plain URI. Descriptive only — never part of the merkle leaf. */
  ref?: string;
  snapshot_id: string;
  materialization_id: string;
  content_hash: string;
  source_revision: string;
  observed_at: string;
  content_type: string;
  bytes: number;
  provenance: Record<string, string>;
  /** Present only with `{ withContent: true }`. */
  content_b64?: string;
}

export interface EvidenceSourceConfig {
  filesystem?: FilesystemConfig;
  github?: GitHubConfig;
  http?: HTTPConfig;
}

export interface EvidenceSource {
  kind: SourceKind | string;
  config: EvidenceSourceConfig;
}

export interface EvidencePolicy {
  strategy: string;
  max_age_seconds?: number;
  pinned_snapshot_id?: string;
}

export interface EvidenceResource {
  uri: string;
  namespace: string;
  path: string;
  source: EvidenceSource;
  policy: EvidencePolicy;
  /** True when the resource definition no longer exists. */
  missing?: boolean;
}

export interface EvidenceReplayEntry {
  position: number;
  match: boolean;
  expected_hash: string;
  actual_hash: string;
  error?: string;
}

export interface EvidenceReplay {
  verified_at: string;
  all_match: boolean;
  entries: EvidenceReplayEntry[];
  /** Set when replay could not run at all (e.g. a blob is gone). */
  error?: string;
}

export interface EvidencePredicate {
  run_id: string;
  manifest_id: string;
  manifest_created_at: string;
  generated_at: string;
  exporter: EvidenceExporter;
  merkle: EvidenceMerkle;
  entries: EvidenceEntry[];
  resources: EvidenceResource[];
  replay: EvidenceReplay;
}

export interface EvidenceBundle {
  _type: string;
  subject: EvidenceSubject[];
  predicateType: string;
  predicate: EvidencePredicate;
}

export interface BuildEvidenceOptions {
  /** Embed each entry's bytes as base64 in `content_b64`. */
  withContent?: boolean;
  /** Clock override, for byte-stable bundles in tests. */
  now?: () => Date;
}

/**
 * merkleLeaf hashes one entry:
 *
 *   sha256(position_be_uint32 || 0x00 || uri || 0x00 || content_hash)
 *
 * The position is a fixed-width big-endian uint32 and the string fields are
 * 0x00-separated, so no two distinct entries can serialize to the same
 * bytes. content_hash is hashed as the recorded "sha256:<hex>" string.
 */
export function merkleLeaf(entry: Pick<EvidenceEntry, "position" | "uri" | "content_hash">): Buffer {
  const position = Buffer.alloc(4);
  position.writeUInt32BE(entry.position >>> 0, 0);
  return createHash("sha256")
    .update(position)
    .update(Buffer.from([0]))
    .update(Buffer.from(entry.uri, "utf-8"))
    .update(Buffer.from([0]))
    .update(Buffer.from(entry.content_hash, "utf-8"))
    .digest();
}

/**
 * merkleRoot computes the hex root of a standard binary merkle tree over
 * the entries' leaves in the order given (manifest position order — order
 * is a hard Readproof invariant, so it is committed to, never sorted away).
 *
 * Rules, identical to Go's evidence.MerkleRoot:
 *   - zero entries      -> sha256 of the empty input
 *   - exactly one entry -> the root is that entry's leaf
 *   - odd level         -> the last node is paired with itself (the Bitcoin
 *     rule), then parent = sha256(left || right)
 */
export function merkleRoot(
  entries: ReadonlyArray<Pick<EvidenceEntry, "position" | "uri" | "content_hash">>,
): string {
  if (entries.length === 0) {
    return createHash("sha256").digest("hex");
  }

  let level = entries.map((e) => merkleLeaf(e));
  while (level.length > 1) {
    const next: Buffer[] = [];
    for (let i = 0; i < level.length; i += 2) {
      const left = level[i] as Buffer;
      const right = (level[i + 1] ?? left) as Buffer;
      next.push(createHash("sha256").update(left).update(right).digest());
    }
    level = next;
  }
  return (level[0] as Buffer).toString("hex");
}

/**
 * buildEvidence assembles an evidence bundle for a manifest id or run id,
 * using only public SDK calls.
 *
 * ```ts
 * const bundle = await buildEvidence(rp, "run-a", { withContent: true });
 * console.log(bundle.subject[0].digest.sha256);
 * ```
 */
export async function buildEvidence(
  rp: Readproof,
  target: string,
  opts: BuildEvidenceOptions = {},
): Promise<EvidenceBundle> {
  const manifest = await rp.getManifest(target);
  const now = (opts.now ? opts.now() : new Date()).toISOString();

  const replay = await buildReplay(rp, target, manifest, now);
  const entries = await buildEntries(rp, manifest, replay.contentByPosition, opts.withContent === true);
  const resources = await buildResources(rp, manifest);
  const root = merkleRoot(entries);

  return {
    _type: EVIDENCE_STATEMENT_TYPE,
    subject: [{ name: manifest.manifest_id, digest: { sha256: root } }],
    predicateType: EVIDENCE_PREDICATE_TYPE,
    predicate: {
      run_id: manifest.run_id,
      manifest_id: manifest.manifest_id,
      manifest_created_at: manifest.created_at,
      generated_at: now,
      exporter: { name: EVIDENCE_EXPORTER_NAME, version: EVIDENCE_EXPORTER_VERSION },
      merkle: { algorithm: EVIDENCE_MERKLE_ALGORITHM, leaf: EVIDENCE_MERKLE_LEAF_FORMULA, root },
      entries,
      resources,
      replay: replay.section,
    },
  };
}

/**
 * encodeEvidence renders a bundle the way `readproof evidence export` does:
 * two-space indentation and a trailing newline.
 */
export function encodeEvidence(bundle: EvidenceBundle): string {
  return `${JSON.stringify(bundle, null, 2)}\n`;
}

interface ReplaySection {
  section: EvidenceReplay;
  contentByPosition: Map<number, string>;
}

async function buildReplay(
  rp: Readproof,
  target: string,
  manifest: Manifest,
  now: string,
): Promise<ReplaySection> {
  try {
    const result = await rp.replay(target);
    const contentByPosition = new Map<number, string>();
    const entries: EvidenceReplayEntry[] = result.entries.map((e) => {
      contentByPosition.set(e.position, e.content);
      return {
        position: e.position,
        match: e.match,
        expected_hash: e.recorded_hash,
        actual_hash: e.replayed_hash,
      };
    });

    const section: EvidenceReplay = {
      verified_at: now,
      all_match: entries.every((e) => e.match),
      entries,
    };
    // A short replay is a mismatch even if every entry it did return
    // matched, so say so rather than reporting all_match.
    if (result.entries.length !== manifest.entries.length) {
      section.all_match = false;
      section.error = `replay returned ${result.entries.length} of ${manifest.entries.length} manifest entries`;
    }
    return { section, contentByPosition };
  } catch (err) {
    // An un-replayable manifest is precisely the thing an auditor needs a
    // record of, so the export succeeds and records the failure.
    return {
      section: {
        verified_at: now,
        all_match: false,
        entries: [],
        error: err instanceof Error ? err.message : String(err),
      },
      contentByPosition: new Map(),
    };
  }
}

async function buildEntries(
  rp: Readproof,
  manifest: Manifest,
  contentByPosition: Map<number, string>,
  withContent: boolean,
): Promise<EvidenceEntry[]> {
  const entries: EvidenceEntry[] = [];
  for (const me of manifest.entries) {
    // A manifest entry pointing at a snapshot that no longer exists is an
    // integrity failure of the store — let it throw rather than emit
    // evidence that quietly omits what the agent saw.
    const snapshot = await rp.getSnapshot(me.snapshot_id);
    const entry: EvidenceEntry = {
      position: me.position,
      uri: me.uri,
      ...(me.ref ? { ref: me.ref } : {}),
      snapshot_id: me.snapshot_id,
      materialization_id: me.materialization_id,
      content_hash: me.content_hash,
      source_revision: snapshot.source_revision,
      observed_at: snapshot.observed_at,
      content_type: snapshot.content_type,
      bytes: snapshot.bytes,
      provenance: sortedRecord(snapshot.provenance),
    };
    if (withContent) {
      const content = contentByPosition.get(me.position);
      if (content !== undefined) {
        // The SDK's replay() hands back decoded text, so the bytes are
        // re-encoded from UTF-8 here. Readproof payloads are text (markdown,
        // JSON, YAML); a genuinely binary payload would not survive that
        // round trip, and its content_b64 would fail re-hashing — which is
        // why the Go exporter, which keeps the raw bytes, is the one to
        // use for binary sources.
        entry.content_b64 = Buffer.from(content, "utf-8").toString("base64");
      }
    }
    entries.push(entry);
  }
  return entries;
}

async function buildResources(rp: Readproof, manifest: Manifest): Promise<EvidenceResource[]> {
  const seen = new Set<string>();
  const resources: EvidenceResource[] = [];

  for (const me of manifest.entries) {
    if (seen.has(me.uri)) {
      continue;
    }
    seen.add(me.uri);

    try {
      const res = await rp.getResource(me.uri);
      const policy: EvidencePolicy = { strategy: res.policy.strategy };
      // Omitted-when-zero, matching the Go exporter's `omitempty` tags so
      // both implementations emit the same keys.
      if (res.policy.max_age_seconds) {
        policy.max_age_seconds = res.policy.max_age_seconds;
      }
      if (res.policy.pinned_snapshot_id) {
        policy.pinned_snapshot_id = res.policy.pinned_snapshot_id;
      }
      resources.push({
        uri: res.uri,
        namespace: res.namespace,
        path: res.path,
        source: toEvidenceSource(res.source),
        policy,
      });
    } catch (err) {
      if (!isNotFound(err)) {
        throw err;
      }
      // Recorded rather than fatal: a manifest stays replayable after its
      // resource is deregistered, and the evidence should say exactly that.
      resources.push(missingResource(me.uri));
    }
  }
  return resources;
}

function toEvidenceSource(source: {
  kind: SourceKind;
  filesystem?: FilesystemConfig;
  github?: GitHubConfig;
  http?: HTTPConfig;
}): EvidenceSource {
  const config: EvidenceSourceConfig = {};
  if (source.filesystem) {
    config.filesystem = { path: source.filesystem.path };
  }
  if (source.github) {
    config.github = {
      owner: source.github.owner,
      repo: source.github.repo,
      path: source.github.path,
      ref: source.github.ref,
    };
  }
  if (source.http) {
    const http: HTTPConfig = { url: source.http.url };
    if (source.http.headers) {
      http.headers = redactHeaders(source.http.headers);
    }
    config.http = http;
  }
  return { kind: source.kind, config };
}

function missingResource(uri: string): EvidenceResource {
  const parsed = /^readproof:\/\/([^/]+)\/(.+)$/.exec(uri);
  return {
    uri,
    namespace: parsed?.[1] ?? "",
    path: parsed?.[2] ?? "",
    source: { kind: "", config: {} },
    policy: { strategy: "" },
    missing: true,
  };
}

const SENSITIVE_HEADER_NAMES = new Set([
  "authorization",
  "cookie",
  "set-cookie",
  "proxy-authorization",
]);
const SENSITIVE_HEADER_SUBSTRINGS = ["token", "key", "secret", "password", "credential", "auth"];

/**
 * redactHeaders mirrors Go's internal/redact. readproofd already redacts header
 * values on the wire, so this is belt-and-braces: a bundle is an artifact
 * built to be exported, and it must never carry a credential even if it
 * was fed a response from something other than a current readproofd.
 */
function redactHeaders(headers: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const name of Object.keys(headers).sort()) {
    const lower = name.toLowerCase();
    const sensitive =
      SENSITIVE_HEADER_NAMES.has(lower) || SENSITIVE_HEADER_SUBSTRINGS.some((s) => lower.includes(s));
    out[name] = sensitive ? "[REDACTED]" : (headers[name] as string);
  }
  return out;
}

function isNotFound(err: unknown): boolean {
  if (err instanceof ReadproofError && err.status === 404) {
    return true;
  }
  return err instanceof Error && err.message.toLowerCase().includes("not found");
}

/**
 * sortedRecord copies a map with its keys in sorted order. Go's
 * encoding/json sorts map keys, so the TypeScript exporter has to sort too
 * for the two to emit identical JSON.
 */
function sortedRecord(record: Record<string, string> | undefined): Record<string, string> {
  const out: Record<string, string> = {};
  for (const key of Object.keys(record ?? {}).sort()) {
    out[key] = (record as Record<string, string>)[key] as string;
  }
  return out;
}
