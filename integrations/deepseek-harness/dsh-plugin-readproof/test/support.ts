/**
 * Test fixture: a real `readproofd`, built from this repository, with the
 * refund-agent policy registered, plus a real Cordis app with the harness
 * tool registry and this plugin mounted.
 *
 * Nothing here is mocked. The point of these tests is that the plugin talks
 * to the actual server and the actual harness; a mock would only prove the
 * plugin agrees with itself.
 */

import { execFile } from 'node:child_process'
import { spawn, type ChildProcess } from 'node:child_process'
import { copyFile, mkdir, mkdtemp, rm } from 'node:fs/promises'
import { createServer } from 'node:net'
import { existsSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'
import { fileURLToPath } from 'node:url'
import { promisify } from 'node:util'

import { Readproof } from '@readproof/sdk'
import { Context } from '@deepseek-ai/cordis'
import type { Agent } from '@deepseek-ai/dsh-agent'
import { CallId } from '@deepseek-ai/dsh-llm'
import { SessionId } from '@deepseek-ai/dsh-session'
import SystemPrompt from '@deepseek-ai/dsh-system-prompt'
import ToolRuntime, { type JsonValue, type ToolExecutionResult } from '@deepseek-ai/dsh-tools'

import * as readproofPlugin from '../src/index.js'
import type { Config } from '../src/config.js'

const execFileAsync = promisify(execFile)

export const DEMO_URI = 'readproof://demo/policies/refunds'

export interface Fixture {
  endpoint: string
  /** Path of the file the demo resource resolves from; edit it to change the source. */
  policyPath: string
  /** The `readproof` CLI built from this repository. */
  readproofBin: string
  /** The `readproofd` binary built from this repository. */
  readproofdBin: string
  /** A scratch directory that is removed on `stop()`. */
  tmpDir: string
  client: Readproof
  stop: () => Promise<void>
}

/** Walk up from this file until the Readproof repository root (the one with go.mod). */
export function repoRoot(): string {
  let dir = dirname(fileURLToPath(import.meta.url))
  for (let i = 0; i < 10; i++) {
    if (existsSync(join(dir, 'go.mod'))) return dir
    const parent = resolve(dir, '..')
    if (parent === dir) break
    dir = parent
  }
  throw new Error('could not find the repository root (no go.mod above this test file)')
}

/**
 * Build `readproofd` and `readproof`, start `readproofd` on a free port over a fresh data
 * directory, and register the refund-agent policy against a copy of the
 * fixture (a copy, so a test can edit it without dirtying the repository).
 */
export async function startFixture(): Promise<Fixture> {
  const root = repoRoot()
  const tmpDir = await mkdtemp(join(tmpdir(), 'dsh-plugin-readproof-'))
  const binDir = join(tmpDir, 'bin')
  const dataDir = join(tmpDir, 'data')
  const policyDir = join(tmpDir, 'policies')
  await Promise.all([mkdir(binDir), mkdir(dataDir), mkdir(policyDir)])

  const readproofdBin = join(binDir, 'readproofd')
  const readproofBin = join(binDir, 'readproof')
  await execFileAsync('go', ['build', '-o', readproofdBin, './cmd/readproofd'], { cwd: root })
  await execFileAsync('go', ['build', '-o', readproofBin, './cmd/readproof'], { cwd: root })

  const policyPath = join(policyDir, 'refunds.md')
  await copyFile(join(root, 'examples', 'refund-agent', 'policies', 'refunds.md'), policyPath)

  const port = await freePort()
  const endpoint = `http://127.0.0.1:${port}`
  const child = spawn(readproofdBin, ['--addr', `127.0.0.1:${port}`, '--data-dir', dataDir], {
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  const logs: string[] = []
  child.stdout?.on('data', (c: Buffer) => logs.push(c.toString('utf-8')))
  child.stderr?.on('data', (c: Buffer) => logs.push(c.toString('utf-8')))

  try {
    await waitForHealth(endpoint, 15_000)
  } catch (err) {
    child.kill('SIGKILL')
    await rm(tmpDir, { recursive: true, force: true })
    throw new Error(`${err instanceof Error ? err.message : String(err)}\nreadproofd output:\n${logs.join('')}`)
  }

  const client = new Readproof({ endpoint })
  await client.registerResource({
    uri: DEMO_URI,
    source: { kind: 'filesystem', filesystem: { path: policyPath } },
    policy: { strategy: 'require_fresh' },
  })

  return {
    endpoint,
    policyPath,
    readproofBin,
    readproofdBin,
    tmpDir,
    client,
    stop: async () => {
      await stopChild(child)
      await rm(tmpDir, { recursive: true, force: true })
    },
  }
}

export interface App {
  ctx: Context
  /** Call one tool the way the agent loop would, optionally on a session's behalf. */
  call: (name: string, args: Record<string, JsonValue>, agent?: Agent) => Promise<ToolExecutionResult>
  /** The canonical JSON value of a successful call; throws with the tool's message on failure. */
  value: (name: string, args: Record<string, JsonValue>, agent?: Agent) => Promise<JsonValue>
  toolNames: () => string[]
  stop: () => Promise<void>
}

/**
 * Compose the smallest app that can execute a tool: the system prompt
 * registry (which `@deepseek-ai/dsh-tools` injects), the tool registry, and
 * this plugin. No model and no agent loop are involved — `ctx.tools.execute`
 * drives the same pipeline the loop drives.
 */
export async function startApp(config: Partial<Config>): Promise<App> {
  const ctx = new Context()
  await ctx.plugin(SystemPrompt)
  await ctx.plugin(ToolRuntime)
  const fiber = await ctx.plugin(readproofPlugin, config)
  // `await ctx.plugin(...)` settles once loading finished; `await()` rethrows
  // a startup error instead of leaving the fiber quietly FAILED.
  await fiber.await()

  let callCounter = 0
  const call = (name: string, args: Record<string, JsonValue>, agent?: Agent): Promise<ToolExecutionResult> =>
    ctx.tools.execute({
      callId: CallId(`test-${++callCounter}`),
      name,
      arguments: args,
      signal: new AbortController().signal,
      ...(agent ? { agent } : {}),
    })

  return {
    ctx,
    call,
    async value(name, args, agent) {
      const result = await call(name, args, agent)
      if (result.isError) throw new Error(`${name} failed: ${renderContent(result)}`)
      return result.value
    },
    toolNames: () => ctx.tools.schemas().map((schema) => schema.name),
    stop: async () => {
      await fiber.dispose()
    },
  }
}

/**
 * A stand-in for the live agent the loop would pass.
 *
 * On the native execution path the registry treats the agent as an opaque
 * scope key (a WeakMap key in `@deepseek-ai/dsh-scope`) and this plugin reads
 * only `agent.id`, so an object with an id is enough. Building a real Agent
 * would drag in the agent loop, an LLM adapter, and a session store — none of
 * which this plugin touches.
 */
export function fakeAgent(id: string): Agent {
  return { id: SessionId(id) } as unknown as Agent
}

/** Join a tool result's text blocks — what the model would read. */
export function renderContent(result: ToolExecutionResult): string {
  return result.content.map((block) => (block.type === 'text' ? block.text : '')).join('\n')
}

/** Read a property off a tool's canonical value without casting. */
export function field(value: JsonValue | undefined, key: string): JsonValue | undefined {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return undefined
  return value[key]
}

export function str(value: JsonValue | undefined): string {
  if (typeof value !== 'string') throw new Error(`expected a string, got ${JSON.stringify(value)}`)
  return value
}

export function list(value: JsonValue | undefined): JsonValue[] {
  if (!Array.isArray(value)) throw new Error(`expected an array, got ${JSON.stringify(value)}`)
  return value
}

export async function freePort(): Promise<number> {
  return new Promise((res, rej) => {
    const server = createServer()
    server.once('error', rej)
    server.listen(0, '127.0.0.1', () => {
      const address = server.address()
      if (address === null || typeof address === 'string') {
        server.close()
        rej(new Error('could not determine a free port'))
        return
      }
      const { port } = address
      server.close(() => res(port))
    })
  })
}

async function waitForHealth(endpoint: string, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs
  let last = 'no attempt made'
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${endpoint}/healthz`)
      if (res.ok) return
      last = `status ${res.status}`
    } catch (err) {
      last = err instanceof Error ? err.message : String(err)
    }
    await delay(50)
  }
  throw new Error(`${endpoint}/healthz did not answer within ${timeoutMs}ms: ${last}`)
}

async function stopChild(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) return
  const exited = new Promise<void>((res) => child.once('exit', () => res()))
  child.kill('SIGTERM')
  const timer = setTimeout(() => child.kill('SIGKILL'), 2_000)
  try {
    await exited
  } finally {
    clearTimeout(timer)
  }
}
