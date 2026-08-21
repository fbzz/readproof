/**
 * The JSON payloads the tools return.
 *
 * These mirror internal/mcp/types.go field for field, so a model that has
 * seen Ctx through `ctx mcp` sees the same shapes here, and so anything
 * written against one surface reads the other. They are deliberately
 * separate from the SDK's wire types (sdk/typescript/src/types.ts), which
 * are shaped by ctxd's HTTP API rather than by what a model needs.
 */
import type { DiffResult, Manifest, ReplayResult, Resource, ResolveResult, Snapshot, Tag } from '@ctx/sdk';
import type { JsonValue } from '@deepseek-ai/dsh-tools';
import type { ContentPayload } from './content.js';
import type { SessionMount } from './session-runs.js';
export type SourceInfo = {
    kind: string;
    path?: string;
    owner?: string;
    repo?: string;
    ref?: string;
    url?: string;
    /** Already redacted by ctxd (internal/wire.SourceToWire) before it reaches us. */
    headers?: Record<string, string>;
};
export type PolicyInfo = {
    strategy: string;
    max_age_seconds?: number;
    pinned_snapshot_id?: string;
};
export type ResourceInfo = {
    uri: string;
    namespace: string;
    path: string;
    description: string;
    source: SourceInfo;
    policy: PolicyInfo;
    current_snapshot_id?: string;
};
export type SnapshotInfo = {
    snapshot_id: string;
    resource_uri: string;
    source_revision: string;
    content_hash: string;
    observed_at: string;
    created_at: string;
    content_type: string;
    bytes: number;
    provenance?: Record<string, string>;
    /** Tag names currently pointing at this snapshot — "which of these can I pin?". */
    tags?: string[];
};
export type ResolveOut = {
    uri: string;
    ref?: string;
    decision: string;
    snapshot_id: string;
    content_hash: string;
    source_revision: string;
    observed_at: string;
    content_type: string;
    bytes: number;
    materialization_id: string;
    provenance?: Record<string, string>;
    content?: ContentPayload;
    /**
     * Present when the plugin also recorded this read in the session's run
     * (see the `sessionRuns` config field). Not part of the MCP surface.
     */
    session_run?: SessionMount;
};
export type ManifestEntryOut = {
    position: number;
    uri: string;
    ref?: string;
    snapshot_id: string;
    materialization_id: string;
    content_hash: string;
};
export type ManifestOut = {
    manifest_id: string;
    run_id: string;
    created_at: string;
    entries: ManifestEntryOut[];
};
export type DiffEntryOut = {
    uri: string;
    status: string;
    snapshot_id_a?: string;
    snapshot_id_b?: string;
    source_revision_a?: string;
    source_revision_b?: string;
    observed_at_a?: string;
    observed_at_b?: string;
    ref_a?: string;
    ref_b?: string;
    unified_diff?: string;
};
export type DiffOut = {
    manifest_a: string;
    manifest_b: string;
    changed: number;
    added: number;
    removed: number;
    unchanged: number;
    entries: DiffEntryOut[];
};
export type ReplayEntryOut = {
    position: number;
    uri: string;
    materialization_id: string;
    recorded_hash: string;
    replayed_hash: string;
    match: boolean;
    content?: ContentPayload;
};
export type ReplayOut = {
    manifest_id: string;
    run_id: string;
    all_match: boolean;
    entries: ReplayEntryOut[];
};
export type TagInfo = {
    uri: string;
    tag: string;
    snapshot_id: string;
    updated_at: string;
    /** The exact string to read this tag by — spelling it out saves a model the syntax. */
    reference: string;
};
export declare function resourceInfo(r: Resource): ResourceInfo;
export declare function snapshotInfo(s: Snapshot, tags: string[]): SnapshotInfo;
export declare function resolveOut(r: ResolveResult, content: ContentPayload | undefined, mount: SessionMount | undefined): ResolveOut;
export declare function manifestOut(m: Manifest): ManifestOut;
export declare function diffOut(d: DiffResult): DiffOut;
export declare function replayOut(r: ReplayResult, entries: ReplayEntryOut[]): ReplayOut;
export declare function tagInfo(t: Tag): TagInfo;
/**
 * Round-trip a payload through JSON.
 *
 * The harness validates every tool's canonical value as lossless JSON and
 * persists it, so `undefined`-valued properties have to be gone before the
 * value leaves `execute`. One stringify/parse pair does that and proves the
 * value really is JSON, which no structural type can.
 */
export declare function toJson<T>(value: T): JsonValue;
//# sourceMappingURL=payloads.d.ts.map