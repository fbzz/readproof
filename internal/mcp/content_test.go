package mcp

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestIsTextual(t *testing.T) {
	for _, tc := range []struct {
		name        string
		contentType string
		content     []byte
		want        bool
	}{
		{"markdown", "text/markdown", []byte("# hi"), true},
		{"markdown with charset", "Text/Markdown; charset=UTF-8", []byte("# hi"), true},
		{"json", "application/json", []byte(`{"a":1}`), true},
		{"structured syntax suffix", "application/vnd.api+json", []byte(`{"a":1}`), true},
		{"yaml", "application/yaml", []byte("a: 1"), true},
		{"svg is xml", "image/svg+xml", []byte("<svg/>"), true},
		{"png", "image/png", []byte("\x89PNG\r\n"), false},
		{"pdf", "application/pdf", []byte("%PDF-1.7"), false},
		// The filesystem adapter labels every unrecognized extension
		// application/octet-stream, so those have to be sniffed or most
		// plain-text documents would come back base64.
		{"octet-stream text", "application/octet-stream", []byte("plain enough\n"), true},
		{"octet-stream binary", "application/octet-stream", []byte{0x00, 0x01, 0x02}, false},
		{"empty type text", "", []byte("plain enough\n"), true},
		{"empty type invalid utf8", "", []byte{0xff, 0xfe, 0xfd}, false},
		{"empty content", "", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTextual(tc.contentType, tc.content); got != tc.want {
				t.Fatalf("isTextual(%q, %q) = %v, want %v", tc.contentType, tc.content, got, tc.want)
			}
		})
	}
}

func TestEncodeCapsInlineContent(t *testing.T) {
	// A multi-byte rune straddling the cut point is what makes a naive
	// byte-slice truncation produce invalid UTF-8.
	content := []byte(strings.Repeat("é", 100))
	const hash = "sha256:deadbeef"

	enc := encode(content, "text/plain", hash, 51)
	if !enc.Truncated {
		t.Fatalf("expected truncation of %d bytes at a 51-byte cap", len(content))
	}
	if enc.TotalBytes != len(content) {
		t.Fatalf("total_bytes = %d, want %d", enc.TotalBytes, len(content))
	}
	if !utf8.ValidString(enc.Text) {
		t.Fatalf("truncated text is not valid UTF-8")
	}
	if !strings.Contains(enc.Text, hash) || !strings.Contains(enc.Text, "truncated") {
		t.Fatalf("truncated text carries no marker naming the content hash: %q", enc.Text)
	}

	full := encode(content, "text/plain", hash, len(content))
	if full.Truncated || full.Text != string(content) {
		t.Fatalf("content at exactly the cap must pass through untouched")
	}
}

func TestEncodeBinaryAsBase64(t *testing.T) {
	content := []byte{0x89, 'P', 'N', 'G', 0x00, 0x01}

	enc := encode(content, "image/png", "sha256:deadbeef", DefaultMaxInlineBytes)
	if enc.Encoding != encodingBase64 {
		t.Fatalf("encoding = %q, want %q", enc.Encoding, encodingBase64)
	}
	if enc.Text != "" {
		t.Fatalf("binary content must not be delivered as text")
	}
	if got := enc.blobBase64(); got != base64.StdEncoding.EncodeToString(content) {
		t.Fatalf("blobBase64 = %q, want the base64 of the bytes", got)
	}
}
