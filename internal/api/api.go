// Package api exposes the Readproof resolution pipeline over HTTP: the wire
// contract every handler here implements is defined in internal/wire, and
// shared by internal/client/remote on the client side.
package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fbzz/readproof/internal/app"
	"github.com/fbzz/readproof/internal/diff"
	"github.com/fbzz/readproof/internal/manifest"
	"github.com/fbzz/readproof/internal/resource"
	"github.com/fbzz/readproof/internal/run"
	"github.com/fbzz/readproof/internal/snapshot"
	"github.com/fbzz/readproof/internal/source"
	"github.com/fbzz/readproof/internal/tag"
	"github.com/fbzz/readproof/internal/wire"
)

// Options configures the HTTP API.
type Options struct {
	// APIKey, when non-empty, requires every request except /healthz to
	// carry a matching "Authorization: Bearer <APIKey>" header. Empty
	// (the default) leaves the API unauthenticated — fine for local
	// development, not for exposing readproofd beyond localhost.
	APIKey string
}

// NewHandler builds the full Readproof HTTP API over an already-opened App.
func NewHandler(a *app.App, opts Options) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/resources", handleRegisterResource(a))
	mux.HandleFunc("GET /v1/resources", handleListResources(a))
	mux.HandleFunc("GET /v1/resources/get", handleGetResource(a))
	mux.HandleFunc("GET /v1/resources/history", handleHistory(a))
	mux.HandleFunc("GET /v1/snapshots", handleGetSnapshot(a))
	mux.HandleFunc("PUT /v1/tags", handleSetTag(a))
	mux.HandleFunc("GET /v1/tags", handleListTags(a))
	mux.HandleFunc("DELETE /v1/tags", handleDeleteTag(a))
	mux.HandleFunc("POST /v1/resolve", handleResolve(a))
	mux.HandleFunc("POST /v1/runs", handleRunStart(a))
	mux.HandleFunc("POST /v1/runs/mount", handleRunMount(a))
	mux.HandleFunc("POST /v1/runs/commit", handleRunCommit(a))
	mux.HandleFunc("GET /v1/manifests", handleGetManifest(a))
	mux.HandleFunc("GET /v1/diff", handleDiff(a))
	mux.HandleFunc("GET /v1/replay", handleReplay(a))
	mux.HandleFunc("GET /healthz", handleHealthz)

	if opts.APIKey == "" {
		return mux
	}
	return requireAPIKey(opts.APIKey, mux)
}

// requireAPIKey wraps next so every request (except /healthz, which
// container/orchestrator healthchecks need to reach unauthenticated) must
// carry "Authorization: Bearer <key>" matching key exactly.
func requireAPIKey(key string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		const prefix = "Bearer "
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, prefix) || !constantTimeEqual(strings.TrimPrefix(auth, prefix), key) {
			writeError(w, http.StatusUnauthorized, errors.New("missing or invalid API key"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func handleRegisterResource(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req wire.RegisterResourceRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		parsed, err := resource.ParseURI(req.URI)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		res := resource.Resource{
			URI:          req.URI,
			Namespace:    parsed.Namespace,
			Path:         parsed.Path,
			SourceConfig: wire.SourceFromWire(req.Source),
			Policy:       wire.PolicyFromWire(req.Policy),
		}
		// A source definition the server's policy refuses is refused here,
		// not three calls later on the first resolve: an operator who
		// registers a filesystem source outside every --filesystem-root
		// should learn that from the registration, with the flag named. The
		// adapter still enforces it at fetch time — a row can predate the
		// policy that now refuses it.
		if err := a.Sources.Validate(res.SourceConfig); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := a.Resources.Create(r.Context(), res); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		created, err := a.Resources.Get(r.Context(), req.URI)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, wire.ResourceToWireRedacted(created))
	}
}

func handleListResources(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resources, err := a.Resources.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		out := make([]wire.ResourceWire, len(resources))
		for i, res := range resources {
			out[i] = wire.ResourceToWireRedacted(res)
		}
		writeJSON(w, http.StatusOK, wire.ResourceListResponse{Resources: out})
	}
}

func handleGetResource(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uri := r.URL.Query().Get("uri")
		if uri == "" {
			writeError(w, http.StatusBadRequest, errors.New("missing required query parameter: uri"))
			return
		}
		res, err := a.Resources.Get(r.Context(), uri)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, wire.ResourceToWireRedacted(res))
	}
}

func handleHistory(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uri := r.URL.Query().Get("uri")
		if uri == "" {
			writeError(w, http.StatusBadRequest, errors.New("missing required query parameter: uri"))
			return
		}
		history, err := a.Snapshots.ListByResource(r.Context(), uri)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		out := make([]wire.SnapshotWire, len(history))
		for i, s := range history {
			out[i] = wire.SnapshotToWire(s)
		}
		writeJSON(w, http.StatusOK, wire.HistoryResponse{Snapshots: out})
	}
}

func handleGetSnapshot(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, errors.New("missing required query parameter: id"))
			return
		}
		snap, err := a.Snapshots.Get(r.Context(), id)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, wire.SnapshotToWire(snap))
	}
}

func handleSetTag(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req wire.SetTagRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.URI == "" || req.Tag == "" || req.SnapshotID == "" {
			writeError(w, http.StatusBadRequest, errors.New("uri, tag, and snapshot_id are all required"))
			return
		}
		if err := a.Tags.Set(r.Context(), tag.Tag{ResourceURI: req.URI, Name: req.Tag, SnapshotID: req.SnapshotID}); err != nil {
			writeDomainError(w, err)
			return
		}
		t, err := a.Tags.Get(r.Context(), req.URI, req.Tag)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, wire.TagToWire(t))
	}
}

func handleListTags(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uri := r.URL.Query().Get("uri")
		if uri == "" {
			writeError(w, http.StatusBadRequest, errors.New("missing required query parameter: uri"))
			return
		}
		tags, err := a.Tags.List(r.Context(), uri)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		out := make([]wire.TagWire, len(tags))
		for i, t := range tags {
			out[i] = wire.TagToWire(t)
		}
		writeJSON(w, http.StatusOK, wire.TagListResponse{Tags: out})
	}
}

func handleDeleteTag(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uri := r.URL.Query().Get("uri")
		name := r.URL.Query().Get("tag")
		if uri == "" || name == "" {
			writeError(w, http.StatusBadRequest, errors.New("missing required query parameters: uri, tag"))
			return
		}
		if err := a.Tags.Delete(r.Context(), uri, name); err != nil {
			writeDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleResolve(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req wire.ResolveRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		result, err := a.Resolver.Resolve(r.Context(), req.URI)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, wire.ResolveResultToWire(result, time.Now().UTC()))
	}
}

func handleRunStart(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req wire.RunStartRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := a.RunBuilder.Start(r.Context(), req.RunID); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleRunMount(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req wire.RunMountRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		mounts, err := a.Runs.ListMounts(r.Context(), req.RunID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		position := len(mounts)

		result, err := a.RunBuilder.Mount(r.Context(), req.RunID, req.URI)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, wire.RunMountResponse{
			Position: position,
			Resolve:  wire.ResolveResultToWire(result, time.Now().UTC()),
		})
	}
}

func handleRunCommit(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req wire.RunCommitRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		man, err := a.RunBuilder.Commit(r.Context(), req.RunID)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, wire.ManifestToWire(man))
	}
}

func handleGetManifest(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("target")
		if target == "" {
			writeError(w, http.StatusBadRequest, errors.New("missing required query parameter: target"))
			return
		}
		man, err := a.Manifests.GetByIDOrRun(r.Context(), target)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, wire.ManifestToWire(man))
	}
}

func handleDiff(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetA := r.URL.Query().Get("a")
		targetB := r.URL.Query().Get("b")
		if targetA == "" || targetB == "" {
			writeError(w, http.StatusBadRequest, errors.New("missing required query parameters: a, b"))
			return
		}
		manA, err := a.Manifests.GetByIDOrRun(r.Context(), targetA)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		manB, err := a.Manifests.GetByIDOrRun(r.Context(), targetB)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		result, err := diff.Compute(r.Context(), manA, manB, a.Blobs, a.Snapshots)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, wire.DiffResultToWire(result))
	}
}

func handleReplay(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("target")
		if target == "" {
			writeError(w, http.StatusBadRequest, errors.New("missing required query parameter: target"))
			return
		}
		result, err := a.Replayer.Replay(r.Context(), target)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, wire.ReplayResultToWire(result))
	}
}

// writeDomainError maps well-known domain sentinel errors to their proper
// HTTP status; everything else is a 500.
func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, resource.ErrNotFound),
		errors.Is(err, snapshot.ErrNotFound),
		errors.Is(err, manifest.ErrNotFound),
		errors.Is(err, run.ErrNotFound),
		errors.Is(err, tag.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, run.ErrAlreadyCommitted):
		// The run exists and the request is well-formed; it just lost the
		// race (or repeated itself) against the commit that already
		// produced this run's one manifest.
		writeError(w, http.StatusConflict, err)
	case source.IsDenied(err):
		// The server's source policy refused this resource — a filesystem
		// path outside every allow-listed root, a private target address, an
		// environment variable that may not be expanded. Nothing failed and
		// nothing leaked, so this is a 400 carrying the reason (and the flag
		// that relaxes it), not a generic 500.
		writeError(w, http.StatusBadRequest, err)
	case errors.Is(err, tag.ErrInvalidName), errors.Is(err, tag.ErrSnapshotMismatch):
		// The caller sent a well-formed request naming something that can
		// never be valid — a bad tag name, or a snapshot of another
		// resource — so this is a 400, not a 500.
		writeError(w, http.StatusBadRequest, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

// MaxRequestBytes caps the request body every JSON endpoint will read. No
// Readproof request carries content — resources are registered by
// reference, and bytes only ever travel outward — so the largest legitimate
// body is a resource definition with a handful of headers. 1 MiB is orders
// of magnitude above that and keeps an unauthenticated peer from spending
// the server's memory a request at a time.
const MaxRequestBytes int64 = 1 << 20

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBytes)

	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("request body exceeds %d bytes", MaxRequestBytes))
			return false
		}
		writeError(w, http.StatusBadRequest, errors.New("invalid request body: "+err.Error()))
		return false
	}
	// A second JSON value after the first is a malformed request, not a
	// document to ignore: accepting it would let two callers disagree about
	// what a request said.
	if dec.More() {
		writeError(w, http.StatusBadRequest, errors.New("invalid request body: unexpected data after the JSON value"))
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, wire.ErrorResponse{Error: err.Error()})
}
