package mcp

import (
	"context"
	"fmt"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"ctx/internal/policy"
	"ctx/internal/resolver"
	"ctx/internal/resource"
	"ctx/internal/source"
)

// methodResourcesList is the JSON-RPC method the list middleware claims.
const methodResourcesList = "resources/list"

// registerResources wires the resource half of the server.
//
// The listing is served by receiving middleware rather than by
// (*mcpsdk.Server).AddResource because Ctx's resource set is not static:
// another process can register a resource against the same store while
// this server is running, and a listing assembled at startup would go
// stale silently. Reads are served by a single resource template, which is
// also what makes `ctx://ns/path@tag` readable — the SDK's read path only
// accepts a URI that a registered resource or template matches, and no
// static registration can enumerate every tag.
func (s *server) registerResources(srv *mcpsdk.Server) {
	srv.AddResourceTemplate(&mcpsdk.ResourceTemplate{
		Name:        "ctx-resource",
		Title:       "Ctx resource",
		URITemplate: uriTemplate,
		Description: "Any registered Ctx resource, addressed as ctx://<namespace>/<path>. " +
			"Append @<tag> (ctx://acme/policies/refunds@prod) to read exactly the snapshot that tag names, " +
			"with no source fetch and no policy evaluation. Reading resolves through the resource's " +
			"freshness policy and may record a new snapshot.",
	}, s.readResource)

	srv.AddReceivingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			if method != methodResourcesList {
				return next(ctx, method, req)
			}
			return s.listResources(ctx)
		}
	})
}

// listResources projects every registered Ctx resource into an MCP
// resource. The whole set is returned in one page: Ctx namespaces hold
// curated documents, not a filesystem, so paginating would cost a
// round-trip per page for no benefit.
func (s *server) listResources(ctx context.Context) (*mcpsdk.ListResourcesResult, error) {
	resources, err := s.client.ListResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcp: list resources: %w", err)
	}

	out := &mcpsdk.ListResourcesResult{
		// The SDK only applies the protocol's cache defaults to results it
		// builds itself, and this one bypasses that path. TTL 0 is also
		// the honest answer: another process can register a resource at
		// any moment, so a cached listing is stale the instant it lands.
		Cacheable: mcpsdk.Cacheable{TTLMs: 0, CacheScope: "public"},
		Resources: make([]*mcpsdk.Resource, 0, len(resources)),
	}
	for _, r := range resources {
		entry := &mcpsdk.Resource{
			URI:         r.URI,
			Name:        r.Path,
			Title:       r.URI,
			Description: describeResource(r),
			Meta:        resourceMeta(r),
		}
		// The MIME type and size are properties of the bytes currently
		// stored, not of the resource, so they exist only once something
		// has been resolved. A failure to load the snapshot is not worth
		// failing the whole listing over: the resource is still readable.
		if r.CurrentSnapshotID != "" {
			if snap, err := s.client.GetSnapshot(ctx, r.CurrentSnapshotID); err == nil {
				entry.MIMEType = normalizeContentType(snap.ContentType)
				entry.Size = snap.Bytes
			}
		}
		out.Resources = append(out.Resources, entry)
	}
	return out, nil
}

// readResource resolves the requested reference and returns its bytes.
// Policy and any "@<tag>" suffix are honored by client.Resolve, so an MCP
// read is exactly the same operation as `ctx get` — including its side
// effect of recording a snapshot when the policy says to fetch.
func (s *server) readResource(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
	uri := req.Params.URI

	result, err := s.client.Resolve(ctx, uri)
	if err != nil {
		if isNotFound(err) {
			// The SDK maps this to the protocol's resource-not-found
			// error; anything else would surface as a generic failure.
			return nil, mcpsdk.ResourceNotFoundError(uri)
		}
		return nil, fmt.Errorf("mcp: read %s: %w", uri, err)
	}

	enc := encode(result.Content, result.Snapshot.ContentType, result.Snapshot.ContentHash, s.opts.maxInlineBytes())
	contents := &mcpsdk.ResourceContents{
		URI:      uri,
		MIMEType: normalizeContentType(result.Snapshot.ContentType),
		Meta:     readMeta(result, enc),
	}
	if enc.Encoding == encodingBase64 {
		contents.Blob = enc.Blob
	} else {
		contents.Text = enc.Text
	}
	return &mcpsdk.ReadResourceResult{Contents: []*mcpsdk.ResourceContents{contents}}, nil
}

// readMeta is the per-content `_meta` block: the provenance that turns a
// blob of text into a citable observation. The SDK exposes a `_meta` slot
// on ResourceContents itself, so it travels with the bytes it describes
// rather than with the response as a whole.
func readMeta(result resolver.ResolveResult, enc encodedContent) mcpsdk.Meta {
	meta := mcpsdk.Meta{
		"uri":                result.Snapshot.ResourceURI,
		"ref":                result.Ref,
		"snapshot_id":        result.Snapshot.SnapshotID,
		"content_hash":       result.Snapshot.ContentHash,
		"source_revision":    result.Snapshot.SourceRevision,
		"observed_at":        formatTime(result.Snapshot.ObservedAt),
		"decision":           result.Decision.String(),
		"materialization_id": result.Materialization.MaterializationID,
		"content_type":       result.Snapshot.ContentType,
		"bytes":              result.Snapshot.Bytes,
	}
	// Only present when true, so its absence can't be read as "the bytes
	// above are definitely complete" by a client that ignores the field.
	if enc.Truncated {
		meta["truncated"] = true
	}
	return meta
}

// resourceMeta carries the resource's definition alongside its listing
// entry, so a model can see *why* a read behaves the way it does (which
// source, which freshness policy) without a second call. Source config is
// redacted here for the same reason evidence bundles are: this is the
// projection that leaves the process.
func resourceMeta(r resource.Resource) mcpsdk.Meta {
	return mcpsdk.Meta{
		"namespace":           r.Namespace,
		"path":                r.Path,
		"source":              sourceInfo(r.SourceConfig),
		"policy":              policyInfo(r.Policy),
		"current_snapshot_id": r.CurrentSnapshotID,
	}
}

// describeResource is the one-line hint MCP clients show to the model. It
// leads with the two facts that change how the resource behaves — where
// the bytes come from and how fresh they are required to be — then names
// the concrete origin.
func describeResource(r resource.Resource) string {
	desc := fmt.Sprintf("%s · %s", r.SourceConfig.Kind, policyLabel(r.Policy))
	if loc := sourceLocator(r.SourceConfig); loc != "" {
		desc += " — " + loc
	}
	return desc
}

func policyLabel(p policy.Policy) string {
	switch p.Strategy {
	case policy.StrategyAllowStale:
		if p.MaxAge > 0 {
			return fmt.Sprintf("allow_stale(max_age=%s)", p.MaxAge)
		}
		return "allow_stale"
	case policy.StrategyPinned:
		return fmt.Sprintf("pinned(%s)", p.PinnedSnapshotID)
	default:
		return string(p.Strategy)
	}
}

// sourceLocator renders the origin compactly. HTTP headers are never part
// of it — they are the field that carries credentials, and the redacted
// copy in `_meta` is the only place they appear at all.
func sourceLocator(cfg source.Config) string {
	switch {
	case cfg.Filesystem != nil:
		return cfg.Filesystem.Path
	case cfg.GitHub != nil:
		gh := cfg.GitHub
		loc := fmt.Sprintf("%s/%s:%s", gh.Owner, gh.Repo, gh.Path)
		if gh.Ref != "" {
			loc += "@" + gh.Ref
		}
		return loc
	case cfg.HTTP != nil:
		return cfg.HTTP.URL
	default:
		return ""
	}
}

// formatTime renders a timestamp as RFC3339 and a zero time as "", so a
// consumer never has to special-case the year 1.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
