package resolver_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"readproof/internal/policy"
	"readproof/internal/resolver"
	"readproof/internal/resource"
	"readproof/internal/source"
	"readproof/internal/tag"
)

const (
	originalPolicyText = "Products can be refunded within 30 days.\n"
	updatedPolicyText  = "Products can be refunded within 14 days.\n"
)

// registerFixture registers readproof://demo/policies/refunds against a
// temp file under the strictest policy (require_fresh), so a tag ref
// bypassing policy is unambiguous once the file changes.
func registerFixture(t *testing.T, ctx context.Context, res *resolver.Resolver, content string) (uri, filePath string) {
	t.Helper()
	filePath = filepath.Join(t.TempDir(), "refunds.md")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	uri = "readproof://demo/policies/refunds"
	if err := res.Resources.Create(ctx, resource.Resource{
		URI:       uri,
		Namespace: "demo",
		Path:      "policies/refunds",
		SourceConfig: source.Config{
			Kind:       source.KindFilesystem,
			Filesystem: &source.FilesystemConfig{Path: filePath},
		},
		Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
	}); err != nil {
		t.Fatalf("create resource: %v", err)
	}
	return uri, filePath
}

func TestResolveByTagIgnoresPolicyAndSourceChanges(t *testing.T) {
	res, _ := newTestResolver(t)
	ctx := context.Background()
	uri, filePath := registerFixture(t, ctx, res, originalPolicyText)

	first, err := res.Resolve(ctx, uri)
	if err != nil {
		t.Fatalf("initial resolve: %v", err)
	}
	if err := res.Tags.Set(ctx, tag.Tag{ResourceURI: uri, Name: "prod", SnapshotID: first.Snapshot.SnapshotID}); err != nil {
		t.Fatalf("set tag: %v", err)
	}

	// The source changes; require_fresh would normally force a re-fetch.
	if err := os.WriteFile(filePath, []byte(updatedPolicyText), 0o644); err != nil {
		t.Fatalf("edit fixture: %v", err)
	}

	tagged, err := res.Resolve(ctx, uri+"@prod")
	if err != nil {
		t.Fatalf("resolve by tag: %v", err)
	}
	if string(tagged.Content) != originalPolicyText {
		t.Fatalf("tagged resolve returned live content: got %q, want %q", string(tagged.Content), originalPolicyText)
	}
	if tagged.Snapshot.SnapshotID != first.Snapshot.SnapshotID {
		t.Fatalf("tagged resolve returned snapshot %s, want %s", tagged.Snapshot.SnapshotID, first.Snapshot.SnapshotID)
	}
	if tagged.Decision != policy.DecisionUseTag {
		t.Fatalf("decision = %s, want use_tag", tagged.Decision)
	}
	if tagged.Ref != "prod" {
		t.Fatalf("Ref = %q, want %q", tagged.Ref, "prod")
	}
	if tagged.Materialization.MaterializationID != first.Materialization.MaterializationID {
		t.Fatalf("tagged resolve created a new materialization: %s vs %s",
			tagged.Materialization.MaterializationID, first.Materialization.MaterializationID)
	}

	// Nothing was observed, so no snapshot row is created by a tagged resolve.
	history, err := res.Snapshots.ListByResource(ctx, uri)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 snapshot row after a tagged resolve, got %d", len(history))
	}

	// ResolveRef is the same path with the ref passed explicitly.
	viaRef, err := res.ResolveRef(ctx, uri, "prod")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if viaRef.Snapshot.SnapshotID != first.Snapshot.SnapshotID {
		t.Fatalf("ResolveRef returned snapshot %s, want %s", viaRef.Snapshot.SnapshotID, first.Snapshot.SnapshotID)
	}

	// A bare URI still resolves through the policy and sees the new bytes.
	fresh, err := res.Resolve(ctx, uri)
	if err != nil {
		t.Fatalf("resolve after edit: %v", err)
	}
	if string(fresh.Content) != updatedPolicyText {
		t.Fatalf("bare resolve did not see the edited source: got %q", string(fresh.Content))
	}
	if fresh.Ref != "" {
		t.Fatalf("bare resolve set Ref = %q, want empty", fresh.Ref)
	}
}

func TestResolveUnknownTagNamesURIAndTag(t *testing.T) {
	res, _ := newTestResolver(t)
	ctx := context.Background()
	uri, _ := registerFixture(t, ctx, res, originalPolicyText)

	_, err := res.Resolve(ctx, uri+"@nope")
	if !errors.Is(err, tag.ErrNotFound) {
		t.Fatalf("resolve unknown tag: got %v, want tag.ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), uri) || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error must name both the uri and the tag, got: %v", err)
	}
}

func TestResolveInvalidTagName(t *testing.T) {
	res, _ := newTestResolver(t)
	ctx := context.Background()
	uri, _ := registerFixture(t, ctx, res, originalPolicyText)

	if _, err := res.ResolveRef(ctx, uri, "not a tag"); !errors.Is(err, tag.ErrInvalidName) {
		t.Fatalf("resolve invalid tag name: got %v, want tag.ErrInvalidName", err)
	}
}
