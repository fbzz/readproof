/**
 * dsh-plugin-ctx — Ctx as a DeepSeek Harness plugin.
 *
 * Ctx gives the documents an agent reads a stable `ctx://` identity, a
 * freshness policy, content-addressed snapshots, per-run manifests, diff,
 * byte-exact replay, and evidence bundles. This plugin registers that whole
 * surface as model-callable tools, mirroring the MCP server in
 * internal/mcp so the two surfaces cannot drift.
 *
 * @module dsh-plugin-ctx
 */
import type { Context } from '@deepseek-ai/cordis';
import { Config } from './config.js';
export { Config };
export type { Config as ConfigType } from './config.js';
export { TOOL_BASE_NAMES } from './tools.js';
export declare const name = "ctx";
/**
 * Only `tools` is required. `systemPrompt` is read opportunistically with
 * `ctx.get` instead of being injected: injecting it would hold this plugin
 * — and every Ctx tool — hostage to a service it merely decorates. (The
 * same optional-backend idiom `ToolRuntime` uses for `approval`.)
 */
export declare const inject: string[];
export declare function apply(ctx: Context, config: Config): Promise<() => Promise<void>>;
//# sourceMappingURL=index.d.ts.map