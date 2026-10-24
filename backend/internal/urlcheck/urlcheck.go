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
// A site scan (QĐ-16) checks the start page and the pages its same-site
// links reach, breadth first, up to a page limit and three links deep —
// each page an ordinary check — optionally signed in as one of the
// environment's test accounts (whose sign-in autotest keeps).
//
// Owns url_checks (migration 0037) and site_scans (0042). Imports only
// platform/*. This file is the module's whole contract (§6.9.4).
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
	ScanID        string // "" for a check a tester asked for
	Depth         int    // links from the scan's start page
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
	Links   []string // the page's same-site links, for a scan
}

// Checker opens a URL and reports on it, signed in first when login is
// set. NewHTTPChecker is the real one.
type Checker interface {
	Check(ctx context.Context, url string, login *Login) (Result, error)
}

// Login is how the checker signs in: the sign-in page, the steps
// (autotest's step shape) and their variables, password included.
type Login struct {
	URL   string            `json:"url"`
	Steps json.RawMessage   `json:"steps"`
	Vars  map[string]string `json:"vars"`
}

// Logins hands a scan an environment test account's sign-in (autotest);
// nil when the account has no working steps yet.
type Logins interface {
	Login(ctx context.Context, projectID, environmentID, account string) (*Login, error)
}

// Site scans.
const (
	// MaxScanPages a scan may check; DefaultScanPages when none is asked.
	MaxScanPages     = 50
	DefaultScanPages = 20
	// MaxScanDepth links from the start page.
	MaxScanDepth = 3
	// MaxScansRunning per project at once.
	MaxScansRunning = 1
)

const (
	ScanRunning = "running"
	ScanDone    = "done"
)

// Scan is one site scan; the counts are over its pages so far.
type Scan struct {
	ID            string
	ProjectID     string
	EnvironmentID string
	StartURL      string
	MaxPages      int
	Account       string
	Status        string
	RequestedBy   string
	Error         string
	Pages         int // found
	Checked       int // done or error
	Passed        int
	Warnings      int
	Failed        int
	CreatedAt     time.Time
	FinishedAt    time.Time
	Checks        []Check // on GetScan: its pages, nearest first
}

type NewScan struct {
	ProjectID     string
	EnvironmentID string
	URL           string
	Account       string // an environment test account key; "" = signed out
	MaxPages      int    // 0 = DefaultScanPages
	Actor         string
}

var (
	ErrNotFound = errors.New("urlcheck: not found")
	// ErrInvalid: the address is not an http(s) URL with a host, or carries
	// credentials.
	ErrInvalid = errors.New("urlcheck: invalid input")
	// ErrBusy: the project already has MaxQueued checks waiting, or a
	// scan running.
	ErrBusy = errors.New("urlcheck: too many checks waiting")
)

type Service interface {
	// Create queues a check of url, in the same transaction as its job.
	Create(ctx context.Context, projectID, environmentID, url, actor string) (Check, error)
	// List returns the project's newest checks a tester asked for (not a
	// scan's pages), without reports.
	List(ctx context.Context, projectID string, limit int) ([]Check, error)
	Get(ctx context.Context, projectID, id string) (Check, error)
	// Screenshot returns a JPEG: which is "desktop" or "mobile".
	Screenshot(ctx context.Context, projectID, id, which string) ([]byte, error)
	// Worker consumes JobRun with the checker; cmd/worker runs it when
	// URLCHECK=on.
	Worker(concurrency int) *queue.Worker
	// Prune deletes up to limit checks created before the cut-off.
	Prune(ctx context.Context, before time.Time, limit int) (int, error)
	// PurgeProject deletes every check and scan of a project being purged.
	PurgeProject(ctx context.Context, projectID string) error

	// CreateScan queues the start page; each page checked queues the next.
	CreateScan(ctx context.Context, in NewScan) (Scan, error)
	Scans(ctx context.Context, projectID string, limit int) ([]Scan, error)
	GetScan(ctx context.Context, projectID, id string) (Scan, error)
}
