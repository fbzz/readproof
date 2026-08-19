import type { Ctx } from "./client.js";
import type { Manifest, ResolveResult } from "./types.js";

/**
 * Run is the manifest-aware resolve flow: every `mount()` resolves a
 * resource AND records it as the next ordered entry in the manifest that
 * `commit()` produces — the SDK's equivalent of the CLI's
 * `ctx run --id <run-id> <uri...>` / `ctx run mount`.
 *
 * The run starts lazily on the first `mount()` (or an explicit `start()`)
 * call, so `ctx.run({ id }).mount(uri)` reads naturally without a separate
 * setup step.
 */
export class Run {
  readonly id: string;
  private readonly ctx: Ctx;
  private started: Promise<void> | null = null;

  constructor(ctx: Ctx, id: string) {
    this.ctx = ctx;
    this.id = id;
  }

  /** Starts the run. Idempotent — safe to call more than once. */
  start(): Promise<void> {
    if (!this.started) {
      this.started = this.ctx._startRun(this.id);
    }
    return this.started;
  }

  /** Resolves uri and stages it as the next entry in this run. */
  async mount(uri: string): Promise<ResolveResult> {
    await this.start();
    const { resolve } = await this.ctx._mountRun(this.id, uri);
    return resolve;
  }

  /** Commits every mounted resource into an immutable Manifest. */
  async commit(): Promise<Manifest> {
    await this.start();
    return this.ctx._commitRun(this.id);
  }
}
