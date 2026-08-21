import { type Ctx, type Manifest, type ResolveResult } from '@ctx/sdk';
/** Where a resolve landed in the session's run. */
export interface SessionMount {
    run_id: string;
    position: number;
}
/**
 * SessionRuns mirrors every model-driven resolve into a Ctx run keyed by the
 * DSH session, so "what did this session read?" is answerable without the
 * model having to drive the run lifecycle itself.
 *
 * The session id comes from `exec.agent.id` inside a tool body: `Agent.id`
 * is a `SessionId` and is documented as "the single identity shared with
 * {@link session}" (@deepseek-ai/dsh-agent, lib/types/runtime-types.d.ts).
 * A call with no agent — a direct `ctx.tools.execute(...)` from a test or a
 * plugin — has no session, and is simply not mirrored.
 */
export declare class SessionRuns {
    private readonly client;
    private readonly warn;
    private readonly states;
    /** Per-session serialization: parallel tool calls must not both open a run. */
    private readonly queues;
    constructor(client: Ctx, warn: (message: string) => void);
    /**
     * Resolve `uri` by mounting it into the session's run, so exactly one
     * fetch happens and the run records precisely the bytes the model got.
     */
    mount(sessionId: string, uri: string): Promise<{
        result: ResolveResult;
        mount: SessionMount;
    }>;
    /** The session's current run id, if one has been opened. */
    runIdFor(sessionId: string): string | undefined;
    /** Every run id this plugin owns that is still open. */
    openRunIds(): string[];
    /**
     * Commit the session's run if it is open and has entries. Returns the
     * manifest, or undefined when there was nothing to commit.
     */
    commit(sessionId: string): Promise<Manifest | undefined>;
    /**
     * Commit the session run named by `target`, if `target` is one of ours and
     * still open. This is what makes `ctx_manifest`/`ctx_diff`/`ctx_replay`/
     * `ctx_evidence_export` work on a session run mid-session: a manifest only
     * exists after a commit, and the session has not ended yet.
     */
    commitIfOwned(target: string): Promise<Manifest | undefined>;
    /** Commit every open run. Used when the plugin unloads. */
    commitAll(): Promise<void>;
    /** Drop a session's state without committing — the session is gone. */
    forget(sessionId: string): void;
    private open;
    /**
     * Run `body` after every earlier operation for this session. The chain is
     * kept per session so two sessions never wait on each other.
     */
    private serialize;
}
/**
 * A Ctx run id is opaque, but it ends up in manifests, evidence bundles, and
 * CLI arguments, so keep it to characters that survive all three unquoted.
 */
export declare function runIdBase(sessionId: string): string;
//# sourceMappingURL=session-runs.d.ts.map