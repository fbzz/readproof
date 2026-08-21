import { ReadproofError } from "./errors.js";
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

export interface ReadproofOptions {
  /** Base URL of a running readproofd, e.g. "http://localhost:8080". */
  endpoint: string;
  /** Sent as "Authorization: Bearer <apiKey>" if the server has --api-key set. */
  apiKey?: string;
  /** Override fetch (e.g. for testing). Defaults to the global fetch. */
  fetch?: typeof fetch;
  /**
   * Milliseconds before a request is aborted. Defaults to 30_000. A hung
   * readproofd otherwise stalls the agent turn that is waiting on it for as
   * long as the connection stays open — there is no default timeout in
   * `fetch`. Pass 0 to wait indefinitely.
   */
  timeoutMs?: number;
  /**
   * Maximum response bytes accepted before parsing. Defaults to 16 MiB.
   * Responses carry document content, so they are not tiny — but they are
   * also buffered whole, and an endless one is a memory problem the caller
   * never asked for.
   */
  maxResponseBytes?: number;
}

/** Default request timeout: long enough for a replay, short enough to fail. */
export const DEFAULT_TIMEOUT_MS = 30_000;

/** Default cap on a single response body. */
export const DEFAULT_MAX_RESPONSE_BYTES = 16 * 1024 * 1024;

/**
 * How much of a response body an error message may quote. The message reaches
 * a model in the agent-tool paths built on this SDK, so an unbounded body
 * would become unbounded prompt.
 */
const MAX_ERROR_BODY_CHARS = 512;

function truncateForError(text: string): string {
  if (text.length <= MAX_ERROR_BODY_CHARS) return text;
  return `${text.slice(0, MAX_ERROR_BODY_CHARS)}… (${text.length} chars total, truncated)`;
}

/**
 * Validate the endpoint at construction, where the mistake is.
 *
 * A relative path, a typo'd scheme, or a `file:`/`data:` URL otherwise fails
 * later, once per call, as whatever the runtime's fetch happens to say. Only
 * http and https are accepted: this client talks to readproofd over HTTP and
 * nothing else.
 */
function normalizeEndpoint(endpoint: string): string {
  let parsed: URL;
  try {
    parsed = new URL(endpoint);
  } catch {
    throw new ReadproofError(
      `readproof: invalid endpoint ${JSON.stringify(endpoint)}: expected an absolute URL such as "http://localhost:8080"`,
    );
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    throw new ReadproofError(
      `readproof: invalid endpoint ${JSON.stringify(endpoint)}: scheme ${parsed.protocol} is not supported (want http or https)`,
    );
  }
  // Trim trailing slashes with a loop rather than /\/+$/: that regex
  // backtracks quadratically on a long run of slashes (CodeQL
  // js/polynomial-redos), and the endpoint is caller-supplied.
  let trimmed = endpoint;
  while (trimmed.endsWith("/")) trimmed = trimmed.slice(0, -1);
  return trimmed;
}

export interface RunOptions {
  /** Run id — must be unique per run against a given readproofd instance. */
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
 * Readproof is the TypeScript SDK client for a running readproofd. Every method maps
 * 1:1 to one of readproofd's HTTP API endpoints.
 *
 * ```ts
 * const rp = new Readproof({ endpoint: "http://localhost:8080" });
 * const policy = await rp.resolve("readproof://acme/policies/refunds");
 * ```
 */
export class Readproof {
  private readonly endpoint: string;
  private readonly apiKey?: string;
  private readonly fetchImpl: typeof fetch;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;

  constructor(options: ReadproofOptions) {
    this.endpoint = normalizeEndpoint(options.endpoint);
    this.apiKey = options.apiKey;
    this.fetchImpl = options.fetch ?? fetch;
    this.timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
    this.maxResponseBytes = options.maxResponseBytes ?? DEFAULT_MAX_RESPONSE_BYTES;
  }

  /**
   * Resolve a context resource to its current content and metadata.
   *
   * `uri` may carry a trailing `@<tag>` (`readproof://acme/policies/refunds@prod`),
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
   * Begin a context run: `rp.run({ id }).mount(uri)...commit()`. The run
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

    // A timeout has to be a signal rather than a race, so the socket is
    // actually released; without one, a readproofd that accepts a connection
    // and then stalls holds this call for as long as it likes.
    const controller = new AbortController();
    const timer =
      this.timeoutMs > 0 ? setTimeout(() => controller.abort(), this.timeoutMs) : undefined;

    let res: Response;
    try {
      res = await this.fetchImpl(`${this.endpoint}${path}`, {
        method,
        headers: Object.keys(headers).length > 0 ? headers : undefined,
        body: body !== undefined ? JSON.stringify(body) : undefined,
        signal: controller.signal,
      });
    } catch (err) {
      if (controller.signal.aborted) {
        throw new ReadproofError(`readproof: request to ${path} timed out after ${this.timeoutMs}ms`);
      }
      throw err;
    } finally {
      if (timer !== undefined) clearTimeout(timer);
    }

    if (res.status === 204) {
      return undefined as T;
    }

    const text = await this.readCapped(res, path);
    let parsed: unknown;
    try {
      parsed = text.length > 0 ? JSON.parse(text) : undefined;
    } catch {
      throw new ReadproofError(
        `readproof: invalid JSON response from ${path}: ${truncateForError(text)}`,
        res.status,
      );
    }

    if (!res.ok) {
      const message = isErrorResponse(parsed) ? parsed.error : `unexpected status ${res.status}`;
      throw new ReadproofError(`readproofd: ${truncateForError(message)}`, res.status);
    }

    return parsed as T;
  }

  /**
   * Read a response body, refusing anything over the cap.
   *
   * Refusing rather than truncating: a truncated body is not valid JSON, and
   * a caller that got a short read of a replay would be looking at fewer
   * entries than the manifest holds. Content-Length is only a shortcut —
   * chunked responses do not carry one, so the stream is counted as it
   * arrives and aborted the moment it goes over.
   */
  private async readCapped(res: Response, path: string): Promise<string> {
    const tooBig = (bytes: number): ReadproofError =>
      new ReadproofError(
        `readproof: response from ${path} exceeds the ${this.maxResponseBytes}-byte limit (${bytes} bytes)`,
        res.status,
      );

    const declared = Number(res.headers.get("content-length"));
    if (Number.isFinite(declared) && declared > this.maxResponseBytes) {
      throw tooBig(declared);
    }
    if (res.body === null) {
      return res.text();
    }

    const reader = res.body.getReader();
    const chunks: Uint8Array[] = [];
    let total = 0;
    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        if (value === undefined) continue;
        total += value.byteLength;
        if (total > this.maxResponseBytes) {
          await reader.cancel();
          throw tooBig(total);
        }
        chunks.push(value);
      }
    } finally {
      reader.releaseLock();
    }
    return Buffer.concat(chunks).toString("utf-8");
  }
}
