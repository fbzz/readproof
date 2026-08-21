import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { after, before, describe, it } from 'node:test'
import { promisify } from 'node:util'

import { merkleRoot, type EvidenceEntry } from '@readproof/sdk'

import { TOOL_BASE_NAMES } from '../src/index.js'
import {
  DEMO_URI,
  fakeAgent,
  field,
  list,
  renderContent,
  startApp,
  startFixture,
  str,
  type App,
  type Fixture,
} from './support.js'

const execFileAsync = promisify(execFile)

describe('dsh-plugin-readproof against a real readproofd', () => {
  let fixture: Fixture
  let app: App
  let originalPolicy: string

  before(async () => {
    fixture = await startFixture()
    originalPolicy = await readFile(fixture.policyPath, 'utf-8')
    app = await startApp({ endpoint: fixture.endpoint, sessionRuns: false })
  })

  after(async () => {
    await app?.stop()
    await fixture?.stop()
  })

  it('registers the thirteen Readproof tools on ctx.tools', () => {
    const names = app.toolNames()
    // The literal list pins the exact MCP tool names; TOOL_BASE_NAMES has to
    // agree with it, since that is what a consumer builds a tool filter from.
    assert.deepEqual(
      [...TOOL_BASE_NAMES].map((base) => `readproof_${base}`).sort(),
      [
        'readproof_diff',
        'readproof_evidence_export',
        'readproof_history',
        'readproof_manifest',
        'readproof_replay',
        'readproof_resolve',
        'readproof_resources_list',
        'readproof_run_commit',
        'readproof_run_mount',
        'readproof_run_start',
        'readproof_tag_delete',
        'readproof_tag_list',
        'readproof_tag_set',
      ],
    )
    for (const expected of [
      'readproof_resources_list',
      'readproof_resolve',
      'readproof_history',
      'readproof_run_start',
      'readproof_run_mount',
      'readproof_run_commit',
      'readproof_manifest',
      'readproof_diff',
      'readproof_replay',
      'readproof_tag_set',
      'readproof_tag_list',
      'readproof_tag_delete',
      'readproof_evidence_export',
    ]) {
      assert.ok(names.includes(expected), `${expected} is not registered (registered: ${names.join(', ')})`)
    }
  })

  it('lists the registered resource with its source and policy', async () => {
    const value = await app.value('readproof_resources_list', {})
    const resources = list(field(value, 'resources'))
    const demo = resources.find((r) => field(r, 'uri') === DEMO_URI)
    assert.ok(demo, `${DEMO_URI} missing from the listing`)
    assert.equal(field(field(demo, 'source'), 'kind'), 'filesystem')
    assert.equal(field(field(demo, 'policy'), 'strategy'), 'require_fresh')
    assert.match(str(field(demo, 'description')), /^filesystem · require_fresh — /)
  })

  it('readproof_resolve returns the bytes with the snapshot id and content hash', async () => {
    const value = await app.value('readproof_resolve', { uri: DEMO_URI })
    assert.equal(field(value, 'uri'), DEMO_URI)
    assert.equal(field(field(value, 'content'), 'text'), originalPolicy)
    assert.equal(field(field(value, 'content'), 'truncated'), false)
    assert.match(str(field(value, 'snapshot_id')), /^snap_/)
    assert.match(str(field(value, 'content_hash')), /^sha256:[0-9a-f]{64}$/)
    assert.ok(str(field(value, 'source_revision')).length > 0)
    // No agent was passed, so nothing was mirrored into a session run.
    assert.equal(field(value, 'session_run'), undefined)
  })

  it('renders a short human line before the JSON payload', async () => {
    const result = await app.call('readproof_resolve', { uri: DEMO_URI })
    assert.equal(result.isError, false)
    const [headline, payload] = result.content
    assert.equal(headline?.type, 'text')
    assert.match(headline?.type === 'text' ? headline.text : '', /^readproof:\/\/demo\/policies\/refunds → snapshot snap_/)
    assert.equal(payload?.type, 'text')
    assert.doesNotThrow(() => JSON.parse(payload?.type === 'text' ? payload.text : ''))
  })

  it('start → mount → commit yields a manifest holding the entry', async () => {
    await app.value('readproof_run_start', { run_id: 'audit-a' })
    const mounted = await app.value('readproof_run_mount', { run_id: 'audit-a', uri: DEMO_URI })
    assert.equal(field(mounted, 'position'), 0)
    assert.equal(field(field(field(mounted, 'resolved'), 'content'), 'text'), originalPolicy)

    const committed = await app.value('readproof_run_commit', { run_id: 'audit-a' })
    assert.match(str(field(committed, 'manifest_id')), /^manifest_/)
    assert.equal(field(committed, 'run_id'), 'audit-a')

    const entries = list(field(committed, 'entries'))
    assert.equal(entries.length, 1)
    assert.equal(field(entries[0], 'uri'), DEMO_URI)

    // The same manifest is reachable by run id alone.
    const byRun = await app.value('readproof_manifest', { target: 'audit-a' })
    assert.deepEqual(byRun, committed)
  })

  it('readproof_diff reports the changed entry with both source revisions', async () => {
    await writeFile(fixture.policyPath, `${originalPolicy}\nRefunds over $500 need manager approval.\n`, 'utf-8')

    await app.value('readproof_run_start', { run_id: 'audit-b' })
    await app.value('readproof_run_mount', { run_id: 'audit-b', uri: DEMO_URI })
    await app.value('readproof_run_commit', { run_id: 'audit-b' })

    const diff = await app.value('readproof_diff', { a: 'audit-a', b: 'audit-b' })
    assert.equal(field(diff, 'changed'), 1)
    assert.equal(field(diff, 'added'), 0)
    assert.equal(field(diff, 'removed'), 0)

    const entry = list(field(diff, 'entries')).find((e) => field(e, 'uri') === DEMO_URI)
    assert.ok(entry, 'the changed URI is missing from the diff entries')
    assert.equal(field(entry, 'status'), 'changed')
    const revisionA = str(field(entry, 'source_revision_a'))
    const revisionB = str(field(entry, 'source_revision_b'))
    assert.ok(revisionA.length > 0 && revisionB.length > 0, 'both sides must carry a source revision')
    assert.notEqual(revisionA, revisionB)
    assert.match(str(field(entry, 'unified_diff')), /manager approval/)
  })

  it('readproof_replay verifies the old run and hands back exactly the bytes it saw', async () => {
    const replay = await app.value('readproof_replay', { target: 'audit-a', include_content: true })
    assert.equal(field(replay, 'all_match'), true)
    const entries = list(field(replay, 'entries'))
    assert.equal(entries.length, 1)
    assert.equal(field(entries[0], 'match'), true)
    assert.equal(field(entries[0], 'recorded_hash'), field(entries[0], 'replayed_hash'))
    // The source has changed on disk since audit-a; replay reads Readproof's own
    // storage, so it still returns the bytes that run actually saw.
    assert.equal(field(field(entries[0], 'content'), 'text'), originalPolicy)
  })

  it('omits content from a replay unless it is asked for', async () => {
    const replay = await app.value('readproof_replay', { target: 'audit-a' })
    assert.equal(field(list(field(replay, 'entries'))[0], 'content'), undefined)
  })

  it('a tag pins one snapshot, and reading uri@tag returns exactly its bytes', async () => {
    const history = await app.value('readproof_history', { uri: DEMO_URI })
    const snapshots = list(field(history, 'snapshots'))
    assert.ok(snapshots.length >= 2, 'expected at least two snapshots after the edit')

    // history is newest first, so the original bytes are the oldest entry.
    const oldest = snapshots[snapshots.length - 1]
    const oldestId = str(field(oldest, 'snapshot_id'))

    const tag = await app.value('readproof_tag_set', { uri: DEMO_URI, tag: 'prod', snapshot_id: oldestId })
    assert.equal(field(tag, 'reference'), `${DEMO_URI}@prod`)

    const pinned = await app.value('readproof_resolve', { uri: `${DEMO_URI}@prod` })
    assert.equal(field(pinned, 'decision'), 'use_tag')
    assert.equal(field(pinned, 'snapshot_id'), oldestId)
    assert.equal(field(field(pinned, 'content'), 'text'), originalPolicy)

    const tags = await app.value('readproof_tag_list', { uri: DEMO_URI })
    assert.equal(list(field(tags, 'tags')).length, 1)

    // readproof_history/readproof_tag_* accept a tagged reference and operate on the resource.
    const viaTaggedUri = await app.value('readproof_tag_list', { uri: `${DEMO_URI}@prod` })
    assert.equal(field(viaTaggedUri, 'uri'), DEMO_URI)

    const deleted = await app.value('readproof_tag_delete', { uri: DEMO_URI, tag: 'prod' })
    assert.equal(field(deleted, 'deleted'), true)
    assert.equal(list(field(await app.value('readproof_tag_list', { uri: DEMO_URI }), 'tags')).length, 0)
  })

  it('readproof_evidence_export produces a bundle the Go CLI verifies', async () => {
    const bundle = await app.value('readproof_evidence_export', { target: 'audit-a' })

    const predicate = field(bundle, 'predicate')
    const subject = list(field(bundle, 'subject'))[0]
    const root = str(field(field(subject, 'digest'), 'sha256'))

    // The root has to be the SDK's own merkle root over the same entries —
    // if the tool reshaped the bundle, this is where it shows.
    const entries = list(field(predicate, 'entries')).map((e) => ({
      position: Number(field(e, 'position')),
      uri: str(field(e, 'uri')),
      content_hash: str(field(e, 'content_hash')),
    })) satisfies Pick<EvidenceEntry, 'position' | 'uri' | 'content_hash'>[]
    assert.equal(root, merkleRoot(entries))
    assert.equal(field(field(predicate, 'merkle'), 'root'), root)
    assert.equal(field(field(predicate, 'replay'), 'all_match'), true)

    const bundlePath = join(fixture.tmpDir, 'evidence-audit-a.json')
    await writeFile(bundlePath, `${JSON.stringify(bundle, null, 2)}\n`, 'utf-8')
    const { stdout } = await execFileAsync(fixture.readproofBin, [
      '--server',
      fixture.endpoint,
      'evidence',
      'verify',
      bundlePath,
    ])
    assert.match(stdout, /evidence verified/)
    assert.ok(stdout.includes(root), `the CLI report should name the same merkle root:\n${stdout}`)
  })

  it('turns an unknown URI into a readable tool error and stays healthy', async () => {
    const result = await app.call('readproof_resolve', { uri: 'readproof://demo/does-not-exist' })
    assert.equal(result.isError, true)
    const message = result.isError ? result.error.message : ''
    assert.match(message, /^resolve readproof:\/\/demo\/does-not-exist: /)
    assert.match(message, /not found/i)
    assert.match(renderContent(result), /does-not-exist/)

    // The failure was materialized as a result, not thrown at the harness.
    const still = await app.value('readproof_resources_list', {})
    assert.ok(list(field(still, 'resources')).length >= 1)
  })

  it('rejects a malformed URI before contacting the server', async () => {
    const result = await app.call('readproof_history', { uri: 'not-a-readproof-uri' })
    assert.equal(result.isError, true)
    assert.match(result.isError ? result.error.message : '', /must start with "readproof:\/\/"/)
  })

  it('rejects arguments that do not match the declared schema', async () => {
    const result = await app.call('readproof_resolve', {})
    assert.equal(result.isError, true)
    assert.match(result.isError ? result.error.message : '', /uri/)
  })
})

describe('session runs', () => {
  let fixture: Fixture
  let app: App
  let policy: string

  before(async () => {
    fixture = await startFixture()
    policy = await readFile(fixture.policyPath, 'utf-8')
    app = await startApp({ endpoint: fixture.endpoint, sessionRuns: true })
  })

  after(async () => {
    await app?.stop()
    await fixture?.stop()
  })

  it('mirrors every resolve made on a session’s behalf into that session’s run', async () => {
    const agent = fakeAgent('sess-01')
    const first = await app.value('readproof_resolve', { uri: DEMO_URI }, agent)
    const mount = field(first, 'session_run')
    assert.ok(mount, 'readproof_resolve should report the run it was recorded in')
    assert.equal(field(mount, 'run_id'), 'dsh-sess-01')
    assert.equal(field(mount, 'position'), 0)
    assert.equal(field(field(first, 'content'), 'text'), policy)

    const second = await app.value('readproof_resolve', { uri: DEMO_URI }, agent)
    assert.equal(field(field(second, 'session_run'), 'position'), 1)
  })

  it('commits the session run lazily when its manifest is asked for', async () => {
    const manifest = await app.value('readproof_manifest', { target: 'dsh-sess-01' })
    assert.equal(field(manifest, 'run_id'), 'dsh-sess-01')
    const entries = list(field(manifest, 'entries'))
    assert.equal(entries.length, 2)
    assert.equal(field(entries[0], 'uri'), DEMO_URI)
    assert.equal(field(entries[0], 'position'), 0)
    assert.equal(field(entries[1], 'position'), 1)
  })

  it('continues in the next epoch after its run was committed', async () => {
    // A committed run refuses further mounts, so the session moves on rather
    // than failing the model's next read.
    const again = await app.value('readproof_resolve', { uri: DEMO_URI }, fakeAgent('sess-01'))
    assert.equal(field(field(again, 'session_run'), 'run_id'), 'dsh-sess-01-2')
    assert.equal(field(field(again, 'session_run'), 'position'), 0)
  })

  it('commits a session’s run when the agent is disposed', async () => {
    const agent = fakeAgent('sess-02')
    await app.value('readproof_resolve', { uri: DEMO_URI }, agent)

    app.ctx.emit('agent/disposed', { agent })
    // The listener commits asynchronously; the manifest exists once it lands.
    const manifest = await eventually(() => app.value('readproof_manifest', { target: 'dsh-sess-02' }))
    assert.equal(list(field(manifest, 'entries')).length, 1)
  })

  it('leaves a call with no agent unrecorded', async () => {
    const value = await app.value('readproof_resolve', { uri: DEMO_URI })
    assert.equal(field(value, 'session_run'), undefined)
  })

  it('commits every open session run when the plugin unloads', async () => {
    await app.value('readproof_resolve', { uri: DEMO_URI }, fakeAgent('sess-03'))
    await app.stop()

    // Read it back through the SDK: the plugin is gone, but its run is not.
    const manifest = await fixture.client.getManifest('dsh-sess-03')
    assert.equal(manifest.run_id, 'dsh-sess-03')
    assert.equal(manifest.entries.length, 1)
    assert.equal(manifest.entries[0]?.uri, DEMO_URI)
  })
})

/** Retry `body` until it stops throwing, for a listener that runs off-thread. */
async function eventually<T>(body: () => Promise<T>, timeoutMs = 5_000): Promise<T> {
  const deadline = Date.now() + timeoutMs
  let last: unknown
  while (Date.now() < deadline) {
    try {
      return await body()
    } catch (err) {
      last = err
      await new Promise((res) => setTimeout(res, 25))
    }
  }
  throw last instanceof Error ? last : new Error(String(last))
}
