import { buildEvidence, ReadproofError, type Readproof } from '@readproof/sdk'
import type { Context } from '@deepseek-ai/cordis'
import type { ContentBlock } from '@deepseek-ai/dsh-llm'
import { defineTool, type JsonValue, type ToolRunContext } from '@deepseek-ai/dsh-tools'

import type { Config } from './config.js'
import { embeddedContentBytes, encodeContent } from './content.js'
import { commitRun, mountRun, startRun } from './readproof-client.js'
import {
  diffOut,
  manifestOut,
  replayOut,
  resolveOut,
  resourceInfo,
  snapshotInfo,
  tagInfo,
  toJson,
  type ReplayEntryOut,
} from './payloads.js'
import type { SessionRuns } from './session-runs.js'

export interface ToolDeps {
  client: Readproof
  config: Config
  /** Absent when `sessionRuns` is off. */
  sessions?: SessionRuns
}

/** The tool names this plugin registers, without the configured prefix. */
export const TOOL_BASE_NAMES = [
  'resources_list',
  'resolve',
  'history',
  'run_start',
  'run_mount',
  'run_commit',
  'manifest',
  'diff',
  'replay',
  'tag_set',
  'tag_list',
  'tag_delete',
  'evidence_export',
] as const

/**
 * Register every Readproof tool on `ctx.tools`.
 *
 * Names, descriptions, parameter documentation, and result shapes mirror
 * internal/mcp/tools.go, so the harness surface and the MCP surface are the
 * same surface. The descriptions are written for a model, not for a
 * changelog: each says what the tool is *for* and what it costs.
 *
 * `ctx.tools.register` returns a disposer that Cordis attaches to this
 * plugin's fiber, so unloading or hot-replacing the plugin unregisters
 * every tool (docs/cordis-tutorial/07-into-the-harness.md).
 */
export function registerTools(ctx: Context, deps: ToolDeps): void {
  const { client, config, sessions } = deps
  /** Qualify a base name with the configured prefix. */
  const t = (base: string): string => `${config.toolPrefix}${base}`

  ctx.tools.register(
    defineTool({
      name: t('resources_list'),
      description:
        'List every document registered in Readproof, with its readproof:// URI, where its bytes come from, and the freshness policy that governs reading it. ' +
        'Call this first to discover what you are allowed to read.',
      parameters: {},
      output: {
        schema: { type: 'json', description: 'A { resources: [...] } object.' },
        render: (_args, value) => blocks(`${count(read(value).resources, 'resource')} registered in Readproof.`, value),
      },
      async execute() {
        return attempt('list resources', async () => {
          const resources = await client.listResources()
          return toJson({ resources: resources.map(resourceInfo) })
        })
      },
    }),
  )

  ctx.tools.register(
    defineTool({
      name: t('resolve'),
      description:
        'Read one document and get its bytes together with the snapshot id, content hash, and source revision that identify exactly what you read. ' +
        'Use it whenever you need the current governed version of a policy, spec, or runbook; append @<tag> to the URI to read a pinned snapshot instead. ' +
        'Note: depending on the resource’s policy this may fetch from the source and record a new snapshot.' +
        (sessions
          ? ` Every read is also recorded in this session’s Readproof run, so the result carries the run id it landed in; commit that run with ${t('run_commit')} to freeze it into a manifest.`
          : ''),
      parameters: {
        uri: {
          type: 'string',
          required: true,
          description:
            'the resource to read, readproof://<namespace>/<path>; append @<tag> (e.g. readproof://acme/policies/refunds@prod) to pin exactly the snapshot that tag names',
        },
      },
      output: {
        schema: { type: 'json', description: 'The resolved snapshot, its provenance, and the content.' },
        render: (args, value) => blocks(resolveHeadline(args.uri, value), value),
      },
      async execute(args, exec) {
        return attempt(`resolve ${args.uri}`, async () => {
          const sessionId = sessionIdOf(exec)
          if (sessions && sessionId !== undefined) {
            const { result, mount } = await sessions.mount(sessionId, args.uri)
            return toJson(resolveOut(result, inline(result.content, result.snapshot.content_hash, config), mount))
          }
          const result = await client.resolve(args.uri)
          return toJson(resolveOut(result, inline(result.content, result.snapshot.content_hash, config), undefined))
        })
      },
    }),
  )

  ctx.tools.register(
    defineTool({
      name: t('history'),
      description:
        'List every snapshot Readproof has recorded for one resource, newest first, with the tags that point at each. ' +
        `Use it to find the snapshot id to pin with ${t('tag_set')}, or to see when and how often a document changed.`,
      parameters: {
        uri: {
          type: 'string',
          required: true,
          description: 'a Readproof resource reference, readproof://<namespace>/<path>, optionally with a trailing @<tag>',
        },
      },
      output: {
        schema: { type: 'json', description: 'A { uri, snapshots: [...] } object, newest snapshot first.' },
        render: (_args, value) =>
          blocks(
            `${count(read(value).snapshots, 'snapshot')} recorded for ${text(read(value).uri)}.`,
            value,
          ),
      },
      async execute(args) {
        const uri = bareUri(args.uri)
        return attempt(`history ${uri}`, async () => {
          // Two calls, as the MCP server does: the tags are what turn a list
          // of snapshot ids into "which of these can I pin?".
          const [snapshots, tags] = await Promise.all([client.history(uri), client.listTags(uri)])
          const bySnapshot = new Map<string, string[]>()
          for (const tag of tags) {
            const names = bySnapshot.get(tag.snapshot_id) ?? []
            names.push(tag.tag)
            bySnapshot.set(tag.snapshot_id, names)
          }
          return toJson({
            uri,
            snapshots: snapshots.map((s) => snapshotInfo(s, bySnapshot.get(s.id) ?? [])),
          })
        })
      },
    }),
  )

  ctx.tools.register(
    defineTool({
      name: t('run_start'),
      description:
        'Open a run: the container that records everything you are about to read. ' +
        `Call it once with an id you choose, then ${t('run_mount')} for each document, then ${t('run_commit')} — which returns the manifest id that names the complete, replayable set of bytes this run saw.`,
      parameters: {
        run_id: {
          type: 'string',
          required: true,
          description: 'an identifier you choose for this run; reuse the same value across start, mount, and commit',
        },
      },
      output: {
        schema: { type: 'json', description: 'A { run_id, next_step } object.' },
        render: (args, value) => blocks(`Run ${args.run_id} is open.`, value),
      },
      async execute(args) {
        return attempt(`start run ${args.run_id}`, async () => {
          await startRun(client, args.run_id)
          return toJson({
            run_id: args.run_id,
            // Spell out the lifecycle: a model that called start in isolation
            // needs to know a manifest only exists after a commit.
            next_step: `call ${t('run_mount')} for each resource this run reads, then ${t('run_commit')} to get the manifest id`,
          })
        })
      },
    }),
  )

  ctx.tools.register(
    defineTool({
      name: t('run_mount'),
      description:
        'Read a document and record it in an open run at the next position. ' +
        `Use this instead of ${t('resolve')} whenever the work you are doing should be auditable: mounted reads become manifest entries you can later diff, replay, and export as evidence. ` +
        `Mount order is preserved because it can change what a model concludes. Like ${t('resolve')}, this may fetch from the source and record a new snapshot.`,
      parameters: {
        run_id: { type: 'string', required: true, description: `the run id passed to ${t('run_start')}` },
        uri: {
          type: 'string',
          required: true,
          description: 'the resource to mount, readproof://<namespace>/<path>, optionally with @<tag>',
        },
      },
      output: {
        schema: { type: 'json', description: 'A { run_id, position, resolved } object.' },
        render: (args, value) =>
          blocks(`Mounted ${args.uri} into run ${args.run_id} at position ${number(read(value).position)}.`, value),
      },
      async execute(args) {
        return attempt(`mount ${args.uri} into run ${args.run_id}`, async () => {
          const { position, resolve } = await mountRun(client, args.run_id, args.uri)
          return toJson({
            run_id: args.run_id,
            position,
            resolved: resolveOut(resolve, inline(resolve.content, resolve.snapshot.content_hash, config), undefined),
          })
        })
      },
    }),
  )

  ctx.tools.register(
    defineTool({
      name: t('run_commit'),
      description:
        'Close a run and freeze everything mounted into it as an immutable manifest. ' +
        `Returns the manifest id — cite it in whatever you produce, because it is the single handle for ${t('manifest')}, ${t('diff')}, ${t('replay')}, and ${t('evidence_export')}.`,
      parameters: {
        run_id: {
          type: 'string',
          required: true,
          description: 'an identifier you choose for this run; reuse the same value across start, mount, and commit',
        },
      },
      output: {
        schema: { type: 'json', description: 'The committed manifest.' },
        render: (_args, value) => blocks(manifestHeadline(value), value),
      },
      async execute(args) {
        return attempt(`commit run ${args.run_id}`, async () => {
          // A session run this plugin owns is committed through the session
          // bookkeeping, so its in-memory state stops handing out mounts for
          // a run id that can no longer accept them.
          const owned = await sessions?.commitIfOwned(args.run_id)
          const man = owned ?? (await commitRun(client, args.run_id))
          return toJson(manifestOut(man))
        })
      },
    }),
  )

  ctx.tools.register(
    defineTool({
      name: t('manifest'),
      description:
        'Show a committed manifest: every document the run read, in mount order, with the snapshot id and content hash of each. ' +
        "Use it to answer 'what exactly did that run see?' from a manifest id or run id alone.",
      parameters: {
        target: { type: 'string', required: true, description: 'a manifest id, or the run id it was committed from' },
      },
      output: {
        schema: { type: 'json', description: 'The manifest and its entries in mount order.' },
        render: (_args, value) => blocks(manifestHeadline(value), value),
      },
      async execute(args) {
        return attempt(`get manifest ${args.target}`, async () => {
          await sessions?.commitIfOwned(args.target)
          return toJson(manifestOut(await client.getManifest(args.target)))
        })
      },
    }),
  )

  ctx.tools.register(
    defineTool({
      name: t('diff'),
      description:
        'Compare what two runs read. For each document it reports added/removed/changed/unchanged, the unified text diff of any change, and the provenance behind it — each side’s source revision, observation time, and pinned tag. ' +
        'Use it to explain why two runs of the same task disagreed.',
      parameters: {
        a: { type: 'string', required: true, description: 'the baseline manifest id or run id' },
        b: {
          type: 'string',
          required: true,
          description: 'the manifest id or run id to compare against the baseline',
        },
      },
      output: {
        schema: { type: 'json', description: 'Per-URI status, provenance, and unified diffs.' },
        render: (_args, value) => {
          const v = read(value)
          return blocks(
            `${number(v.changed)} changed, ${number(v.added)} added, ${number(v.removed)} removed, ${number(v.unchanged)} unchanged.`,
            value,
          )
        },
      },
      async execute(args) {
        return attempt(`diff ${args.a} ${args.b}`, async () => {
          await Promise.all([sessions?.commitIfOwned(args.a), sessions?.commitIfOwned(args.b)])
          return toJson(diffOut(await client.diff(args.a, args.b)))
        })
      },
    }),
  )

  ctx.tools.register(
    defineTool({
      name: t('replay'),
      description:
        'Reconstruct a manifest’s bytes from Readproof’s own storage and re-hash them, without contacting any source. ' +
        'Use it to prove a past run is still reproducible, or (with include_content) to get back exactly the bytes that run saw even after the sources changed.',
      parameters: {
        target: { type: 'string', required: true, description: 'a manifest id, or the run id it was committed from' },
        include_content: {
          type: 'boolean',
          description: "also return each entry's reconstructed bytes; off by default because verification only needs the hashes",
        },
      },
      output: {
        schema: { type: 'json', description: 'Per-entry recorded vs. replayed hashes, and optionally the bytes.' },
        render: (_args, value) => {
          const v = read(value)
          const ok = v.all_match === true
          return blocks(
            `${ok ? 'Replay verified' : 'REPLAY MISMATCH'}: ${count(v.entries, 'entry', 'entries')} in manifest ${text(v.manifest_id)}.`,
            value,
          )
        },
      },
      async execute(args) {
        return attempt(`replay ${args.target}`, async () => {
          await sessions?.commitIfOwned(args.target)
          const result = await client.replay(args.target)
          const entries: ReplayEntryOut[] = result.entries.map((e) => ({
            position: e.position,
            uri: e.uri,
            materialization_id: e.materialization_id,
            recorded_hash: e.recorded_hash,
            replayed_hash: e.replayed_hash,
            match: e.match,
            ...(args.include_content === true
              ? { content: encodeContent(e.content, e.recorded_hash, config.maxInlineBytes) }
              : {}),
          }))
          return toJson(replayOut(result, entries))
        })
      },
    }),
  )

  ctx.tools.register(
    defineTool({
      name: t('tag_set'),
      description:
        'Create or move a named pointer from a resource to one of its snapshots, so it can be read as readproof://<namespace>/<path>@<tag>. ' +
        "Use it to freeze a known-good version (e.g. 'prod') that later reads resolve to with no fetch and no policy evaluation.",
      parameters: {
        uri: { type: 'string', required: true, description: 'the resource the tag belongs to, readproof://<namespace>/<path>' },
        tag: { type: 'string', required: true, description: 'the tag name, matching ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$' },
        snapshot_id: {
          type: 'string',
          required: true,
          description: `the snapshot to point the tag at; it must be a snapshot of this same resource (see ${t('history')})`,
        },
      },
      output: {
        schema: { type: 'json', description: 'The tag and the exact uri@tag reference to read it by.' },
        render: (_args, value) => blocks(`Tag set: read it as ${text(read(value).reference)}.`, value),
      },
      async execute(args) {
        const uri = bareUri(args.uri)
        return attempt(`set tag ${uri}@${args.tag}`, async () =>
          toJson(tagInfo(await client.setTag(uri, args.tag, args.snapshot_id))),
        )
      },
    }),
  )

  ctx.tools.register(
    defineTool({
      name: t('tag_list'),
      description: 'List the tags on one resource and the snapshot each points at, with the exact uri@tag reference to read it by.',
      parameters: {
        uri: {
          type: 'string',
          required: true,
          description: 'a Readproof resource reference, readproof://<namespace>/<path>, optionally with a trailing @<tag>',
        },
      },
      output: {
        schema: { type: 'json', description: 'A { uri, tags: [...] } object.' },
        render: (_args, value) =>
          blocks(`${count(read(value).tags, 'tag')} on ${text(read(value).uri)}.`, value),
      },
      async execute(args) {
        const uri = bareUri(args.uri)
        return attempt(`list tags ${uri}`, async () => {
          const tags = await client.listTags(uri)
          return toJson({ uri, tags: tags.map(tagInfo) })
        })
      },
    }),
  )

  ctx.tools.register(
    defineTool({
      name: t('tag_delete'),
      description:
        'Remove a tag. The snapshot it pointed at is untouched and still readable by its snapshot id; only the name goes away. ' +
        'Manifests that mounted the tag keep replaying identically, because they recorded the snapshot, not the name.',
      parameters: {
        uri: { type: 'string', required: true, description: 'the resource the tag belongs to' },
        tag: {
          type: 'string',
          required: true,
          description: 'the tag name to delete; the snapshot it pointed at is untouched',
        },
      },
      output: {
        schema: { type: 'json', description: 'A { uri, tag, deleted } object.' },
        render: (args, value) => blocks(`Deleted tag ${args.tag} from ${args.uri}.`, value),
      },
      async execute(args) {
        const uri = bareUri(args.uri)
        return attempt(`delete tag ${uri}@${args.tag}`, async () => {
          await client.deleteTag(uri, args.tag)
          return toJson({ uri, tag: args.tag, deleted: true })
        })
      },
    }),
  )

  ctx.tools.register(
    defineTool({
      name: t('evidence_export'),
      description:
        'Produce a portable, integrity-checked record of one run: an in-toto statement whose digest is a Merkle root over the manifest entries, plus each document’s provenance, the resource definitions behind them, and a replay verification. Bundles are unsigned, so they are tamper-evident against the Readproof store, not offline. ' +
        "Use it when someone needs to audit what the agent read — 'with_content' additionally embeds the bytes.",
      parameters: {
        target: { type: 'string', required: true, description: 'a manifest id, or the run id it was committed from' },
        with_content: {
          type: 'boolean',
          description:
            'embed each entry’s bytes in the bundle as base64; off by default so the bundle proves what was read without disclosing it. Refused when the summed content exceeds the inline limit — use the readproof CLI for a full bundle',
        },
      },
      output: {
        schema: { type: 'json', description: 'An in-toto Statement v1 evidence bundle.' },
        render: (_args, value) => blocks(evidenceHeadline(value), value),
      },
      async execute(args) {
        return attempt(`export evidence for ${args.target}`, async () => {
          await sessions?.commitIfOwned(args.target)
          const withContent = args.with_content === true
          // buildEvidence is composed purely from public SDK calls, so this
          // bundle is byte-comparable with the one `readproof evidence export`
          // writes for the same target — same merkle root above all.
          const bundle = await buildEvidence(client, args.target, { withContent })
          // Every other content path here caps what reaches the model at
          // maxInlineBytes; with_content embeds the full base64 of every
          // entry, so without this check the documented cap simply did not
          // apply to the largest payload the plugin can produce.
          //
          // Refused, not truncated: a bundle is evidence because its merkle
          // root covers exactly the entries it shows. Cutting the content
          // would leave a document that still claims that root while no
          // longer carrying what it attests to — worse than no bundle. The
          // full export belongs outside a model's context anyway.
          if (withContent) {
            const total = embeddedContentBytes(bundle.predicate.entries)
            if (total > config.maxInlineBytes) {
              throw new Error(
                `with_content would embed ${total} bytes, over this deployment's ${config.maxInlineBytes}-byte inline limit. ` +
                  `A truncated bundle is not evidence — its merkle root would no longer cover what it shows — so it is refused rather than cut. ` +
                  `Export the full bundle outside this conversation with: readproof evidence export ${args.target} --with-content --out bundle.json. ` +
                  `Without with_content the same bundle still proves what was read (hashes, provenance, replay), and readproof_replay returns individual entries.`,
              )
            }
          }
          return toJson(bundle)
        })
      },
    }),
  )
}

/**
 * The DSH session a tool call belongs to, or undefined for a call with no
 * agent behind it (a direct `ctx.tools.execute` from a plugin or a test).
 *
 * `Agent.id` is documented as "the single identity shared with
 * {@link session}" (@deepseek-ai/dsh-agent), so it is the session id.
 */
export function sessionIdOf(exec: ToolRunContext): string | undefined {
  return exec.agent?.id
}

/** Strip a trailing `@<tag>`; mirrors resource.SplitRef in Go. */
export function bareUri(raw: string): string {
  const prefix = 'readproof://'
  if (!raw.startsWith(prefix)) {
    throw new Error(`readproof: invalid uri ${JSON.stringify(raw)}: must start with "${prefix}"`)
  }
  const rest = raw.slice(prefix.length)
  const at = rest.lastIndexOf('@')
  if (at < 0) return raw
  if (at === rest.length - 1) {
    throw new Error(`readproof: invalid reference ${JSON.stringify(raw)}: empty tag after "@"`)
  }
  return prefix + rest.slice(0, at)
}

/**
 * Run `body`, turning any failure into a readable tool error.
 *
 * The registry materializes a thrown error as an `isError` tool result with
 * this message, so the model reads it and can correct itself; nothing here
 * ever takes the harness down. The `<what>: <cause>` shape matches the way
 * internal/mcp/tools.go wraps the same failures.
 */
async function attempt<T>(what: string, body: () => Promise<T>): Promise<T> {
  try {
    return await body()
  } catch (err) {
    throw new Error(`${what}: ${describe(err)}`)
  }
}

function describe(err: unknown): string {
  if (err instanceof ReadproofError) return err.message
  if (err instanceof Error) return err.message
  return String(err)
}

/** Cap the inline bytes a single result carries. */
function inline(content: string, contentHash: string, config: Config) {
  return encodeContent(content, contentHash, config.maxInlineBytes)
}

/** A short human line, then the JSON payload — both as text blocks. */
function blocks(headline: string, value: JsonValue): ContentBlock[] {
  return [
    { type: 'text', text: headline },
    { type: 'text', text: JSON.stringify(value, null, 2) },
  ]
}

function resolveHeadline(uri: string, value: JsonValue): string {
  const v = read(value)
  const truncated = read(v.content).truncated === true ? ' (truncated)' : ''
  const run = v.session_run === undefined ? '' : ` Recorded in run ${text(read(v.session_run).run_id)}.`
  return `${uri} → snapshot ${text(v.snapshot_id)} (${text(v.decision)}, ${number(v.bytes)} bytes${truncated}).${run}`
}

function manifestHeadline(value: JsonValue): string {
  const v = read(value)
  return `Manifest ${text(v.manifest_id)} for run ${text(v.run_id)}: ${count(v.entries, 'entry', 'entries')}.`
}

function evidenceHeadline(value: JsonValue): string {
  const bundle = read(value)
  const subject = read(first(bundle.subject))
  const predicate = read(bundle.predicate)
  const replay = read(predicate.replay)
  const verified = replay.all_match === true ? 'replay verified' : 'REPLAY DID NOT VERIFY'
  return `Evidence bundle for manifest ${text(subject.name)}: merkle root ${text(read(subject.digest).sha256)}, ${verified}.`
}

// Readers that narrow a JsonValue without casting. `render` is handed the
// validated canonical value as an opaque JsonValue, so every field access in
// a headline has to go through one of these.

function read(value: JsonValue | undefined): Record<string, JsonValue> {
  return typeof value === 'object' && value !== null && !Array.isArray(value) ? value : {}
}

function first(value: JsonValue | undefined): JsonValue | undefined {
  return Array.isArray(value) ? value[0] : undefined
}

function text(value: JsonValue | undefined): string {
  return typeof value === 'string' ? value : ''
}

function number(value: JsonValue | undefined): number {
  return typeof value === 'number' ? value : 0
}

function count(value: JsonValue | undefined, singular: string, plural = `${singular}s`): string {
  const n = Array.isArray(value) ? value.length : 0
  return `${n} ${n === 1 ? singular : plural}`
}
