// These types mirror internal/wire/wire.go exactly — the JSON contract
// readproofd's HTTP API speaks. Field names are kept snake_case (unconverted, as
// they appear on the wire) so this file stays a direct, unambiguous
// reflection of the server contract rather than an independently
// maintained mapping that can drift from it.

export type SourceKind = "filesystem" | "github" | "http";

export interface FilesystemConfig {
  path: string;
}

export interface GitHubConfig {
  owner: string;
  repo: string;
  path: string;
  ref: string;
}

export interface HTTPConfig {
  url: string;
  headers?: Record<string, string>;
}

export interface SourceConfig {
  kind: SourceKind;
  filesystem?: FilesystemConfig;
  github?: GitHubConfig;
  http?: HTTPConfig;
}

export type PolicyStrategy = "require_fresh" | "allow_stale" | "pinned";

export interface Policy {
  strategy: PolicyStrategy;
  max_age_seconds?: number;
  pinned_snapshot_id?: string;
}

export interface Resource {
  uri: string;
  namespace: string;
  path: string;
  source: SourceConfig;
  policy: Policy;
  current_snapshot_id?: string;
  created_at: string;
  updated_at: string;
}

export interface RegisterResourceInput {
  uri: string;
  source: SourceConfig;
  policy: Policy;
}

export interface Snapshot {
  id: string;
  resource_uri: string;
  source_revision: string;
  content_hash: string;
  observed_at: string;
  created_at: string;
  content_type: string;
  bytes: number;
  provenance: Record<string, string>;
}

export interface Materialization {
  id: string;
  snapshot_id: string;
  strategy: string;
  content_hash: string;
  bytes: number;
  created_at: string;
}

/** A named, movable pointer from a resource to one of its snapshots. */
export interface Tag {
  uri: string;
  tag: string;
  snapshot_id: string;
  updated_at: string;
}

// FreshnessStatus matches policy.Decision.String() on the Go side.
// "use_tag" is a `readproof://ns/path@tag` resolve: exactly that snapshot, no
// source fetch, policy not consulted.
export type FreshnessStatus = "fetch" | "use_current" | "use_pinned" | "use_tag";

export interface Freshness {
  status: FreshnessStatus;
  age_seconds: number;
}

export interface ResolveResourceSummary {
  uri: string;
  /** The "@<tag>" this resolve was pinned to; absent for a plain URI. */
  ref?: string;
  policy: Policy;
}

/** The public shape of a resolve — content is decoded text, not base64. */
export interface ResolveResult {
  resource: ResolveResourceSummary;
  snapshot: Snapshot;
  materialization: Materialization;
  freshness: Freshness;
  content: string;
}

export interface ManifestEntry {
  position: number;
  /** Always the bare readproof://ns/path — the tag, if any, is in `ref`. */
  uri: string;
  ref?: string;
  snapshot_id: string;
  materialization_id: string;
  content_hash: string;
}

export interface Manifest {
  manifest_id: string;
  run_id: string;
  created_at: string;
  entries: ManifestEntry[];
}

export type DiffStatus = "changed" | "added" | "removed" | "unchanged";

export interface DiffEntry {
  uri: string;
  status: DiffStatus;
  snapshot_id_a?: string;
  snapshot_id_b?: string;
  /**
   * Per-side provenance — why the resolved bytes differ. Present only for a
   * side whose manifest contains this URI; timestamps are RFC3339.
   */
  source_revision_a?: string;
  source_revision_b?: string;
  observed_at_a?: string;
  observed_at_b?: string;
  ref_a?: string;
  ref_b?: string;
  unified_diff?: string;
}

export interface DiffResult {
  manifest_a: Manifest;
  manifest_b: Manifest;
  entries: DiffEntry[];
}

/** The public shape of a replayed entry — content is decoded text. */
export interface ReplayEntry {
  position: number;
  uri: string;
  materialization_id: string;
  recorded_hash: string;
  replayed_hash: string;
  content: string;
  match: boolean;
}

export interface ReplayResult {
  manifest: Manifest;
  entries: ReplayEntry[];
}
