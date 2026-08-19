// This file re-runs the exact same Refund Agent demo assertions as
// demo_test.go, but against live Postgres + MinIO instead of embedded
// SQLite + local disk — proving the storage swap is a true drop-in (same
// assertions, only the backend changes).
package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ctx/internal/app"
	"ctx/internal/ids"
	"ctx/internal/policy"
	"ctx/internal/resource"
	"ctx/internal/source"
)

func TestRefundAgentDemoReplayInvariant_Postgres(t *testing.T) {
	dsn := os.Getenv("CTX_TEST_POSTGRES_DSN")
	endpoint := os.Getenv("CTX_TEST_MINIO_ENDPOINT")
	if dsn == "" || endpoint == "" {
		t.Skip("CTX_TEST_POSTGRES_DSN and CTX_TEST_MINIO_ENDPOINT not both set; skipping postgres+minio e2e test")
	}
	accessKey := envOrDefault("CTX_TEST_MINIO_ACCESS_KEY", "ctxadmin")
	secretKey := envOrDefault("CTX_TEST_MINIO_SECRET_KEY", "ctx_dev_password_minio")

	fixtureDir := t.TempDir()
	fixturePath := filepath.Join(fixtureDir, "refunds.md")

	const originalContent = "Products can be refunded within 30 days.\n"
	const updatedContent = "Products can be refunded within 14 days.\n"

	if err := os.WriteFile(fixturePath, []byte(originalContent), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	ctx := context.Background()
	a, err := app.OpenPostgres(ctx,
		app.PostgresConfig{DSN: dsn},
		app.S3Config{
			Endpoint:        endpoint,
			AccessKeyID:     accessKey,
			SecretAccessKey: secretKey,
			Bucket:          "ctx-blobs-e2e",
			UseSSL:          false,
		},
	)
	if err != nil {
		t.Fatalf("open postgres-backed app: %v", err)
	}
	defer a.Close()

	// Unique per run: resources/runs are durable rows in a shared live
	// Postgres instance, unlike SQLite's per-test temp file.
	suffix := ids.New("e2e")
	uri := "ctx://demo-" + suffix + "/policies/refunds"
	runAID := "run-a-" + suffix
	runBID := "run-b-" + suffix

	err = a.Resources.Create(ctx, resource.Resource{
		URI:       uri,
		Namespace: "demo-" + suffix,
		Path:      "policies/refunds",
		SourceConfig: source.Config{
			Kind:       source.KindFilesystem,
			Filesystem: &source.FilesystemConfig{Path: fixturePath},
		},
		Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
	})
	if err != nil {
		t.Fatalf("register resource: %v", err)
	}

	if _, err := a.Resolver.Resolve(ctx, uri); err != nil {
		t.Fatalf("initial resolve: %v", err)
	}

	manA, err := a.RunBuilder.Run(ctx, runAID, []string{uri})
	if err != nil {
		t.Fatalf("run-a: %v", err)
	}
	if len(manA.Entries) != 1 {
		t.Fatalf("expected 1 manifest entry for run-a, got %d", len(manA.Entries))
	}

	if err := os.WriteFile(fixturePath, []byte(updatedContent), 0o644); err != nil {
		t.Fatalf("edit fixture: %v", err)
	}

	manB, err := a.RunBuilder.Run(ctx, runBID, []string{uri})
	if err != nil {
		t.Fatalf("run-b: %v", err)
	}

	if manA.Entries[0].ContentHash == manB.Entries[0].ContentHash {
		t.Fatalf("expected manifest content hashes to differ after the source changed")
	}

	replayResult, err := a.Replayer.Replay(ctx, runAID)
	if err != nil {
		t.Fatalf("replay run-a: %v", err)
	}
	if !replayResult.AllMatch() {
		t.Fatalf("replay verification failed: %+v", replayResult.Entries)
	}
	entry := replayResult.Entries[0]
	if entry.RecordedHash != entry.ReplayedHash {
		t.Fatalf("SHA256 mismatch: recorded=%s replayed=%s", entry.RecordedHash, entry.ReplayedHash)
	}
	if string(entry.Content) != originalContent {
		t.Fatalf("replayed content does not match the original: got %q, want %q", string(entry.Content), originalContent)
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
