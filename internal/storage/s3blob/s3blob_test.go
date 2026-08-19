package s3blob

import (
	"context"
	"os"
	"testing"

	"ctx/internal/ids"
)

// testConfig builds a Config from environment variables, skipping the test
// if CTX_TEST_MINIO_ENDPOINT is unset so `go test ./...` stays green without
// a live MinIO instance.
func testConfig(t *testing.T) Config {
	t.Helper()

	endpoint := os.Getenv("CTX_TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("CTX_TEST_MINIO_ENDPOINT not set; skipping live MinIO test")
	}

	accessKey := os.Getenv("CTX_TEST_MINIO_ACCESS_KEY")
	if accessKey == "" {
		accessKey = "ctxadmin"
	}
	secretKey := os.Getenv("CTX_TEST_MINIO_SECRET_KEY")
	if secretKey == "" {
		secretKey = "ctx_dev_password_minio"
	}

	return Config{
		Endpoint:        endpoint,
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
		Bucket:          "ctx-blobs-test",
		UseSSL:          false,
	}
}

func TestStorePutGetHas(t *testing.T) {
	cfg := testConfig(t)
	ctx := context.Background()

	store, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	content := []byte("Products can be refunded within 30 days.\n")

	hash1, err := store.Put(content)
	if err != nil {
		t.Fatalf("put 1: %v", err)
	}
	hash2, err := store.Put(content)
	if err != nil {
		t.Fatalf("put 2: %v", err)
	}
	if hash1 != hash2 {
		t.Fatalf("expected identical content hash, got %s vs %s", hash1, hash2)
	}

	got, err := store.Get(hash1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content mismatch: got %q, want %q", string(got), string(content))
	}

	has, err := store.Has(hash1)
	if err != nil {
		t.Fatalf("has (stored): %v", err)
	}
	if !has {
		t.Fatalf("expected Has to report true for a stored blob")
	}

	neverStored := ids.ContentHash([]byte("this content was never put into the store"))
	has, err = store.Has(neverStored)
	if err != nil {
		t.Fatalf("has (never stored): %v", err)
	}
	if has {
		t.Fatalf("expected Has to report false for a hash that was never Put")
	}
}
