import assert from 'node:assert/strict'
import { describe, it } from 'node:test'

import { encodeContent } from '../src/content.js'
import { endpointForAddr } from '../src/spawn.js'
import { runIdBase } from '../src/session-runs.js'

const HASH = 'sha256:c8b0bb212e93151d720746e36ff3b7076727cb577614feafa0d61f168965aedb'

describe('inline content capping', () => {
  it('passes content through untouched below the cap', () => {
    const payload = encodeContent('Products can be refunded within 30 days.\n', HASH, 1024)
    assert.equal(payload.truncated, false)
    assert.equal(payload.text, 'Products can be refunded within 30 days.\n')
    assert.equal(payload.total_bytes, 41)
    assert.equal(payload.encoding, 'utf-8')
  })

  it('cuts on a rune boundary and names the content hash of the whole', () => {
    // 'é' is two bytes, so a 5-byte cap lands mid-character on the third one.
    const payload = encodeContent('ééé', HASH, 5)
    assert.equal(payload.truncated, true)
    assert.equal(payload.total_bytes, 6)
    assert.ok(payload.text.startsWith('éé'), `unexpected prefix: ${JSON.stringify(payload.text)}`)
    assert.ok(!payload.text.startsWith('ééé'))
    assert.ok(!payload.text.includes('�'), 'truncation must not produce a replacement character')
    assert.match(payload.text, /\[ctx: content truncated — 4 of 6 bytes shown\./)
    assert.ok(payload.text.includes(HASH), 'the marker must name the content hash')
  })

  it('reports the total length of the complete content, not the prefix', () => {
    const payload = encodeContent('x'.repeat(5000), HASH, 100)
    assert.equal(payload.total_bytes, 5000)
    assert.match(payload.text, /100 of 5000 bytes shown/)
  })
})

describe('spawned endpoint derivation', () => {
  it('reaches a wildcard listener at the loopback address', () => {
    assert.equal(endpointForAddr('127.0.0.1:18080'), 'http://127.0.0.1:18080')
    assert.equal(endpointForAddr(':8080'), 'http://127.0.0.1:8080')
    assert.equal(endpointForAddr('0.0.0.0:8080'), 'http://127.0.0.1:8080')
    assert.equal(endpointForAddr('[::1]:8080'), 'http://[::1]:8080')
  })
})

describe('session run ids', () => {
  it('derives a shell- and manifest-safe id from the session id', () => {
    assert.equal(runIdBase('01M0GQE8MCRQHJC8E1CY9AQGZT'), 'dsh-01M0GQE8MCRQHJC8E1CY9AQGZT')
    assert.equal(runIdBase('a b/c'), 'dsh-a-b-c')
    assert.equal(runIdBase(''), 'dsh-session')
  })
})
