// Package version is the single source of truth for the Readproof version.
//
// Everything that stamps a version into something durable — the evidence
// bundle's exporter field, the MCP server info, `readproof version` — reads
// it from here. A record that says "produced by readproof 0.3.1" is only
// useful if every producer agrees on what 0.3.1 means, and separate
// literals drift.
package version

// Version is the released version of this build. Bump it here, and only
// here, at release time.
const Version = "0.3.2"

// Commit is the short git SHA this binary was built from. It is empty for
// an ordinary `go build`/`go test` and set at release time with:
//
//	go build -ldflags "-X github.com/fbzz/readproof/internal/version.Commit=$(git rev-parse --short HEAD)" ./cmd/readproof
//
// Keeping it out of Version means the version a bundle records stays
// reproducible across builds of the same source.
var Commit string

// String is the human-facing build identity: "0.3.1", or "0.3.1+a1b2c3d"
// when the commit was stamped in.
func String() string {
	if Commit == "" {
		return Version
	}
	return Version + "+" + Commit
}
