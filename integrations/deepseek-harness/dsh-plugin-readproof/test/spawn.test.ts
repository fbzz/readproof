import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { mkdir, mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { after, before, describe, it } from 'node:test'
import { promisify } from 'node:util'

import { field, freePort, list, repoRoot, startApp, type App } from './support.js'

const execFileAsync = promisify(execFile)

describe('spawn: true', () => {
  let tmpDir: string
  let ctxdBin: string
  let endpoint: string
  let app: App | undefined

  before(async () => {
    tmpDir = await mkdtemp(join(tmpdir(), 'dsh-plugin-ctx-spawn-'))
    await mkdir(join(tmpDir, 'data'))
    ctxdBin = join(tmpDir, 'ctxd')
    await execFileAsync('go', ['build', '-o', ctxdBin, './cmd/ctxd'], { cwd: repoRoot() })
    endpoint = `http://127.0.0.1:${await freePort()}`
  })

  after(async () => {
    await app?.stop()
    await rm(tmpDir, { recursive: true, force: true })
  })

  it('starts a private ctxd, serves tools from it, and kills it on disposal', async () => {
    const addr = endpoint.replace('http://', '')
    app = await startApp({
      spawn: true,
      ctxdPath: ctxdBin,
      dataDir: join(tmpDir, 'data'),
      addr,
      sessionRuns: false,
    })

    // The child answers: the plugin only finishes loading after /healthz does.
    assert.ok((await fetch(`${endpoint}/healthz`)).ok)
    // And the tools are wired to it — an empty data directory lists nothing.
    const value = await app.value('ctx_resources_list', {})
    assert.equal(list(field(value, 'resources')).length, 0)

    await app.stop()
    app = undefined

    // The disposer killed the child, so nothing is listening any more.
    await assert.rejects(fetch(`${endpoint}/healthz`))
  })

  it('fails to load loudly when the ctxd binary does not exist', async () => {
    await assert.rejects(
      startApp({
        spawn: true,
        ctxdPath: join(tmpDir, 'no-such-ctxd'),
        dataDir: join(tmpDir, 'data'),
        addr: `127.0.0.1:${await freePort()}`,
        sessionRuns: false,
      }),
      /could not start|exited before becoming healthy/,
    )
  })
})
