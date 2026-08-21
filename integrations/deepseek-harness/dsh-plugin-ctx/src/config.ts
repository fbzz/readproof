import Schema from '@deepseek-ai/schemastery'

/**
 * Plugin configuration. Every value two deployments could plausibly want to
 * set differently is a field here rather than a constant, per the harness
 * configuration doctrine (docs/user/develop/basic/config.md).
 */
export interface Config {
  /** Base URL of a running `ctxd`. Ignored when `spawn` is true. */
  endpoint: string
  /**
   * Bearer token for a `ctxd` started with `--api-key`. Left empty here it
   * falls back to `CTX_API_KEY`, so a deployment never has to write the
   * secret into a patch file that lives in version control.
   */
  apiKey: string
  /** Start a private `ctxd` child process instead of using `endpoint`. */
  spawn: boolean
  /** Executable used when `spawn` is true; resolved on PATH unless absolute. */
  ctxdPath: string
  /** `--data-dir` for the spawned `ctxd`. `~` expands to the home directory. */
  dataDir: string
  /** `--addr` for the spawned `ctxd`; also determines the endpoint used. */
  addr: string
  /** Milliseconds to wait for a spawned `ctxd` to answer `/healthz`. */
  spawnTimeoutMs: number
  /**
   * Mirror every model-driven resolve into a Ctx run keyed by the DSH
   * session, so a session's reads are replayable without the model having
   * to drive `ctx_run_start` / `ctx_run_mount` itself.
   */
  sessionRuns: boolean
  /** Prefix for every registered tool name. `ctx_` yields `ctx_resolve`. */
  toolPrefix: string
  /** Contribute a short "how to use Ctx" section to the system prompt. */
  systemPromptSection: boolean
  /**
   * Cap on inline content bytes per tool result. Past it the text is cut on
   * a UTF-8 boundary and a marker naming the content hash is appended —
   * the same 1 MiB default `ctx mcp` uses (internal/mcp/content.go).
   */
  maxInlineBytes: number
}

/**
 * The input type is `Partial<Config>` because every field has a default:
 * Cordis validates a row's raw `config` through this schema before `apply`
 * runs, so a patch may set as little as it likes and still produce a
 * complete `Config`.
 */
export const Config: Schema<Partial<Config>, Config> = Schema.object({
  endpoint: Schema.string()
    .default('http://127.0.0.1:8080')
    .description('Base URL of a running ctxd. Ignored when spawn is true.'),
  apiKey: Schema.string()
    .default('')
    .description('Bearer token for ctxd; falls back to $CTX_API_KEY when empty.'),
  spawn: Schema.boolean()
    .default(false)
    .description('Start a private ctxd child process instead of using endpoint.'),
  ctxdPath: Schema.string()
    .default('ctxd')
    .description('ctxd executable used when spawn is true.'),
  dataDir: Schema.string()
    .default('~/.ctx')
    .description('Data directory for the spawned ctxd.'),
  addr: Schema.string()
    .default('127.0.0.1:18080')
    .description('Listen address for the spawned ctxd.'),
  spawnTimeoutMs: Schema.number()
    .default(10_000)
    .description('How long to wait for a spawned ctxd to answer /healthz.'),
  sessionRuns: Schema.boolean()
    .default(true)
    .description('Record every model-driven resolve in a per-session Ctx run.'),
  toolPrefix: Schema.string()
    .default('ctx_')
    .description('Prefix for every registered tool name.'),
  systemPromptSection: Schema.boolean()
    .default(true)
    .description('Contribute a short Ctx usage section to the system prompt.'),
  maxInlineBytes: Schema.number()
    .default(1024 * 1024)
    .description('Maximum inline content bytes per tool result.'),
})
