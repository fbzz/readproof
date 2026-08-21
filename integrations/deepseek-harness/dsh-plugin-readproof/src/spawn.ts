import { spawn, type ChildProcess } from 'node:child_process'
import { homedir } from 'node:os'
import { join } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'

import type { Config } from './config.js'

export interface SpawnedReadproofd {
  /** Base URL the SDK client should talk to. */
  endpoint: string
  /** Kills the child and resolves once it has actually exited. */
  stop: () => Promise<void>
}

/**
 * Start a private `readproofd` and wait until it answers `/healthz`.
 *
 * The caller owns the returned `stop`; it is registered as a Cordis effect
 * disposer so an HMR reload or an unload of the plugin takes the child with
 * it rather than leaking a listener on the configured port.
 */
export async function spawnReadproofd(config: Config, log: (message: string) => void): Promise<SpawnedReadproofd> {
  const dataDir = expandHome(config.dataDir)
  const endpoint = endpointForAddr(config.addr)

  const child = spawn(config.readproofdPath, readproofdArgs(config, dataDir), {
    // readproofd logs to stderr and speaks HTTP, so nothing needs its stdin; the
    // output is piped rather than inherited so it can be prefixed and does
    // not interleave into a CLI's own rendering.
    stdio: ['ignore', 'pipe', 'pipe'],
    env: childEnv(),
  })

  const forward = (chunk: Buffer): void => {
    const text = chunk.toString('utf-8').trimEnd()
    if (text.length > 0) log(`readproofd: ${text}`)
  }
  child.stdout?.on('data', forward)
  child.stderr?.on('data', forward)

  // A spawn failure (ENOENT for a readproofd that is not on PATH) arrives
  // asynchronously on 'error', so it has to be raced against the health
  // probe rather than caught around the spawn call.
  const exited = new Promise<never>((_resolve, reject) => {
    child.once('error', (err: Error) => reject(new Error(`readproof: could not start ${config.readproofdPath}: ${err.message}`)))
    child.once('exit', (code, signal) =>
      reject(new Error(`readproof: ${config.readproofdPath} exited before becoming healthy (code ${code}, signal ${signal})`)),
    )
  })

  try {
    await Promise.race([waitForHealth(endpoint, config.spawnTimeoutMs), exited])
  } catch (err) {
    await killChild(child)
    throw err
  }
  // The startup listeners above reject a promise nobody awaits any more; an
  // exit after this point is reported through the ordinary log instead.
  child.removeAllListeners('error')
  child.removeAllListeners('exit')
  child.once('exit', (code, signal) => log(`readproofd exited (code ${code}, signal ${signal})`))

  log(`spawned ${config.readproofdPath} on ${endpoint} (data-dir ${dataDir})`)
  return { endpoint, stop: () => killChild(child) }
}

/**
 * Variables the child needs in order to run at all: how to find its own
 * executable and libraries, and where to put temporary files. Windows spellings
 * included, because a missing SystemRoot breaks process creation there.
 */
const PROCESS_BASICS = ['PATH', 'Path', 'HOME', 'TMPDIR', 'TEMP', 'TMP', 'SystemRoot', 'USERPROFILE']

/**
 * The environment a spawned `readproofd` gets.
 *
 * Passing no `env` means inheriting the parent's entire environment — and a
 * `readproofd`'s environment is reachable through a `"${VAR}"` source header,
 * so every unrelated credential the harness happens to hold would sit one
 * resource registration away from an outbound request. (Server-side that now
 * needs `--header-env-allow` as well, but defence in depth is the point:
 * what the child never receives cannot be allow-listed by mistake.)
 *
 * Forwarded: the process basics above, plus `READPROOF*` / `READPROOFD*` —
 * readproofd's own configuration, including the API key it must have to
 * require one. Everything else, `OTEL_*` and cloud credentials alike, stops
 * here; configure the child through the plugin's own config instead.
 */
export function childEnv(parent: NodeJS.ProcessEnv = process.env): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = {}
  for (const name of PROCESS_BASICS) {
    const value = parent[name]
    if (value !== undefined) env[name] = value
  }
  for (const [name, value] of Object.entries(parent)) {
    if (value !== undefined && (name.startsWith('READPROOF_') || name.startsWith('READPROOFD_'))) {
      env[name] = value
    }
  }
  return env
}

/**
 * argv for the child, allow-list roots included.
 *
 * `readproofd` refuses filesystem sources unless it is given at least one
 * `--filesystem-root`, so a deployment that governs local documents has to
 * name their directory here. Each root is expanded like `dataDir`: the value
 * comes from a config file written by a human, not from a shell.
 */
export function readproofdArgs(config: Config, dataDir: string): string[] {
  const args = ['--addr', config.addr, '--data-dir', dataDir]
  for (const root of config.filesystemRoots ?? []) {
    if (root.trim() === '') continue
    args.push('--filesystem-root', expandHome(root))
  }
  return args
}

/** `:8080` and `0.0.0.0:8080` are reachable locally at 127.0.0.1. */
export function endpointForAddr(addr: string): string {
  const [rawHost, port] = splitHostPort(addr)
  const host = rawHost === '' || rawHost === '0.0.0.0' || rawHost === '::' ? '127.0.0.1' : rawHost
  // A bare IPv6 literal needs brackets to be a valid URL authority.
  const authority = host.includes(':') ? `[${host}]` : host
  return `http://${authority}:${port}`
}

function splitHostPort(addr: string): [string, string] {
  const idx = addr.lastIndexOf(':')
  if (idx < 0) return ['127.0.0.1', addr]
  return [addr.slice(0, idx).replace(/^\[|\]$/g, ''), addr.slice(idx + 1)]
}

/** Expand a leading `~` — a config file is written by a human, not a shell. */
export function expandHome(path: string): string {
  if (path === '~') return homedir()
  if (path.startsWith('~/')) return join(homedir(), path.slice(2))
  return path
}

async function waitForHealth(endpoint: string, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs
  let lastError = 'no attempt made'
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${endpoint}/healthz`)
      if (res.ok) return
      lastError = `status ${res.status}`
    } catch (err) {
      lastError = err instanceof Error ? err.message : String(err)
    }
    await delay(50)
  }
  throw new Error(`readproof: ${endpoint}/healthz did not answer within ${timeoutMs}ms: ${lastError}`)
}

async function killChild(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) return
  const exited = new Promise<void>((resolve) => child.once('exit', () => resolve()))
  child.kill('SIGTERM')
  // SIGKILL only if SIGTERM was ignored: readproofd has no graceful-shutdown hook
  // today, but escalating immediately would make one impossible to add.
  const timer = setTimeout(() => child.kill('SIGKILL'), 2_000)
  try {
    await exited
  } finally {
    clearTimeout(timer)
  }
}
