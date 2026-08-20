import { CtxError } from "./errors.js";
import { Run } from "./run.js";
import type {
  DiffResult,
  Manifest,
  RegisterResourceInput,
  ReplayEntry,
  ReplayResult,
  Resource,
  ResolveResult,
  Snapshot,
  Tag,
} from "./types.js";

export interface CtxOptions {
  /** Base URL of a running ctxd, e.g. "http://localhost:8080". */
  endpoint: string;
  /** Sent as "Authorization: Bearer <apiKey>" if the server has --api-key set. */
  apiKey?: string;
  /** Override fetch (e.g. for testing). Defaults to the global fetch. */
  fetch?: typeof fetch;
}

export interface RunOptions {
  /** Run id — must be unique per run against a given ctxd instance. */
  id: string;
}

// Wire shapes with base64-encoded content, decoded before being handed to
// callers — see decodeResolveResponse/decodeReplayResponse below.
interface RawResolveResponse extends Omit<ResolveResult, "content"> {
  content: string;
}
interface RawReplayEntry extends Omit<ReplayEntry, "content"> {
  content: string;
}
interface RawReplayResult extends Omit<ReplayResult, "entries"> {
  entries: RawReplayEntry[];
}
interface RunMountResponseRaw {
  position: number;
  resolve: RawResolveResponse;
}

function decodeBase64(value: string): string {
  return Buffer.from(value, "base64").toString("utf-8");
}

function decodeResolveResponse(raw: RawResolveResponse): ResolveResult {
  return { ...raw, content: decodeBase64(raw.content) };
}

function decodeReplayResult(raw: RawReplayResult): ReplayResult {
  return {
    ...raw,
    entries: raw.entries.map((e) => ({ ...e, content: decodeBase64(e.content) })),
  };
}

function isErrorResponse(v: unknown): v is { error: string } {
  return typeof v === "object" && v !== null && typeof (v as { error?: unknown }).error === "string";
}

/**
 * Ctx is the TypeScript SDK client for a running ctxd. Every method maps
 * 1:1 to one of ctxd's HTTP API endpoints.
 *
 * ```ts
 * const ctx = new Ctx({ endpoint: "http://localhost:8080" });
 * const policy = await ctx.resolve("ctx://acme/policies/refunds");
 * ```
 */
export class Ctx {
  private readonly endpoint: string;
  private readonly apiKey?: string;
  private readonly fetchImpl: typeof fetch;

  constructor(options: CtxOptions) {
    this.endpoint = options.endpoint.replace(/\/+$/, "");
    this.apiKey = options.apiKey;
    this.fetchImpl = options.fetch ?? fetch;
  }

  /**
   * Resolve a context resource to its current content and metadata.
   *
   * `uri` may carry a trailing `@<tag>` (`ctx://acme/policies/refunds@prod`),
   * which delivers exactly that tagged snapshot: no source fetch, and the
   * resource's freshness policy is not consulted
   * (`freshness.status === "use_tag"`).
   */
  async resolve(uri: string): Promise<ResolveResult> {
    const raw = await this.request<RawResolveResponse>("POST", "/v1/resolve", { uri });
    return decodeResolveResponse(raw);
  }

  /**
   * Point `tag` at `snapshotId`, creating the tag or moving an existing one.
   * The snapshot must belong to `uri`.
   */
  async setTag(uri: string, tag: string, snapshotId: string): Promise<Tag> {
    return this.request<Tag>("PUT", "/v1/tags", { uri, tag, snapshot_id: snapshotId });
  }

  /** A resource's tags, sorted by name. */
  async listTags(uri: string): Promise<Tag[]> {
    const res = await this.request<{ tags: Tag[] }>("GET", `/v1/tags?uri=${encodeURIComponent(uri)}`);
    return res.tags;
  }

  /** Delete a tag. The snapshot it pointed at is untouched. */
  async deleteTag(uri: string, tag: string): Promise<void> {
    await this.request<void>("DELETE", `/v1/tags?uri=${encodeURIComponent(uri)}&tag=${encodeURIComponent(tag)}`);
  }

  async registerResource(input: RegisterResourceInput): Promise<Resource> {
    return this.request<Resource>("POST", "/v1/resources", input);
  }

  async listResources(): Promise<Resource[]> {
    const res = await this.request<{ resources: Resource[] }>("GET", "/v1/resources");
    return res.resources;
  }

  async getResource(uri: string): Promise<Resource> {
    return this.request<Resource>("GET", `/v1/resources/get?uri=${encodeURIComponent(uri)}`);
  }

  async getSnapshot(id: string): Promise<Snapshot> {
    return this.request<Snapshot>("GET", `/v1/snapshots?id=${encodeURIComponent(id)}`);
  }

  async history(uri: string): Promise<Snapshot[]> {
    const res = await this.request<{ snapshots: Snapshot[] }>("GET", `/v1/resources/history?uri=${encodeURIComponent(uri)}`);
    return res.snapshots;
  }

  async getManifest(manifestOrRunId: string): Promise<Manifest> {
    return this.request<Manifest>("GET", `/v1/manifests?target=${encodeURIComponent(manifestOrRunId)}`);
  }

  async diff(targetA: string, targetB: string): Promise<DiffResult> {
    return this.request<DiffResult>(
      "GET",
      `/v1/diff?a=${encodeURIComponent(targetA)}&b=${encodeURIComponent(targetB)}`,
    );
  }

  async replay(manifestOrRunId: string): Promise<ReplayResult> {
    const raw = await this.request<RawReplayResult>("GET", `/v1/replay?target=${encodeURIComponent(manifestOrRunId)}`);
    return decodeReplayResult(raw);
  }

  /**
   * Begin a context run: `ctx.run({ id }).mount(uri)...commit()`. The run
   * starts lazily on the first `mount()` call — every resolved resource
   * becomes an ordered entry in the manifest produced by `commit()`.
   */
  run(options: RunOptions): Run {
    return new Run(this, options.id);
  }

  /** @internal used by Run. */
  async _startRun(runId: string): Promise<void> {
    await this.request<void>("POST", "/v1/runs", { run_id: runId });
  }

  /** @internal used by Run. */
  async _mountRun(runId: string, uri: string): Promise<{ position: number; resolve: ResolveResult }> {
    const raw = await this.request<RunMountResponseRaw>("POST", "/v1/runs/mount", { run_id: runId, uri });
    return { position: raw.position, resolve: decodeResolveResponse(raw.resolve) };
  }

  /** @internal used by Run. */
  async _commitRun(runId: string): Promise<Manifest> {
    return this.request<Manifest>("POST", "/v1/runs/commit", { run_id: runId });
  }

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = {};
    if (body !== undefined) {
      headers["Content-Type"] = "application/json";
    }
    if (this.apiKey) {
      headers["Authorization"] = `Bearer ${this.apiKey}`;
    }

    const res = await this.fetchImpl(`${this.endpoint}${path}`, {
      method,
      headers: Object.keys(headers).length > 0 ? headers : undefined,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });

    if (res.status === 204) {
      return undefined as T;
    }

    const text = await res.text();
    let parsed: unknown;
    try {
      parsed = text.length > 0 ? JSON.parse(text) : undefined;
    } catch {
      throw new CtxError(`ctx: invalid JSON response from ${path}: ${text}`, res.status);
    }

    if (!res.ok) {
      const message = isErrorResponse(parsed) ? parsed.error : `unexpected status ${res.status}`;
      throw new CtxError(`ctxd: ${message}`, res.status);
    }

    return parsed as T;
  }
}
