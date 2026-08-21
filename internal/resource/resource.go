package resource

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fbzz/readproof/internal/policy"
	"github.com/fbzz/readproof/internal/source"
)

// ErrNotFound is returned by Store.Get/UpdateCurrentSnapshot when a Resource
// isn't registered.
var ErrNotFound = errors.New("resource: not found")

// URI is a parsed readproof://<namespace>/<path> logical identifier.
type URI struct {
	Namespace string
	Path      string
}

// ParseURI parses a *bare* readproof://<namespace>/<path> identifier. It
// rejects anything carrying a trailing "@<ref>": "@" is reserved for tag
// refs, and silently folding one into the path would make
// `readproof://ns/p@prod` register or look up a resource literally named
// "p@prod". Callers handling a user-supplied reference that may carry a ref
// must run it through SplitRef first and parse the bare URI SplitRef
// returns.
func ParseURI(raw string) (URI, error) {
	const prefix = "readproof://"
	if !strings.HasPrefix(raw, prefix) {
		return URI{}, fmt.Errorf("resource: invalid uri %q: must start with %q", raw, prefix)
	}
	rest := strings.TrimPrefix(raw, prefix)
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return URI{}, fmt.Errorf("resource: invalid uri %q: expected readproof://<namespace>/<path>", raw)
	}
	if strings.Contains(rest, "@") {
		return URI{}, fmt.Errorf("resource: invalid uri %q: %q is reserved for tag refs (readproof://<namespace>/<path>@<tag>)", raw, "@")
	}
	return URI{Namespace: parts[0], Path: parts[1]}, nil
}

// SplitRef splits a possibly ref-bearing reference —
// readproof://<ns>/<path> or readproof://<ns>/<path>@<ref> — into its bare
// URI and ref. A bare URI returns ref "". Only the LAST "@" is treated as
// the ref delimiter; whatever precedes it must be a valid bare URI, so a
// stray second "@" is an error rather than a silently mangled path. This is
// the single entry point for user-supplied references (CLI args, API
// request bodies); everything downstream works with the bare URI plus an
// explicit ref, never with the combined string.
func SplitRef(raw string) (uri, ref string, err error) {
	const prefix = "readproof://"
	rest, ok := strings.CutPrefix(raw, prefix)
	if !ok {
		return "", "", fmt.Errorf("resource: invalid uri %q: must start with %q", raw, prefix)
	}
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		uri, ref = prefix+rest[:i], rest[i+1:]
		if ref == "" {
			return "", "", fmt.Errorf("resource: invalid reference %q: empty tag after %q", raw, "@")
		}
	} else {
		uri = raw
	}
	if _, err := ParseURI(uri); err != nil {
		return "", "", err
	}
	return uri, ref, nil
}

func (u URI) String() string {
	return "readproof://" + u.Namespace + "/" + u.Path
}

// Resource is the stable logical identity consumed by applications.
type Resource struct {
	URI          string
	Namespace    string
	Path         string
	SourceID     string
	SourceConfig source.Config
	PolicyID     string
	Policy       policy.Policy

	// CurrentSnapshotID is "" until the first successful resolve.
	CurrentSnapshotID string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Store is the single public port over a Resource's sources+policies+
// resources rows — callers never juggle three stores for one Resource.
type Store interface {
	Create(ctx context.Context, r Resource) error
	Get(ctx context.Context, uri string) (Resource, error)
	List(ctx context.Context) ([]Resource, error)
	UpdateCurrentSnapshot(ctx context.Context, uri, snapshotID string) error
}
