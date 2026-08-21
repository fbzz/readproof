/**
 * Inline content capping, mirroring internal/mcp/content.go so a model
 * talking to this plugin sees the same payload shape and the same
 * truncation marker it would see through `readproof mcp`.
 *
 * One deliberate difference: the TypeScript SDK decodes a resolve's bytes to
 * a UTF-8 string before this layer ever sees them (sdk/typescript/src/
 * client.ts, `decodeResolveResponse`), so there is no base64 branch here and
 * `encoding` is always "utf-8". Binary resources are outside what this path
 * can represent faithfully; see the README's limitations.
 */

/** Mirrors ContentPayload in internal/mcp/types.go. */
export interface ContentPayload {
  encoding: 'utf-8'
  text: string
  truncated: boolean
  /** Always the length of the COMPLETE content, so a caller can tell how much is missing. */
  total_bytes: number
}

/**
 * The marker appended to truncated text. Kept byte-identical to
 * `truncationMarker` in internal/mcp/content.go: it names the content hash,
 * which is the one identifier that still resolves the complete bytes.
 */
function truncationMarker(shown: number, total: number, contentHash: string): string {
  return (
    `\n\n[readproof: content truncated — ${shown} of ${total} bytes shown. ` +
    `The full content is unchanged and content-addressed as ${contentHash}; ` +
    `use readproof_replay or readproof_evidence_export --with-content to obtain all of it.]\n`
  )
}

/**
 * Cap `content` at `maxBytes`, cutting on a UTF-8 rune boundary so the
 * result is never invalid UTF-8, and appending the marker when it is a
 * prefix.
 */
export function encodeContent(content: string, contentHash: string, maxBytes: number): ContentPayload {
  const buf = Buffer.from(content, 'utf-8')
  const limit = maxBytes > 0 ? maxBytes : 1024 * 1024
  if (buf.length <= limit) {
    return { encoding: 'utf-8', text: content, truncated: false, total_bytes: buf.length }
  }

  // Buffer#toString replaces a trailing partial rune with U+FFFD rather than
  // dropping it, so trim the raw bytes back to a boundary first.
  const kept = trimToRuneBoundary(buf.subarray(0, limit))
  const text = kept.toString('utf-8')
  return {
    encoding: 'utf-8',
    text: text + truncationMarker(kept.length, buf.length, contentHash),
    truncated: true,
    total_bytes: buf.length,
  }
}

/**
 * Total document bytes a bundle's entries embed under `content_b64`.
 *
 * `bytes` is the snapshot's own recorded size, so the total is expressed in
 * the same unit as `maxInlineBytes`; the base64 length is the fallback for an
 * entry that somehow carries content without a size.
 */
export function embeddedContentBytes(
  entries: ReadonlyArray<{ bytes?: number; content_b64?: string }>,
): number {
  let total = 0
  for (const entry of entries) {
    if (entry.content_b64 === undefined) continue
    total += typeof entry.bytes === 'number' ? entry.bytes : Buffer.byteLength(entry.content_b64, 'base64')
  }
  return total
}

/** Drop a trailing partial UTF-8 sequence. At most 3 bytes are ever dropped. */
function trimToRuneBoundary(buf: Buffer): Buffer {
  // Walk back over continuation bytes (10xxxxxx) to the lead byte of the
  // final sequence; a well-formed sequence is at most 4 bytes long.
  let lead = buf.length - 1
  while (lead >= 0 && lead > buf.length - 4) {
    const b = buf.at(lead)
    if (b === undefined || (b & 0b1100_0000) !== 0b1000_0000) break
    lead--
  }
  const b = lead >= 0 ? buf.at(lead) : undefined
  if (b === undefined) return buf
  // The final sequence fits inside the cap — nothing to drop.
  if (lead + sequenceLength(b) <= buf.length) return buf
  return buf.subarray(0, lead)
}

/** Bytes in the UTF-8 sequence a lead byte opens; 1 for ASCII and for invalid leads. */
function sequenceLength(lead: number): number {
  if ((lead & 0b1000_0000) === 0) return 1
  if ((lead & 0b1110_0000) === 0b1100_0000) return 2
  if ((lead & 0b1111_0000) === 0b1110_0000) return 3
  if ((lead & 0b1111_1000) === 0b1111_0000) return 4
  return 1
}
