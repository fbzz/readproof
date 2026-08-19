// Package s3blob implements the blob.Store interface backed by an
// S3-compatible object store (MinIO in dev, and any S3-compatible service in
// production). It is a drop-in swap for blob.LocalStore, preserving the same
// content-addressable dedup semantics: identical content bytes hash to the
// same key and are only ever uploaded once.
package s3blob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"ctx/internal/ids"
	"ctx/internal/storage/blob"
)

// Config holds the connection details for an S3-compatible object store.
type Config struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	UseSSL          bool
}

// Store stores blobs in an S3-compatible bucket, keyed by their full hex
// sha256 digest (no sharding — S3 doesn't need it the way a local
// filesystem does).
type Store struct {
	client *minio.Client
	bucket string
}

var _ blob.Store = (*Store)(nil)

// New constructs a Store, connecting to the configured S3-compatible
// endpoint and ensuring the target bucket exists (creating it if missing) so
// callers don't need a separate provisioning step.
func New(ctx context.Context, cfg Config) (*Store, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("s3blob: new client: %w", err)
	}

	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("s3blob: bucket exists check: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("s3blob: make bucket %s: %w", cfg.Bucket, err)
		}
	}

	return &Store{client: client, bucket: cfg.Bucket}, nil
}

// hexPart validates and strips the "sha256:" prefix from a content hash,
// mirroring blob.LocalStore's hexPart helper.
func hexPart(contentHash string) (string, error) {
	const prefix = "sha256:"
	if len(contentHash) <= len(prefix) || contentHash[:len(prefix)] != prefix {
		return "", fmt.Errorf("s3blob: invalid content hash %q", contentHash)
	}
	return contentHash[len(prefix):], nil
}

// Put uploads content, deduplicating by content hash: if an object with the
// same hash already exists, it is not re-uploaded.
func (s *Store) Put(content []byte) (string, error) {
	ctx := context.Background()
	hexHash := ids.SHA256Hex(content)
	contentHash := "sha256:" + hexHash

	_, err := s.client.StatObject(ctx, s.bucket, hexHash, minio.StatObjectOptions{})
	if err == nil {
		return contentHash, nil // dedup: identical bytes already stored
	}
	if !isNotFound(err) {
		return "", fmt.Errorf("s3blob: stat %s: %w", contentHash, err)
	}

	_, err = s.client.PutObject(ctx, s.bucket, hexHash, bytes.NewReader(content), int64(len(content)), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return "", fmt.Errorf("s3blob: put %s: %w", contentHash, err)
	}
	return contentHash, nil
}

// Get returns the exact original bytes for contentHash.
func (s *Store) Get(contentHash string) ([]byte, error) {
	hexHash, err := hexPart(contentHash)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	obj, err := s.client.GetObject(ctx, s.bucket, hexHash, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("s3blob: get %s: %w", contentHash, err)
	}
	defer obj.Close()

	content, err := io.ReadAll(obj)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("s3blob: object not found %s: %w", contentHash, err)
		}
		return nil, fmt.Errorf("s3blob: read %s: %w", contentHash, err)
	}
	return content, nil
}

// Has reports whether an object with contentHash exists. A "not found"
// response is a valid false result, not an error.
func (s *Store) Has(contentHash string) (bool, error) {
	hexHash, err := hexPart(contentHash)
	if err != nil {
		return false, err
	}

	ctx := context.Background()
	_, err = s.client.StatObject(ctx, s.bucket, hexHash, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	if isNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("s3blob: stat %s: %w", contentHash, err)
}

// isNotFound reports whether err represents a "no such key"/"not found"
// response from the S3-compatible server.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var errResp minio.ErrorResponse
	if errors.As(err, &errResp) {
		return errResp.Code == "NoSuchKey" || errResp.StatusCode == 404
	}
	return minio.ToErrorResponse(err).Code == "NoSuchKey"
}
