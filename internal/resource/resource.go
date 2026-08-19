package resource

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"ctx/internal/policy"
	"ctx/internal/source"
)

// ErrNotFound is returned by Store.Get/UpdateCurrentSnapshot when a Resource
// isn't registered.
var ErrNotFound = errors.New("resource: not found")

// URI is a parsed ctx://<namespace>/<path> logical identifier.
type URI struct {
	Namespace string
	Path      string
}

func ParseURI(raw string) (URI, error) {
	const prefix = "ctx://"
	if !strings.HasPrefix(raw, prefix) {
		return URI{}, fmt.Errorf("resource: invalid uri %q: must start with %q", raw, prefix)
	}
	rest := strings.TrimPrefix(raw, prefix)
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return URI{}, fmt.Errorf("resource: invalid uri %q: expected ctx://<namespace>/<path>", raw)
	}
	return URI{Namespace: parts[0], Path: parts[1]}, nil
}

func (u URI) String() string {
	return "ctx://" + u.Namespace + "/" + u.Path
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
