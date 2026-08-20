package mcp

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Content encodings reported by the tool results, so a caller never has to
// guess whether it is holding text or base64.
const (
	encodingUTF8   = "utf-8"
	encodingBase64 = "base64"
)

// DefaultMaxInlineBytes caps how many bytes of resolved content any single
// resource read or tool result carries inline. A context resource can be a
// multi-megabyte spec; pushing all of it through a model's context window
// unasked is worse than handing back a truncation marker plus the content
// hash the caller can replay against.
const DefaultMaxInlineBytes = 1 << 20 // 1 MiB

// textualTypes are the content types Ctx serves as MCP text content. The
// list is deliberately narrow: anything outside it that is not obviously
// text (see isTextual) goes out as a base64 blob, since a model reading
// mangled binary is worse than a model told the bytes are binary.
var textualTypes = map[string]bool{
	"application/json":       true,
	"application/xml":        true,
	"application/yaml":       true,
	"application/x-yaml":     true,
	"application/javascript": true,
	"application/sql":        true,
	"application/toml":       true,
}

// textualSuffixes cover the structured-syntax suffix convention
// (RFC 6839), e.g. "application/vnd.api+json" or "image/svg+xml".
var textualSuffixes = []string{"+json", "+xml", "+yaml"}

// unknownTypes are the content types that carry no information at all —
// the filesystem adapter emits application/octet-stream for any extension
// it doesn't recognize, which is most of them. For these the bytes decide.
var unknownTypes = map[string]bool{
	"":                         true,
	"application/octet-stream": true,
	"binary/octet-stream":      true,
}

// isTextual reports whether content of the given type should be delivered
// as MCP text rather than a base64 blob. An unknown or absent content type
// falls back to sniffing the bytes: valid UTF-8 with no NUL byte is text.
func isTextual(contentType string, content []byte) bool {
	t := normalizeContentType(contentType)
	if strings.HasPrefix(t, "text/") || textualTypes[t] {
		return true
	}
	for _, suffix := range textualSuffixes {
		if strings.HasSuffix(t, suffix) {
			return true
		}
	}
	if !unknownTypes[t] {
		return false
	}
	// NUL is the cheap, reliable binary tell: valid UTF-8 alone would
	// accept a stray-byte-free binary blob as "text".
	return utf8.Valid(content) && !hasNUL(content)
}

func hasNUL(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	return false
}

// normalizeContentType strips any parameters ("; charset=utf-8") and
// lowercases what's left, so "Text/Markdown; charset=UTF-8" classifies the
// same as "text/markdown".
func normalizeContentType(contentType string) string {
	base, _, _ := strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(base))
}

// truncationMarker is appended to truncated text so a model reading the
// result can tell it is holding a prefix, and knows the one identifier
// (the content hash) that names the complete bytes.
func truncationMarker(shown, total int, contentHash string) string {
	return fmt.Sprintf("\n\n[ctx: content truncated — %d of %d bytes shown. "+
		"The full content is unchanged and content-addressed as %s; "+
		"use ctx_replay or ctx_evidence_export --with-content to obtain all of it.]\n",
		shown, total, contentHash)
}

// encodedContent is the wire-ready form of one resolved payload: exactly
// one of Text/Blob is set, and Truncated says whether it is a prefix.
type encodedContent struct {
	Text      string
	Blob      []byte
	Encoding  string
	Truncated bool
	// TotalBytes is the length of the complete content, which is what the
	// snapshot's hash covers — Text/Blob may be shorter.
	TotalBytes int
}

// encode caps content at maxBytes and splits it into the text or blob form
// the content type calls for. Truncation happens on a UTF-8 rune boundary
// for text so the result is never invalid UTF-8.
func encode(content []byte, contentType, contentHash string, maxBytes int) encodedContent {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxInlineBytes
	}
	out := encodedContent{TotalBytes: len(content)}
	truncated := len(content) > maxBytes
	capped := content
	if truncated {
		capped = content[:maxBytes]
	}
	out.Truncated = truncated

	if !isTextual(contentType, content) {
		out.Encoding = encodingBase64
		out.Blob = capped
		return out
	}

	out.Encoding = encodingUTF8
	if truncated {
		capped = trimToRuneBoundary(capped)
		out.Text = string(capped) + truncationMarker(len(capped), len(content), contentHash)
		return out
	}
	out.Text = string(capped)
	return out
}

// blobBase64 renders the (possibly truncated) blob for tool results, which
// carry base64 in a JSON string rather than MCP's []byte blob field.
func (e encodedContent) blobBase64() string {
	if e.Encoding != encodingBase64 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(e.Blob)
}

// trimToRuneBoundary drops a trailing partial UTF-8 rune left behind by a
// byte-count cut. At most 3 bytes are ever dropped.
func trimToRuneBoundary(b []byte) []byte {
	for len(b) > 0 {
		if r, size := utf8.DecodeLastRune(b); r != utf8.RuneError || size > 1 {
			return b
		}
		b = b[:len(b)-1]
	}
	return b
}
