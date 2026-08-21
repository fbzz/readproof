/**
 * Inline content capping, mirroring internal/mcp/content.go so a model
 * talking to this plugin sees the same payload shape and the same
 * truncation marker it would see through `ctx mcp`.
 *
 * One deliberate difference: the TypeScript SDK decodes a resolve's bytes to
 * a UTF-8 string before this layer ever sees them (sdk/typescript/src/
 * client.ts, `decodeResolveResponse`), so there is no base64 branch here and
 * `encoding` is always "utf-8". Binary resources are outside what this path
 * can represent faithfully; see the README's limitations.
 */
/** Mirrors ContentPayload in internal/mcp/types.go. */
export interface ContentPayload {
    encoding: 'utf-8';
    text: string;
    truncated: boolean;
    /** Always the length of the COMPLETE content, so a caller can tell how much is missing. */
    total_bytes: number;
}
/**
 * Cap `content` at `maxBytes`, cutting on a UTF-8 rune boundary so the
 * result is never invalid UTF-8, and appending the marker when it is a
 * prefix.
 */
export declare function encodeContent(content: string, contentHash: string, maxBytes: number): ContentPayload;
//# sourceMappingURL=content.d.ts.map