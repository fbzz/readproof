/**
 * Test fixture: a real `ctxd`, built from this repository, with the
 * refund-agent policy registered, plus a real Cordis app with the harness
 * tool registry and this plugin mounted.
 *
 * Nothing here is mocked. The point of these tests is that the plugin talks
 * to the actual server and the actual harness; a mock would only prove the
 * plugin agrees with itself.
 */
import { Ctx } from '@ctx/sdk';
import { Context } from '@deepseek-ai/cordis';
import type { Agent } from '@deepseek-ai/dsh-agent';
import { type JsonValue, type ToolExecutionResult } from '@deepseek-ai/dsh-tools';
import type { Config } from '../src/config.js';
export declare const DEMO_URI = "ctx://demo/policies/refunds";
export interface Fixture {
    endpoint: string;
    /** Path of the file the demo resource resolves from; edit it to change the source. */
    policyPath: string;
    /** The `ctx` CLI built from this repository. */
    ctxBin: string;
    /** The `ctxd` binary built from this repository. */
    ctxdBin: string;
    /** A scratch directory that is removed on `stop()`. */
    tmpDir: string;
    client: Ctx;
    stop: () => Promise<void>;
}
/** Walk up from this file until the Ctx repository root (the one with go.mod). */
export declare function repoRoot(): string;
/**
 * Build `ctxd` and `ctx`, start `ctxd` on a free port over a fresh data
 * directory, and register the refund-agent policy against a copy of the
 * fixture (a copy, so a test can edit it without dirtying the repository).
 */
export declare function startFixture(): Promise<Fixture>;
export interface App {
    ctx: Context;
    /** Call one tool the way the agent loop would, optionally on a session's behalf. */
    call: (name: string, args: Record<string, JsonValue>, agent?: Agent) => Promise<ToolExecutionResult>;
    /** The canonical JSON value of a successful call; throws with the tool's message on failure. */
    value: (name: string, args: Record<string, JsonValue>, agent?: Agent) => Promise<JsonValue>;
    toolNames: () => string[];
    stop: () => Promise<void>;
}
/**
 * Compose the smallest app that can execute a tool: the system prompt
 * registry (which `@deepseek-ai/dsh-tools` injects), the tool registry, and
 * this plugin. No model and no agent loop are involved — `ctx.tools.execute`
 * drives the same pipeline the loop drives.
 */
export declare function startApp(config: Partial<Config>): Promise<App>;
/**
 * A stand-in for the live agent the loop would pass.
 *
 * On the native execution path the registry treats the agent as an opaque
 * scope key (a WeakMap key in `@deepseek-ai/dsh-scope`) and this plugin reads
 * only `agent.id`, so an object with an id is enough. Building a real Agent
 * would drag in the agent loop, an LLM adapter, and a session store — none of
 * which this plugin touches.
 */
export declare function fakeAgent(id: string): Agent;
/** Join a tool result's text blocks — what the model would read. */
export declare function renderContent(result: ToolExecutionResult): string;
/** Read a property off a tool's canonical value without casting. */
export declare function field(value: JsonValue | undefined, key: string): JsonValue | undefined;
export declare function str(value: JsonValue | undefined): string;
export declare function list(value: JsonValue | undefined): JsonValue[];
export declare function freePort(): Promise<number>;
//# sourceMappingURL=support.d.ts.map