import type { Config } from './config.js';
export interface SpawnedCtxd {
    /** Base URL the SDK client should talk to. */
    endpoint: string;
    /** Kills the child and resolves once it has actually exited. */
    stop: () => Promise<void>;
}
/**
 * Start a private `ctxd` and wait until it answers `/healthz`.
 *
 * The caller owns the returned `stop`; it is registered as a Cordis effect
 * disposer so an HMR reload or an unload of the plugin takes the child with
 * it rather than leaking a listener on the configured port.
 */
export declare function spawnCtxd(config: Config, log: (message: string) => void): Promise<SpawnedCtxd>;
/** `:8080` and `0.0.0.0:8080` are reachable locally at 127.0.0.1. */
export declare function endpointForAddr(addr: string): string;
/** Expand a leading `~` — a config file is written by a human, not a shell. */
export declare function expandHome(path: string): string;
//# sourceMappingURL=spawn.d.ts.map