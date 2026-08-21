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

import { Ctx } from '@ctx/sdk'
import type { Context } from '@deepseek-ai/cordis'
// Declaration merges: `agent/disposed` and `session/disposed` on
// `Context.Events`, and `ctx.tools` itself. Type-only, so none of these is a
// runtime dependency of this module — the harness supplies them.
import type {} from '@deepseek-ai/dsh-agent'
import type {} from '@deepseek-ai/dsh-session'
import type {} from '@deepseek-ai/dsh-tools'

import { Config } from './config.js'
import { SessionRuns } from './session-runs.js'
import { spawnCtxd } from './spawn.js'
import { registerTools } from './tools.js'

export { Config }
export type { Config as ConfigType } from './config.js'
export { TOOL_BASE_NAMES } from './tools.js'

export const name = 'ctx'

/**
 * Only `tools` is required. `systemPrompt` is read opportunistically with
 * `ctx.get` instead of being injected: injecting it would hold this plugin
 * — and every Ctx tool — hostage to a service it merely decorates. (The
 * same optional-backend idiom `ToolRuntime` uses for `approval`.)
 */
export const inject = ['tools']

export async function apply(ctx: Context, config: Config): Promise<() => Promise<void>> {
  const log = ctx.logger('ctx')

  const spawned = config.spawn ? await spawnCtxd(config, (message) => log.info(message)) : undefined
  const endpoint = spawned?.endpoint ?? config.endpoint
  // An API key belongs in the environment rather than in a patch file that
  // is checked in, so the env var wins nothing but fills a blank.
  const apiKey = config.apiKey !== '' ? config.apiKey : (process.env['CTX_API_KEY'] ?? '')
  const client = new Ctx({ endpoint, ...(apiKey !== '' ? { apiKey } : {}) })

  const sessions = config.sessionRuns
    ? new SessionRuns(client, (message) => log.warn(message))
    : undefined

  registerTools(ctx, { client, config, ...(sessions ? { sessions } : {}) })

  if (sessions) {
    // Commit a session's run when the session ends, so the manifest exists
    // without anyone having to ask for it. Both events are observed because
    // which one fires first depends on the composition: `agent/disposed` is
    // emitted by the agent registry, `session/disposed` by the session
    // store, and a deployment may mount either without the other. Committing
    // is idempotent in SessionRuns, so seeing both is harmless.
    ctx.on('agent/disposed', (payload) => {
      void commit(payload.agent.id)
    })
    ctx.on('session/disposed', (session) => {
      void commit(session.id)
    })
  }

  async function commit(sessionId: string): Promise<void> {
    if (!sessions) return
    try {
      const manifest = await sessions.commit(sessionId)
      if (manifest) log.info(`session ${sessionId} committed as manifest ${manifest.manifest_id}`)
    } catch (err) {
      log.warn(`could not commit the run for session ${sessionId}: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      sessions.forget(sessionId)
    }
  }

  if (config.systemPromptSection) {
    // Opportunistic: a composition without a system prompt (a headless tool
    // harness, a test) still gets the tools, just no prose about them.
    const systemPrompt = ctx.get('systemPrompt')
    if (systemPrompt) {
      systemPrompt.section({
        name: 'ctx',
        // Tool guidance is 100-199 by the harness's ordering convention.
        order: 150,
        text: promptSection(config.toolPrefix, config.sessionRuns),
      })
    } else {
      log.debug('no systemPrompt service; relying on tool descriptions alone')
    }
  }

  log.info(
    `Ctx tools registered against ${endpoint}` +
      (sessions ? ' (session runs on)' : ' (session runs off)'),
  )

  // One disposer, ordered: open runs are committed while ctxd is still up,
  // and only then is a spawned child taken down.
  return async () => {
    if (sessions) await sessions.commitAll()
    if (spawned) await spawned.stop()
  }
}

/** The prose a model reads before it has called anything. */
function promptSection(prefix: string, sessionRuns: boolean): string {
  const t = (base: string): string => `${prefix}${base}`
  const lines = [
    '## Ctx (governed documents)',
    '',
    `Policies, specs, and runbooks live in Ctx under \`ctx://<namespace>/<path>\`. Read them with \`${t('resolve')}\` rather than guessing or reading files directly: what comes back carries the snapshot id, content hash, and source revision that identify exactly which bytes you saw.`,
    '',
    `- \`${t('resources_list')}\` — what you are allowed to read.`,
    `- \`ctx://ns/path@tag\` — read one exact pinned snapshot, with no fetch and no freshness check.`,
    `- \`${t('run_start')}\` → \`${t('run_mount')}\` → \`${t('run_commit')}\` — when the work should be auditable. The commit returns a manifest id; cite it in what you produce.`,
    `- \`${t('diff')}\`, \`${t('replay')}\`, \`${t('evidence_export')}\` — take a manifest id (or a run id) and explain, reproduce, or attest to what a run read.`,
  ]
  if (sessionRuns) {
    lines.push(
      '',
      `Every \`${t('resolve')}\` in this session is already being recorded in a run of its own; the result tells you which. You only need \`${t('run_start')}\` for a run you want to scope yourself.`,
    )
  }
  return lines.join('\n')
}
