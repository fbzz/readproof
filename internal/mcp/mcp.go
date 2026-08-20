// Package mcp exposes a Ctx deployment as a Model Context Protocol server:
// registered resources become readable `ctx://` MCP resources, and the
// operations behind the CLI (resolve, tags, runs, manifests, diff, replay,
// evidence) become MCP tools.
//
// The server is written entirely against client.Client, exactly like every
// CLI command, so `ctx mcp` behaves identically whether it runs embedded
// over a local data directory or against a remote ctxd — the transport is
// chosen once, by the caller constructing the client, and nothing in here
// knows which one it got.
package mcp

import (
	"errors"
	"log/slog"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"ctx/internal/client"
	"ctx/internal/manifest"
	"ctx/internal/resource"
	"ctx/internal/snapshot"
	"ctx/internal/tag"
)

const (
	defaultServerName = "ctx"
	// defaultServerVersion tracks the exporter version in internal/evidence:
	// both identify the Ctx build that produced a record, and letting them
	// drift apart would make two answers to the same question.
	defaultServerVersion = "0.2.0-dev"

	// uriTemplate uses RFC 6570 reserved expansion ({+path}) because a Ctx
	// path is multi-segment ("policies/refunds") and may carry a tag ref
	// ("policies/refunds@prod"). Plain {path} matches neither, which would
	// make resources/read reject every real URI.
	uriTemplate = "ctx://{namespace}/{+path}"
)

// instructions is the MCP `initialize` instructions field: the one chance
// to tell a model what Ctx is before it starts calling tools. It explains
// the model (versioned + policy-governed + pinnable), the run lifecycle
// that produces a manifest id, and the fact that reading has side effects.
const instructions = `Ctx serves versioned, policy-governed documents (policies, specs, runbooks) instead of raw file or web fetches.

Every document has a stable URI, ctx://<namespace>/<path>, listed under resources. Reading one resolves it through its freshness policy: the policy decides whether the source is re-fetched or a cached snapshot is reused, and either way the read is recorded as an immutable snapshot with a content hash, a source revision, and an observation time. Append @<tag> to a URI (ctx://acme/policies/refunds@prod) to pin one exact, previously tagged snapshot: no fetch, no policy, always the same bytes.

When your answer depends on what you read, wrap the reads in a run: ctx_run_start, then ctx_run_mount for each URI, then ctx_run_commit. The commit returns a manifest id — the single identifier for "everything this run saw". Cite it in whatever you produce. Later, ctx_manifest inspects it, ctx_diff compares it against another run and explains why the bytes changed, ctx_replay reconstructs the exact bytes without touching the sources, and ctx_evidence_export produces a signed-ready audit bundle.

Reading or mounting a resource may create a new snapshot. That is intended: it is how Ctx records what you saw.`

// Options configures a server. The zero value is usable.
type Options struct {
	// Name and Version identify this server to MCP clients; they default
	// to "ctx" and the current Ctx version.
	Name    string
	Version string
	// MaxInlineBytes caps the content any one resource read or tool result
	// carries inline (default DefaultMaxInlineBytes). Content past the cap
	// is replaced by a truncation marker naming the content hash.
	MaxInlineBytes int
	// Logger receives SDK server activity. It must never write to stdout
	// on a stdio transport — stdout is the JSON-RPC channel.
	Logger *slog.Logger
}

func (o Options) name() string {
	if o.Name != "" {
		return o.Name
	}
	return defaultServerName
}

func (o Options) version() string {
	if o.Version != "" {
		return o.Version
	}
	return defaultServerVersion
}

func (o Options) maxInlineBytes() int {
	if o.MaxInlineBytes > 0 {
		return o.MaxInlineBytes
	}
	return DefaultMaxInlineBytes
}

// server holds the one dependency every handler needs. It is not exported:
// callers get an *mcpsdk.Server from NewServer and drive it with whatever
// transport they like.
type server struct {
	client client.Client
	opts   Options
}

// NewServer builds an MCP server over c. The caller owns c's lifetime (and
// must Close it) and owns the transport: run the result with
// (*mcpsdk.Server).Run over a StdioTransport, or connect it to an
// in-memory transport in tests.
func NewServer(c client.Client, opts Options) *mcpsdk.Server {
	s := &server{client: c, opts: opts}

	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:        opts.name(),
		Title:       "Ctx",
		Version:     opts.version(),
		Description: "Versioned, policy-governed, replayable context for AI agents",
	}, &mcpsdk.ServerOptions{
		Instructions: instructions,
		Logger:       opts.Logger,
	})

	s.registerResources(srv)
	s.registerTools(srv)
	return srv
}

// isNotFound recognizes a missing resource, snapshot, manifest, or tag
// from either client implementation. The local client returns the typed
// sentinels; the remote client flattens ctxd's 404 body into a plain
// error, so its message is all that is left to match on. This mirrors
// internal/evidence's isNotFound for the same reason.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, resource.ErrNotFound) ||
		errors.Is(err, snapshot.ErrNotFound) ||
		errors.Is(err, manifest.ErrNotFound) ||
		errors.Is(err, tag.ErrNotFound) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

// bareURI strips a trailing "@<tag>" from a reference. Tools that address
// a resource rather than one of its snapshots (history, tag management)
// accept a tagged URI for convenience — a model that just read
// ctx://a/b@prod should be able to paste the same string into ctx_history
// — but must operate on the resource itself.
func bareURI(raw string) (string, error) {
	uri, _, err := resource.SplitRef(raw)
	if err != nil {
		return "", err
	}
	return uri, nil
}
