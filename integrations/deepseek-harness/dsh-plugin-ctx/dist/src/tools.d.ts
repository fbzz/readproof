import { type Ctx } from '@ctx/sdk';
import type { Context } from '@deepseek-ai/cordis';
import { type ToolRunContext } from '@deepseek-ai/dsh-tools';
import type { Config } from './config.js';
import type { SessionRuns } from './session-runs.js';
export interface ToolDeps {
    client: Ctx;
    config: Config;
    /** Absent when `sessionRuns` is off. */
    sessions?: SessionRuns;
}
/** The tool names this plugin registers, without the configured prefix. */
export declare const TOOL_BASE_NAMES: readonly ["resources_list", "resolve", "history", "run_start", "run_mount", "run_commit", "manifest", "diff", "replay", "tag_set", "tag_list", "tag_delete", "evidence_export"];
/**
 * Register every Ctx tool on `ctx.tools`.
 *
 * Names, descriptions, parameter documentation, and result shapes mirror
 * internal/mcp/tools.go, so the harness surface and the MCP surface are the
 * same surface. The descriptions are written for a model, not for a
 * changelog: each says what the tool is *for* and what it costs.
 *
 * `ctx.tools.register` returns a disposer that Cordis attaches to this
 * plugin's fiber, so unloading or hot-replacing the plugin unregisters
 * every tool (docs/cordis-tutorial/07-into-the-harness.md).
 */
export declare function registerTools(ctx: Context, deps: ToolDeps): void;
/**
 * The DSH session a tool call belongs to, or undefined for a call with no
 * agent behind it (a direct `ctx.tools.execute` from a plugin or a test).
 *
 * `Agent.id` is documented as "the single identity shared with
 * {@link session}" (@deepseek-ai/dsh-agent), so it is the session id.
 */
export declare function sessionIdOf(exec: ToolRunContext): string | undefined;
/** Strip a trailing `@<tag>`; mirrors resource.SplitRef in Go. */
export declare function bareUri(raw: string): string;
//# sourceMappingURL=tools.d.ts.map