// Package artifact owns the Artifact table (§6.9.5, §8.4) and the upload
// protocol of §7.7: declare → presigned PUT or multipart → complete, where
// the server HEADs the object, streams it through SHA-256 and only then
// calls it verified. Caps (§10.4) and retention are decided here; the
// object store never learns what a run is, and the client never chooses a
// key.
//
// Three rules from the SPEC shape everything below: a rejected artifact
// never fails the run (§10.4), attribution is per attempt (§7.7), and an
// artifact that arrives before its test.finished event is legal and gets
// linked later (§7.6 #3).
//
// This file is the whole contract (§6.9.4). Domain fan-out: results only.
package artifact

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Kind is §7.7's enum.
type Kind string

const (
	KindScreenshot Kind = "screenshot"
	KindVideo      Kind = "video"
	KindTrace      Kind = "trace"
	KindHAR        Kind = "har"
	KindLog        Kind = "log"
	KindAttachment Kind = "attachment"
)

// Caps is §10.4, bytes per kind, enforced at declare and again at
// complete against what actually landed.
var Caps = map[Kind]int64{
	KindScreenshot: 5 << 20,
	KindVideo:      20 << 20,
	KindTrace:      50 << 20,
	KindHAR:        20 << 20,
	KindLog:        10 << 20,
	KindAttachment: 10 << 20,
}

// RetentionDays is §10.4 by plan. An unknown plan gets free.
var RetentionDays = map[string]map[Kind]int{
	"free":     {KindScreenshot: 14, KindVideo: 7, KindTrace: 7, KindHAR: 7, KindLog: 14, KindAttachment: 14},
	"team":     {KindScreenshot: 30, KindVideo: 14, KindTrace: 14, KindHAR: 14, KindLog: 30, KindAttachment: 30},
	"business": {KindScreenshot: 90, KindVideo: 30, KindTrace: 30, KindHAR: 30, KindLog: 90, KindAttachment: 90},
}

// Protocol constants (§7.7).
const (
	MultipartThreshold = 8 << 20          // at or above: multipart
	PartSize           = 8 << 20          // bytes per part
	MaxParallelParts   = 4                // advisory for the client
	PresignTTL         = 15 * time.Minute // URL lifetime
	AbandonAfter       = 24 * time.Hour   // pending uploads older than this are cleaned up
	UnlinkedAlertAfter = time.Hour        // §6.4: unlinked > 1 h is an alert
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusUploaded Status = "uploaded"
	StatusVerified Status = "verified"
	StatusRejected Status = "rejected"
	StatusExpired  Status = "expired"
	StatusDeleted  Status = "deleted"
)

// Artifact is §8.4 Artifact.
type Artifact struct {
	ID              string
	ProjectID       string
	RunID           string
	AttemptID       string // "" until linked
	ClientTestID    string
	OccurrenceIndex int
	Attempt         int
	Kind            Kind
	Name            string
	ContentType     string
	DeclaredBytes   int64
	ActualBytes     int64
	SHA256          string
	StorageKey      string
	UploadID        string
	Status          Status
	FailureReason   string
	ExpiresAt       time.Time
	CreatedAt       time.Time
	VerifiedAt      time.Time
	LinkedAt        time.Time
}

// Declare is the metadata of §7.7. RunID/ProjectID come from the caller's
// run lookup; the rest is the reporter's.
type Declare struct {
	ProjectID       string
	RunID           string
	Plan            string // organization plan id, for retention
	ClientTestID    string
	OccurrenceIndex int
	Attempt         int
	Kind            Kind
	Name            string
	ContentType     string
	SizeBytes       int64
	SHA256          string
}

// Upload is what the reporter needs to move bytes. Exactly one of Put or
// Multipart is set unless the artifact is already verified (both nil).
type Upload struct {
	Put       *PutUpload
	Multipart *MultipartUpload
}

// PutUpload is the single-request path (< 8 MB).
type PutUpload struct {
	URL       string
	Headers   map[string]string // must be sent verbatim: they are signed
	ExpiresAt time.Time
}

// MultipartUpload is the chunked path. Parts lists only what is still
// missing, so a resumed declare hands back just the work left.
type MultipartUpload struct {
	UploadID  string
	PartSize  int64
	Parallel  int
	Parts     []PartURL
	Received  []int
	ExpiresAt time.Time
}

type PartURL struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

// Complete is the reporter's closing message: the whole-file hash (must
// equal the declared one) and, for multipart, the ETags it collected.
type Complete struct {
	SHA256 string
	Parts  []CompletedPart
}

type CompletedPart struct {
	Number int
	ETag   string
}

var (
	ErrNotFound = errors.New("artifact: not found")
	ErrInvalid  = errors.New("artifact: invalid input")
	// ErrGone: the artifact was expired or deleted; declare a new name.
	ErrGone = errors.New("artifact: gone")
	// ErrUploadIncomplete: complete was called but the object is not there.
	ErrUploadIncomplete = errors.New("artifact: upload incomplete")
	// ErrStore: the object store did not answer; the artifact stays
	// retryable (§14.3: S3 timeout → retryable, run still valid).
	ErrStore = errors.New("artifact: object store unavailable")
)

// TooLargeError is §10.4's 413 with the limit in details.
type TooLargeError struct {
	Kind  Kind
	Limit int64
	Got   int64
}

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("artifact: %s of %d bytes exceeds the %d-byte cap", e.Kind, e.Got, e.Limit)
}

// MismatchError is §7.7's 422: the object was deleted and the artifact
// marked rejected. Code is SIZE_MISMATCH or CHECKSUM_MISMATCH.
type MismatchError struct {
	Code     string
	Expected string
	Actual   string
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf("artifact: %s: expected %s, got %s", e.Code, e.Expected, e.Actual)
}

// Service is the contract.
type Service interface {
	// Declare records the artifact and returns how to upload it. Repeating
	// a declare returns the same artifact with fresh URLs and, for
	// multipart, the parts already received (§7.7 resume).
	Declare(ctx context.Context, in Declare) (a Artifact, up Upload, created bool, err error)
	// Finish verifies the upload: HEAD for size and content type, GET
	// streamed through SHA-256, then verified and linked when its attempt
	// exists. A mismatch deletes the object and returns *MismatchError.
	// Finishing a verified artifact again is a no-op with verified=false.
	Finish(ctx context.Context, projectID, artifactID string, in Complete) (a Artifact, verified bool, err error)

	Get(ctx context.Context, projectID, artifactID string) (Artifact, error)
	ListByRun(ctx context.Context, projectID, runID string) ([]Artifact, error)

	// LinkPending attaches unlinked artifacts to attempts that have since
	// arrived; returns how many it linked and how many remain unlinked
	// longer than UnlinkedAlertAfter (§6.4).
	LinkPending(ctx context.Context) (linked, overdue int, err error)
	// ExpireRetention deletes objects past expires_at (§10.4).
	ExpireRetention(ctx context.Context) (int, error)
	// AbortAbandoned cleans uploads pending longer than AbandonAfter (§7.7).
	AbortAbandoned(ctx context.Context) (int, error)
}

// ValidKind reports whether k is in the §7.7 enum.
func ValidKind(k Kind) bool { _, ok := Caps[k]; return ok }

// AllowedContentType is the content-type policy (§7.7, §11.5): what a
// kind may be stored as. Attachments are open except for types a browser
// would execute if ever rendered inline.
func AllowedContentType(k Kind, ct string) bool {
	switch k {
	case KindScreenshot:
		return ct == "image/png" || ct == "image/webp" || ct == "image/jpeg"
	case KindVideo:
		return ct == "video/webm" || ct == "video/mp4"
	case KindTrace:
		return ct == "application/zip"
	case KindHAR:
		return ct == "application/json"
	case KindLog:
		return ct == "text/plain" || ct == "application/gzip"
	case KindAttachment:
		return ct != "" && ct != "text/html" && ct != "image/svg+xml" && ct != "application/javascript" && ct != "text/javascript"
	}
	return false
}
