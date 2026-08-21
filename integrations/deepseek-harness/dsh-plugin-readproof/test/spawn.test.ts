import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { after, before, describe, it } from 'node:test'
import { promisify } from 'node:util'

import { Readproof } from '@readproof/sdk'

import { field, freePort, list, repoRoot, startApp, type App } from './support.js'

const execFileAsync = promisify(execFile)

describe('spawn: true', () => {
  let tmpDir: string
  let readproofdBin: string
  let endpoint: string
  let app: App | undefined

  before(async () => {
    tmpDir = await mkdtemp(join(tmpdir(), 'dsh-plugin-readproof-spawn-'))
    await mkdir(join(tmpDir, 'data'))
    readproofdBin = join(tmpDir, 'readproofd')
    await execFileAsync('go', ['build', '-o', readproofdBin, './cmd/readproofd'], { cwd: repoRoot() })
    endpoint = `http://127.0.0.1:${await freePort()}`
  })

  after(async () => {
    await app?.stop()
    await rm(tmpDir, { recursive: true, force: true })
  })

  it('starts a private readproofd, serves tools from it, and kills it on disposal', async () => {
    const addr = endpoint.replace('http://', '')
    app = await startApp({
      spawn: true,
      readproofdPath: readproofdBin,
      dataDir: join(tmpDir, 'data'),
      addr,
      sessionRuns: false,
    })

    // The child answers: the plugin only finishes loading after /healthz does.
    assert.ok((await fetch(`${endpoint}/healthz`)).ok)
    // And the tools are wired to it — an empty data directory lists nothing.
    const value = await app.value('readproof_resources_list', {})
    assert.equal(list(field(value, 'resources')).length, 0)

    await app.stop()
    app = undefined

    // The disposer killed the child, so nothing is listening any more.
    await assert.rejects(fetch(`${endpoint}/healthz`))
  })

  // A spawned readproofd inherits the same default-deny as any other:
  // without filesystemRoots it refuses filesystem sources outright, and with
  // them it reads only inside what the config named.
  it('passes filesystemRoots to the child as --filesystem-root', async () => {
    const roots = join(tmpDir, 'policies')
    await mkdir(roots, { recursive: true })
    const policyPath = join(roots, 'refunds.md')
    await writeFile(policyPath, 'Products can be refunded within 30 days.\n')

    const rootedAddr = `127.0.0.1:${await freePort()}`
    const rooted = await startApp({
      spawn: true,
      readproofdPath: readproofdBin,
      dataDir: join(tmpDir, 'data-rooted'),
      addr: rootedAddr,
      filesystemRoots: [roots],
      sessionRuns: false,
    })
    try {
      const client = new Readproof({ endpoint: `http://${rootedAddr}` })
      await client.registerResource({
        uri: 'readproof://demo/policies/refunds',
        source: { kind: 'filesystem', filesystem: { path: policyPath } },
        policy: { strategy: 'require_fresh' },
      })
      const resolved = await client.resolve('readproof://demo/policies/refunds')
      assert.match(resolved.content, /refunded within 30 days/)

      // Outside the root, the same server refuses at registration.
      await assert.rejects(
        client.registerResource({
          uri: 'readproof://demo/etc/hosts',
          source: { kind: 'filesystem', filesystem: { path: '/etc/hosts' } },
          policy: { strategy: 'require_fresh' },
        }),
        /outside every configured/,
      )
    } finally {
      await rooted.stop()
    }

    // With no roots configured at all, filesystem sources are refused.
    const bareAddr = `127.0.0.1:${await freePort()}`
    const bare = await startApp({
      spawn: true,
      readproofdPath: readproofdBin,
      dataDir: join(tmpDir, 'data-bare'),
      addr: bareAddr,
      sessionRuns: false,
    })
    try {
      const client = new Readproof({ endpoint: `http://${bareAddr}` })
      await assert.rejects(
        client.registerResource({
          uri: 'readproof://demo/policies/refunds',
          source: { kind: 'filesystem', filesystem: { path: policyPath } },
          policy: { strategy: 'require_fresh' },
        }),
        /--filesystem-root/,
      )
    } finally {
      await bare.stop()
    }
  })

  it('fails to load loudly when the readproofd binary does not exist', async () => {
    await assert.rejects(
      startApp({
        spawn: true,
        readproofdPath: join(tmpDir, 'no-such-readproofd'),
        dataDir: join(tmpDir, 'data'),
        addr: `127.0.0.1:${await freePort()}`,
        sessionRuns: false,
      }),
      /could not start|exited before becoming healthy/,
    )
  })
})
