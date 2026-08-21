// Package tag defines named, movable pointers from a Resource to one of its
// Snapshots — `(resource_uri, tag) -> snapshot_id`. A tag is the only
// mutable thing in the model: the Snapshot it names never changes, but the
// tag can be re-pointed at a different Snapshot at any time. Resolving
// `readproof://ns/path@<tag>` therefore delivers exactly the bytes that tag
// names, with no source fetch and no policy evaluation.
package tag

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// ErrNotFound is returned by Store.Get/Delete when no such tag exists.
var ErrNotFound = errors.New("tag: not found")

// ErrSnapshotMismatch is returned by Store.Set when the snapshot exists but
// was observed for a different Resource. A tag names a snapshot *of its own
// resource*; a cross-resource pointer would let `readproof://a/x@prod`
// deliver content that never belonged to `readproof://a/x`.
var ErrSnapshotMismatch = errors.New("tag: snapshot belongs to a different resource")

// ErrInvalidName is returned by ValidateName (and therefore by Store.Set)
// for a tag name that breaks the naming rules.
var ErrInvalidName = errors.New("tag: invalid tag name")

// Tag is a named pointer from a Resource to one of its Snapshots.
type Tag struct {
	ResourceURI string
	Name        string
	SnapshotID  string
	UpdatedAt   time.Time
}

// namePattern is the single definition of what a tag name may look like:
// an ASCII alphanumeric first character, then alphanumerics/dot/dash/
// underscore, 1-64 characters total. It deliberately excludes "@" and "/"
// so a tag can never be confused with the URI it hangs off — see
// resource.SplitRef.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidateName is the one place the tag naming rules live. Every write path
// (Store.Set in both storage backends, and so the API and CLI above them)
// goes through it, so no backend can drift from another.
func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("%w %q: must match %s", ErrInvalidName, name, namePattern.String())
	}
	return nil
}

// Store persists tags. Set is an upsert and must reject a SnapshotID that
// doesn't exist or belongs to a different Resource; List returns one
// resource's tags sorted by name.
type Store interface {
	Set(ctx context.Context, t Tag) error
	Get(ctx context.Context, uri, name string) (Tag, error)
	List(ctx context.Context, uri string) ([]Tag, error)
	Delete(ctx context.Context, uri, name string) error
}
