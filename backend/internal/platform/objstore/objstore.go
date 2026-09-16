// Package objstore is the object storage boundary of §7.7: presigned
// uploads (single PUT or multipart), HEAD to verify what actually landed,
// GET to hash it, DELETE for rejects and retention. Keys are opaque strings
// the caller generates — the client never chooses a path or bucket.
//
// Two implementations: s3 (S3 and Cloudflare R2, SigV4 over net/http, no
// SDK) and memory (an httptest-backed fake that honours the same presigned
// URLs, so the artifact flow is tested end to end without credentials).
package objstore

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound: the key (or upload) does not exist.
var ErrNotFound = errors.New("objstore: not found")

// Info is what HEAD reports.
type Info struct {
	Size        int64
	ContentType string
	ETag        string
}

// Part is one uploaded chunk of a multipart upload.
type Part struct {
	Number int
	ETag   string
	Size   int64
}

// Store is the contract. All URLs returned are absolute and usable by a
// client with no credentials until ttl passes.
type Store interface {
	// PresignPut returns a URL for a single PUT of the whole object. The
	// client must send exactly contentType.
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error)
	// CreateMultipart starts a multipart upload and returns its id.
	CreateMultipart(ctx context.Context, key, contentType string) (uploadID string, err error)
	// PresignPart returns a URL for PUT of one part (1-based).
	PresignPart(ctx context.Context, key, uploadID string, partNumber int, ttl time.Duration) (string, error)
	// ListParts reports the parts received so far — for resume.
	ListParts(ctx context.Context, key, uploadID string) ([]Part, error)
	// CompleteMultipart assembles the parts, in order.
	CompleteMultipart(ctx context.Context, key, uploadID string, parts []Part) error
	// AbortMultipart discards an unfinished upload.
	AbortMultipart(ctx context.Context, key, uploadID string) error

	Head(ctx context.Context, key string) (Info, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}
