import type { Ctx, Manifest, ResolveResult } from '@ctx/sdk'

/**
 * Thin wrappers over the three run calls the SDK marks `@internal`.
 *
 * `Ctx.run({ id })` returns a `Run` whose `mount()` drops the position the
 * server assigned, and whose `start()` caches its promise (including a
 * rejection) — but position is load-bearing in Ctx, since mount order is
 * committed to by the manifest and can change what a model concludes, and
 * this plugin has to react to a *failed* start by choosing another run id.
 *
 * These three methods are ordinary public methods on `Ctx`; the `@internal`
 * tag marks them as "used by Run", not as unstable. Confining the calls to
 * this file keeps that dependence in one place and visible.
 */

export function startRun(client: Ctx, runId: string): Promise<void> {
  return client._startRun(runId)
}

export function mountRun(
  client: Ctx,
  runId: string,
  uri: string,
): Promise<{ position: number; resolve: ResolveResult }> {
  return client._mountRun(runId, uri)
}

export function commitRun(client: Ctx, runId: string): Promise<Manifest> {
  return client._commitRun(runId)
}
