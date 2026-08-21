import type { Readproof } from "./client.js";
import type { Manifest, ResolveResult } from "./types.js";

/**
 * Run is the manifest-aware resolve flow: every `mount()` resolves a
 * resource AND records it as the next ordered entry in the manifest that
 * `commit()` produces — the SDK's equivalent of the CLI's
 * `readproof run --id <run-id> <uri...>` / `readproof run mount`.
 *
 * The run starts lazily on the first `mount()` (or an explicit `start()`)
 * call, so `rp.run({ id }).mount(uri)` reads naturally without a separate
 * setup step.
 */
export class Run {
  readonly id: string;
  private readonly rp: Readproof;
  private started: Promise<void> | null = null;

  constructor(rp: Readproof, id: string) {
    this.rp = rp;
    this.id = id;
  }

  /** Starts the run. Idempotent — safe to call more than once. */
  start(): Promise<void> {
    if (!this.started) {
      this.started = this.rp._startRun(this.id);
    }
    return this.started;
  }

  /**
   * Resolves uri and stages it as the next entry in this run. Like
   * `Readproof.resolve`, uri may carry a trailing `@<tag>`; the manifest entry
   * records the bare URI plus that ref.
   */
  async mount(uri: string): Promise<ResolveResult> {
    await this.start();
    const { resolve } = await this.rp._mountRun(this.id, uri);
    return resolve;
  }

  /** Commits every mounted resource into an immutable Manifest. */
  async commit(): Promise<Manifest> {
    await this.start();
    return this.rp._commitRun(this.id);
  }
}
