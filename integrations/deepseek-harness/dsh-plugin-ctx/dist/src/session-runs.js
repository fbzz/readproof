import { CtxError } from '@ctx/sdk';
import { commitRun, mountRun, startRun } from './ctx-client.js';
/** How many run ids to try before giving up on finding an unused one. */
const MAX_EPOCH_PROBE = 16;
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
export class SessionRuns {
    client;
    warn;
    states = new Map();
    /** Per-session serialization: parallel tool calls must not both open a run. */
    queues = new Map();
    constructor(client, warn) {
        this.client = client;
        this.warn = warn;
    }
    /**
     * Resolve `uri` by mounting it into the session's run, so exactly one
     * fetch happens and the run records precisely the bytes the model got.
     */
    async mount(sessionId, uri) {
        return this.serialize(sessionId, async () => {
            const state = await this.open(sessionId);
            const { position, resolve } = await mountRun(this.client, state.runId, uri);
            state.entries = position + 1;
            return { result: resolve, mount: { run_id: state.runId, position } };
        });
    }
    /** The session's current run id, if one has been opened. */
    runIdFor(sessionId) {
        return this.states.get(sessionId)?.runId;
    }
    /** Every run id this plugin owns that is still open. */
    openRunIds() {
        return [...this.states.values()].filter((s) => s.open).map((s) => s.runId);
    }
    /**
     * Commit the session's run if it is open and has entries. Returns the
     * manifest, or undefined when there was nothing to commit.
     */
    async commit(sessionId) {
        return this.serialize(sessionId, async () => {
            const state = this.states.get(sessionId);
            if (!state || !state.open)
                return undefined;
            state.open = false;
            if (state.entries === 0)
                return undefined;
            return commitRun(this.client, state.runId);
        });
    }
    /**
     * Commit the session run named by `target`, if `target` is one of ours and
     * still open. This is what makes `ctx_manifest`/`ctx_diff`/`ctx_replay`/
     * `ctx_evidence_export` work on a session run mid-session: a manifest only
     * exists after a commit, and the session has not ended yet.
     */
    async commitIfOwned(target) {
        for (const [sessionId, state] of this.states) {
            if (state.runId === target && state.open)
                return this.commit(sessionId);
        }
        return undefined;
    }
    /** Commit every open run. Used when the plugin unloads. */
    async commitAll() {
        for (const sessionId of [...this.states.keys()]) {
            try {
                await this.commit(sessionId);
            }
            catch (err) {
                this.warn(`could not commit the run for session ${sessionId}: ${describe(err)}`);
            }
        }
    }
    /** Drop a session's state without committing — the session is gone. */
    forget(sessionId) {
        this.states.delete(sessionId);
        this.queues.delete(sessionId);
    }
    async open(sessionId) {
        const existing = this.states.get(sessionId);
        if (existing?.open)
            return existing;
        const base = runIdBase(sessionId);
        const from = existing ? existing.epoch + 1 : 1;
        for (let epoch = from; epoch < from + MAX_EPOCH_PROBE; epoch++) {
            const runId = epoch === 1 ? base : `${base}-${epoch}`;
            try {
                await startRun(this.client, runId);
            }
            catch (err) {
                // ctxd answers 500 for a run id that already exists (the store's
                // uniqueness constraint). Anything else — unreachable server, bad
                // API key — is not a name collision and must surface.
                if (err instanceof CtxError && err.status === 500)
                    continue;
                throw err;
            }
            const state = { runId, epoch, entries: 0, open: true };
            this.states.set(sessionId, state);
            return state;
        }
        throw new Error(`ctx: no unused run id after ${MAX_EPOCH_PROBE} attempts starting from ${base}`);
    }
    /**
     * Run `body` after every earlier operation for this session. The chain is
     * kept per session so two sessions never wait on each other.
     */
    serialize(sessionId, body) {
        const previous = this.queues.get(sessionId) ?? Promise.resolve();
        // Failures are the caller's to handle; the chain itself must survive one
        // so a single failed mount does not poison the session.
        const next = previous.then(body, body);
        this.queues.set(sessionId, next.catch(() => undefined));
        return next;
    }
}
/**
 * A Ctx run id is opaque, but it ends up in manifests, evidence bundles, and
 * CLI arguments, so keep it to characters that survive all three unquoted.
 */
export function runIdBase(sessionId) {
    const safe = sessionId.replace(/[^A-Za-z0-9._-]/g, '-').slice(0, 96);
    return `dsh-${safe === '' ? 'session' : safe}`;
}
function describe(err) {
    return err instanceof Error ? err.message : String(err);
}
//# sourceMappingURL=session-runs.js.map