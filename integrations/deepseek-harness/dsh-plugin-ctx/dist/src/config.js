import Schema from '@deepseek-ai/schemastery';
/**
 * The input type is `Partial<Config>` because every field has a default:
 * Cordis validates a row's raw `config` through this schema before `apply`
 * runs, so a patch may set as little as it likes and still produce a
 * complete `Config`.
 */
export const Config = Schema.object({
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
});
//# sourceMappingURL=config.js.map