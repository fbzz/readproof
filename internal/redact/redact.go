// Package redact identifies and masks header values that commonly carry
// credentials, so they never round-trip back out through API responses,
// `readproof inspect`, or `readproof resource list` — even if an operator
// pasted a raw secret instead of using the "${VAR}" environment-reference
// form the http source adapter resolves at fetch time (see
// internal/source/http).
package redact

import "strings"

const Placeholder = "[REDACTED]"

var sensitiveHeaderNames = map[string]bool{
	"authorization":       true,
	"cookie":              true,
	"set-cookie":          true,
	"proxy-authorization": true,
}

var sensitiveSubstrings = []string{"token", "key", "secret", "password", "credential", "auth"}

// IsSensitiveHeader reports whether a header name commonly carries a
// credential, by exact well-known name or by substring heuristic (e.g.
// "X-Api-Key", "X-Auth-Token").
func IsSensitiveHeader(name string) bool {
	lower := strings.ToLower(name)
	if sensitiveHeaderNames[lower] {
		return true
	}
	for _, s := range sensitiveSubstrings {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// Headers returns a copy of headers with sensitive values replaced by
// Placeholder. Non-sensitive entries pass through unchanged.
func Headers(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		if IsSensitiveHeader(k) {
			out[k] = Placeholder
		} else {
			out[k] = v
		}
	}
	return out
}
