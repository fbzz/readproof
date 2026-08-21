import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { after, before, describe, it } from 'node:test'
import { promisify } from 'node:util'

import { Readproof } from '@readproof/sdk'

import { childEnv } from '../src/spawn.js'
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

  // RP-22: the child used to inherit the parent's entire environment, which
  // in a harness process means every credential it happens to hold.
  it('gives the child a minimal environment, not the parent’s', () => {
    const env = childEnv({
      PATH: '/usr/bin',
      HOME: '/home/agent',
      TMPDIR: '/tmp',
      READPROOF_API_KEY: 'needed-by-readproofd',
      READPROOFD_HEADER_ENV_ALLOWLIST: 'GITHUB_TOKEN',
      AWS_SECRET_ACCESS_KEY: 'must-not-travel',
      OPENAI_API_KEY: 'must-not-travel',
      GITHUB_TOKEN: 'must-not-travel',
      OTEL_EXPORTER_OTLP_HEADERS: 'authorization=must-not-travel',
    })

    assert.deepEqual(Object.keys(env).sort(), [
      'HOME',
      'PATH',
      'READPROOFD_HEADER_ENV_ALLOWLIST',
      'READPROOF_API_KEY',
      'TMPDIR',
    ])
    for (const value of Object.values(env)) {
      assert.notEqual(value, 'must-not-travel')
    }
  })

  // …and end to end: a variable set in this process is genuinely not in the
  // child's, so a source header referencing it resolves to nothing instead of
  // sending it somewhere. The allow-list is set (and forwarded, being a
  // READPROOFD_ variable) so the refusal under test is the missing value, not
  // the header policy.
  it('does not leak a parent variable into a ${VAR} source header', async () => {
    process.env['SUPER_SECRET_MARKER'] = 'marker-value-that-must-not-travel'
    process.env['READPROOFD_HEADER_ENV_ALLOWLIST'] = 'SUPER_SECRET_MARKER'
    const addr = `127.0.0.1:${await freePort()}`
    const spawned = await startApp({
      spawn: true,
      readproofdPath: readproofdBin,
      dataDir: join(tmpDir, 'data-env'),
      addr,
      sessionRuns: false,
    })
    try {
      const client = new Readproof({ endpoint: `http://${addr}` })
      await client.registerResource({
        uri: 'readproof://demo/remote-doc',
        source: {
          kind: 'http',
          http: { url: 'https://docs.example.test/policy.md', headers: { Authorization: '${SUPER_SECRET_MARKER}' } },
        },
        policy: { strategy: 'require_fresh' },
      })
      await assert.rejects(client.resolve('readproof://demo/remote-doc'), (err: unknown) => {
        const message = (err as Error).message
        assert.match(message, /SUPER_SECRET_MARKER/)
        assert.match(message, /not set in readproofd's environment/)
        assert.doesNotMatch(message, /marker-value-that-must-not-travel/)
        return true
      })
    } finally {
      await spawned.stop()
      delete process.env['SUPER_SECRET_MARKER']
      delete process.env['READPROOFD_HEADER_ENV_ALLOWLIST']
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
