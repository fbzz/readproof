/**
 * The JSON payloads the tools return.
 *
 * These mirror internal/mcp/types.go field for field, so a model that has
 * seen Ctx through `ctx mcp` sees the same shapes here, and so anything
 * written against one surface reads the other. They are deliberately
 * separate from the SDK's wire types (sdk/typescript/src/types.ts), which
 * are shaped by ctxd's HTTP API rather than by what a model needs.
 */

import type {
  DiffResult,
  Manifest,
  ReplayResult,
  Resource,
  ResolveResult,
  Snapshot,
  Tag,
} from '@ctx/sdk'
import type { JsonValue } from '@deepseek-ai/dsh-tools'

import type { ContentPayload } from './content.js'
import type { SessionMount } from './session-runs.js'

export type SourceInfo = {
  kind: string
  path?: string
  owner?: string
  repo?: string
  ref?: string
  url?: string
  /** Already redacted by ctxd (internal/wire.SourceToWire) before it reaches us. */
  headers?: Record<string, string>
}

export type PolicyInfo = {
  strategy: string
  max_age_seconds?: number
  pinned_snapshot_id?: string
}

export type ResourceInfo = {
  uri: string
  namespace: string
  path: string
  description: string
  source: SourceInfo
  policy: PolicyInfo
  current_snapshot_id?: string
}

export type SnapshotInfo = {
  snapshot_id: string
  resource_uri: string
  source_revision: string
  content_hash: string
  observed_at: string
  created_at: string
  content_type: string
  bytes: number
  provenance?: Record<string, string>
  /** Tag names currently pointing at this snapshot — "which of these can I pin?". */
  tags?: string[]
}

export type ResolveOut = {
  uri: string
  ref?: string
  decision: string
  snapshot_id: string
  content_hash: string
  source_revision: string
  observed_at: string
  content_type: string
  bytes: number
  materialization_id: string
  provenance?: Record<string, string>
  content?: ContentPayload
  /**
   * Present when the plugin also recorded this read in the session's run
   * (see the `sessionRuns` config field). Not part of the MCP surface.
   */
  session_run?: SessionMount
}

export type ManifestEntryOut = {
  position: number
  uri: string
  ref?: string
  snapshot_id: string
  materialization_id: string
  content_hash: string
}

export type ManifestOut = {
  manifest_id: string
  run_id: string
  created_at: string
  entries: ManifestEntryOut[]
}

export type DiffEntryOut = {
  uri: string
  status: string
  snapshot_id_a?: string
  snapshot_id_b?: string
  source_revision_a?: string
  source_revision_b?: string
  observed_at_a?: string
  observed_at_b?: string
  ref_a?: string
  ref_b?: string
  unified_diff?: string
}

export type DiffOut = {
  manifest_a: string
  manifest_b: string
  changed: number
  added: number
  removed: number
  unchanged: number
  entries: DiffEntryOut[]
}

export type ReplayEntryOut = {
  position: number
  uri: string
  materialization_id: string
  recorded_hash: string
  replayed_hash: string
  match: boolean
  content?: ContentPayload
}

export type ReplayOut = {
  manifest_id: string
  run_id: string
  all_match: boolean
  entries: ReplayEntryOut[]
}

export type TagInfo = {
  uri: string
  tag: string
  snapshot_id: string
  updated_at: string
  /** The exact string to read this tag by — spelling it out saves a model the syntax. */
  reference: string
}

export function resourceInfo(r: Resource): ResourceInfo {
  return {
    uri: r.uri,
    namespace: r.namespace,
    path: r.path,
    description: describeResource(r),
    source: sourceInfo(r),
    policy: policyInfo(r),
    ...(r.current_snapshot_id ? { current_snapshot_id: r.current_snapshot_id } : {}),
  }
}

function sourceInfo(r: Resource): SourceInfo {
  const src = r.source
  if (src.filesystem) return { kind: src.kind, path: src.filesystem.path }
  if (src.github) {
    return {
      kind: src.kind,
      owner: src.github.owner,
      repo: src.github.repo,
      path: src.github.path,
      ref: src.github.ref,
    }
  }
  if (src.http) {
    return {
      kind: src.kind,
      url: src.http.url,
      ...(src.http.headers ? { headers: src.http.headers } : {}),
    }
  }
  return { kind: src.kind }
}

function policyInfo(r: Resource): PolicyInfo {
  return {
    strategy: r.policy.strategy,
    ...(r.policy.max_age_seconds ? { max_age_seconds: r.policy.max_age_seconds } : {}),
    ...(r.policy.pinned_snapshot_id ? { pinned_snapshot_id: r.policy.pinned_snapshot_id } : {}),
  }
}

/** Mirrors describeResource in internal/mcp/resources.go. */
function describeResource(r: Resource): string {
  const locator = sourceLocator(r)
  const base = `${r.source.kind} · ${policyLabel(r)}`
  return locator === '' ? base : `${base} — ${locator}`
}

function policyLabel(r: Resource): string {
  const p = r.policy
  if (p.strategy === 'allow_stale') {
    return p.max_age_seconds ? `allow_stale(max_age=${formatDuration(p.max_age_seconds)})` : 'allow_stale'
  }
  if (p.strategy === 'pinned') return `pinned(${p.pinned_snapshot_id ?? ''})`
  return p.strategy
}

/**
 * Render seconds the way Go's time.Duration.String() does, since that is
 * what the Go side puts in this label — "1h0m0s", "5m0s", "30s".
 */
function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = seconds % 60
  return h > 0 ? `${h}h${m}m${s}s` : `${m}m${s}s`
}

function sourceLocator(r: Resource): string {
  const src = r.source
  if (src.filesystem) return src.filesystem.path
  if (src.github) {
    const gh = src.github
    const loc = `${gh.owner}/${gh.repo}:${gh.path}`
    return gh.ref ? `${loc}@${gh.ref}` : loc
  }
  if (src.http) return src.http.url
  return ''
}

export function snapshotInfo(s: Snapshot, tags: string[]): SnapshotInfo {
  return {
    snapshot_id: s.id,
    resource_uri: s.resource_uri,
    source_revision: s.source_revision,
    content_hash: s.content_hash,
    observed_at: s.observed_at,
    created_at: s.created_at,
    content_type: s.content_type,
    bytes: s.bytes,
    ...(s.provenance && Object.keys(s.provenance).length > 0 ? { provenance: s.provenance } : {}),
    ...(tags.length > 0 ? { tags } : {}),
  }
}

export function resolveOut(
  r: ResolveResult,
  content: ContentPayload | undefined,
  mount: SessionMount | undefined,
): ResolveOut {
  return {
    uri: r.snapshot.resource_uri,
    ...(r.resource.ref ? { ref: r.resource.ref } : {}),
    decision: r.freshness.status,
    snapshot_id: r.snapshot.id,
    content_hash: r.snapshot.content_hash,
    source_revision: r.snapshot.source_revision,
    observed_at: r.snapshot.observed_at,
    content_type: r.snapshot.content_type,
    bytes: r.snapshot.bytes,
    materialization_id: r.materialization.id,
    ...(r.snapshot.provenance && Object.keys(r.snapshot.provenance).length > 0
      ? { provenance: r.snapshot.provenance }
      : {}),
    ...(content ? { content } : {}),
    ...(mount ? { session_run: mount } : {}),
  }
}

export function manifestOut(m: Manifest): ManifestOut {
  return {
    manifest_id: m.manifest_id,
    run_id: m.run_id,
    created_at: m.created_at,
    entries: m.entries.map((e) => ({
      position: e.position,
      uri: e.uri,
      ...(e.ref ? { ref: e.ref } : {}),
      snapshot_id: e.snapshot_id,
      materialization_id: e.materialization_id,
      content_hash: e.content_hash,
    })),
  }
}

export function diffOut(d: DiffResult): DiffOut {
  const count = (status: string): number => d.entries.filter((e) => e.status === status).length
  return {
    manifest_a: d.manifest_a.manifest_id,
    manifest_b: d.manifest_b.manifest_id,
    changed: count('changed'),
    added: count('added'),
    removed: count('removed'),
    unchanged: count('unchanged'),
    // Unchanged URIs stay listed (without diff text) so a caller sees the
    // full comparison, not just what moved.
    entries: d.entries.map((e) => ({
      uri: e.uri,
      status: e.status,
      ...(e.snapshot_id_a ? { snapshot_id_a: e.snapshot_id_a } : {}),
      ...(e.snapshot_id_b ? { snapshot_id_b: e.snapshot_id_b } : {}),
      ...(e.source_revision_a ? { source_revision_a: e.source_revision_a } : {}),
      ...(e.source_revision_b ? { source_revision_b: e.source_revision_b } : {}),
      ...(e.observed_at_a ? { observed_at_a: e.observed_at_a } : {}),
      ...(e.observed_at_b ? { observed_at_b: e.observed_at_b } : {}),
      ...(e.ref_a ? { ref_a: e.ref_a } : {}),
      ...(e.ref_b ? { ref_b: e.ref_b } : {}),
      ...(e.unified_diff ? { unified_diff: e.unified_diff } : {}),
    })),
  }
}

export function replayOut(r: ReplayResult, entries: ReplayEntryOut[]): ReplayOut {
  return {
    manifest_id: r.manifest.manifest_id,
    run_id: r.manifest.run_id,
    all_match: r.entries.every((e) => e.match),
    entries,
  }
}

export function tagInfo(t: Tag): TagInfo {
  return {
    uri: t.uri,
    tag: t.tag,
    snapshot_id: t.snapshot_id,
    updated_at: t.updated_at,
    reference: `${t.uri}@${t.tag}`,
  }
}

/**
 * Round-trip a payload through JSON.
 *
 * The harness validates every tool's canonical value as lossless JSON and
 * persists it, so `undefined`-valued properties have to be gone before the
 * value leaves `execute`. One stringify/parse pair does that and proves the
 * value really is JSON, which no structural type can.
 */
export function toJson<T>(value: T): JsonValue {
  return JSON.parse(JSON.stringify(value)) as JsonValue
}
