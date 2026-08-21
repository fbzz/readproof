import Schema from '@deepseek-ai/schemastery'

/**
 * Plugin configuration. Every value two deployments could plausibly want to
 * set differently is a field here rather than a constant, per the harness
 * configuration doctrine (docs/user/develop/basic/config.md).
 */
export interface Config {
  /** Base URL of a running `readproofd`. Ignored when `spawn` is true. */
  endpoint: string
  /**
   * Bearer token for a `readproofd` started with `--api-key`. Left empty here it
   * falls back to `READPROOF_API_KEY`, so a deployment never has to write the
   * secret into a patch file that lives in version control.
   */
  apiKey: string
  /** Start a private `readproofd` child process instead of using `endpoint`. */
  spawn: boolean
  /** Executable used when `spawn` is true; resolved on PATH unless absolute. */
  readproofdPath: string
  /** `--data-dir` for the spawned `readproofd`. `~` expands to the home directory. */
  dataDir: string
  /** `--addr` for the spawned `readproofd`; also determines the endpoint used. */
  addr: string
  /**
   * Directories a filesystem-source resource may be read from, passed to the
   * spawned `readproofd` as `--filesystem-root`. A server refuses filesystem
   * sources outright when this is empty — registering one would otherwise be
   * a file-read primitive on the host — so name the directory holding the
   * documents this deployment governs, and nothing wider.
   */
  filesystemRoots: string[]
  /** Milliseconds to wait for a spawned `readproofd` to answer `/healthz`. */
  spawnTimeoutMs: number
  /**
   * Mirror every model-driven resolve into a Readproof run keyed by the DSH
   * session, so a session's reads are replayable without the model having
   * to drive `readproof_run_start` / `readproof_run_mount` itself.
   */
  sessionRuns: boolean
  /** Prefix for every registered tool name. `readproof_` yields `readproof_resolve`. */
  toolPrefix: string
  /** Contribute a short "how to use Readproof" section to the system prompt. */
  systemPromptSection: boolean
  /**
   * Cap on inline content bytes per tool result. Past it the text is cut on
   * a UTF-8 boundary and a marker naming the content hash is appended —
   * the same 1 MiB default `readproof mcp` uses (internal/mcp/content.go).
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
    .description('Base URL of a running readproofd. Ignored when spawn is true.'),
  apiKey: Schema.string()
    .default('')
    .description('Bearer token for readproofd; falls back to $READPROOF_API_KEY when empty.'),
  spawn: Schema.boolean()
    .default(false)
    .description('Start a private readproofd child process instead of using endpoint.'),
  readproofdPath: Schema.string()
    .default('readproofd')
    .description('readproofd executable used when spawn is true.'),
  dataDir: Schema.string()
    .default('~/.readproof')
    .description('Data directory for the spawned readproofd.'),
  addr: Schema.string()
    .default('127.0.0.1:18080')
    .description('Listen address for the spawned readproofd.'),
  filesystemRoots: Schema.array(Schema.string())
    .default([])
    .description('Directories a filesystem source may read from (--filesystem-root). Empty = filesystem sources refused.'),
  spawnTimeoutMs: Schema.number()
    .default(10_000)
    .description('How long to wait for a spawned readproofd to answer /healthz.'),
  sessionRuns: Schema.boolean()
    .default(true)
    .description('Record every model-driven resolve in a per-session Readproof run.'),
  toolPrefix: Schema.string()
    .default('readproof_')
    .description('Prefix for every registered tool name.'),
  systemPromptSection: Schema.boolean()
    .default(true)
    .description('Contribute a short Readproof usage section to the system prompt.'),
  maxInlineBytes: Schema.number()
    .default(1024 * 1024)
    .description('Maximum inline content bytes per tool result.'),
})
