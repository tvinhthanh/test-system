// Package urlcheck runs the URL checks testers ask for (QĐ-14): a tester
// types an address (or picks an environment's), the request is queued, and
// the worker hands it to the checker service — headless Chromium behind a
// guarded proxy, its own Docker network — which reports load, HTTPS, script
// errors, broken resources and links, accessibility, speed and phone fit
// with two screenshots. No test code is needed; it is the first look a
// tester takes at a page, not a test suite.
//
// The checker is reached at one address fixed by configuration
// (URLCHECK_URL, platform/outbound); the address to check is data in the
// request body and is opened only by the checker, never fetched here.
//
// Owns url_checks (migration 0037). Imports only platform/*. This file is
// the module's whole contract (§6.9.4).
package urlcheck

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
)

// JobRun is the queue kind of one check; the job key is the check's id.
const JobRun = "urlcheck.run"

// MaxQueued is how many checks one project may have waiting at once.
const MaxQueued = 5

// Retention is how long checks are kept before the worker prunes them.
const Retention = 90 * 24 * time.Hour

// Status of a check: queued until the worker ran it, then done (with a
// report) or error (the checker could not be reached after the retries).
const (
	StatusQueued = "queued"
	StatusDone   = "done"
	StatusError  = "error"
)

// Check is one requested check. Report is the checker's JSON (see
// checker/src/check.mjs), present on Get. On List, Statuses holds only the
// per-check statuses in report order, so a list stays small.
type Check struct {
	ID            string
	ProjectID     string
	EnvironmentID string
	URL           string
	Status        string
	Verdict       string // passed | warning | failed, "" until done
	Title         string
	FinalURL      string
	RequestedBy   string
	Report        json.RawMessage
	Statuses      []string
	Error         string
	HasDesktop    bool
	HasMobile     bool
	CreatedAt     time.Time
	FinishedAt    time.Time
}

// Result is what the checker answered for one URL.
type Result struct {
	Report  json.RawMessage
	Verdict string
	Desktop []byte // JPEG; nil when the page could not be shown
	Mobile  []byte
}

// Checker opens a URL and reports on it. NewHTTPChecker is the real one.
type Checker interface {
	Check(ctx context.Context, url string) (Result, error)
}

var (
	ErrNotFound = errors.New("urlcheck: not found")
	// ErrInvalid: the address is not an http(s) URL with a host, or carries
	// credentials.
	ErrInvalid = errors.New("urlcheck: invalid input")
	// ErrBusy: the project already has MaxQueued checks waiting.
	ErrBusy = errors.New("urlcheck: too many checks waiting")
)

type Service interface {
	// Create queues a check of url, in the same transaction as its job.
	Create(ctx context.Context, projectID, environmentID, url, actor string) (Check, error)
	// List returns the project's newest checks, without reports.
	List(ctx context.Context, projectID string, limit int) ([]Check, error)
	Get(ctx context.Context, projectID, id string) (Check, error)
	// Screenshot returns a JPEG: which is "desktop" or "mobile".
	Screenshot(ctx context.Context, projectID, id, which string) ([]byte, error)
	// Worker consumes JobRun with the checker; cmd/worker runs it when
	// URLCHECK=on.
	Worker(concurrency int) *queue.Worker
	// Prune deletes up to limit checks created before the cut-off.
	Prune(ctx context.Context, before time.Time, limit int) (int, error)
	// PurgeProject deletes every check of a project being purged.
	PurgeProject(ctx context.Context, projectID string) error
}
