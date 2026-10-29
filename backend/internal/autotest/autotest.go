// Package autotest runs test cases by itself (QĐ-16). A tester names where
// a case starts (an address on an environment's site, with {{variables}}),
// the test data and, when the page needs a signed-in user, one of the
// environment's test accounts; a model reads the page and drafts runnable
// cases, or the tester writes their own steps. A run opens each case in a
// browser session of the checker service, signs in, and runs the steps one
// by one; when a step's element is gone (the page changed), a model may
// repair that one step from what the page shows now — never an expectation,
// which is what the case checks. Each finished case is recorded in a
// testmgmt plan with its screenshots, and the whole run as a test run (the
// JUnit import), so the release gate and flaky detection read it like CI.
//
// Steps are data, never code (steps.go): the checker interprets them with
// Playwright's locators, inside the same SSRF guard as a URL check, and
// keeps the browser on the environment's site. A test account's password
// is sealed (platform/secretbox), sent to the checker only, and never to a
// model, which sees {{password}}.
//
// Owns environment_accounts, auto_specs, auto_drafts, auto_runs and
// auto_run_cases (migration 0042). Imports platform/* and tenancy (an
// environment's address). This file is the module's whole contract
// (§6.9.4).
package autotest

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/llm"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

// Queue kinds; each job's key is the row's id ("<run>/<case>" for a case).
const (
	JobDraft = "autotest.draft"
	JobLogin = "autotest.login"
	JobCase  = "autotest.case"
)

const (
	// MaxQueuedDrafts may wait per project; DailyDrafts per project in 24 h
	// (each is a paid model call on someone's key).
	MaxQueuedDrafts = 2
	DailyDrafts     = 30
	// MaxDraftCount cases per draft request.
	MaxDraftCount = 15
	// MaxActiveRuns queued or running per project; DailyRuns per project.
	MaxActiveRuns = 2
	DailyRuns     = 100
	// MaxRunCases per run.
	MaxRunCases = 100
	// MaxHealsPerCase steps a model may repair in one case, MaxHealsPerRun
	// in one run.
	MaxHealsPerCase = 2
	MaxHealsPerRun  = 20
	// MaxAccounts per environment.
	MaxAccounts = 20
	// Retention before the worker prunes drafts and runs.
	Retention = 90 * 24 * time.Hour
)

// Spec is what makes a test case runnable.
type Spec struct {
	CaseID    string
	ProjectID string
	Target    string            // where the case starts: relative to the environment's address, or absolute on its site
	Account   string            // the environment's test account key; "" = not signed in
	Inputs    map[string]string // the case's {{variables}}
	Steps     []Step
	Source    string // "ai" | "manual"
	UpdatedBy string
	UpdatedAt time.Time
}

// SpecInput saves a spec; Source "" is manual.
type SpecInput struct {
	Target  string
	Account string
	Inputs  map[string]string
	Steps   []Step
	Source  string
	Actor   string
}

// Sign-in states of an account.
const (
	LoginUnknown = "unknown" // no steps yet: found on first use
	LoginFinding = "finding" // a model is reading the sign-in page
	LoginOK      = "ok"
	LoginFailed  = "failed"
)

// Account is an environment's test account, without its password.
type Account struct {
	ID             string
	ProjectID      string
	EnvironmentID  string
	Key            string
	Username       string
	LoginURL       string // the sign-in page; "" = the environment's address
	LoginSteps     []Step // nil until found or written
	LoginStatus    string
	LoginError     string
	LoginCheckedAt time.Time
	UpdatedBy      string
	UpdatedAt      time.Time
}

// AccountInput creates or updates the account Key of an environment.
// Password "" keeps the stored one (required on create); LoginSteps nil
// keeps the steps, a pointer to an empty slice forgets them (found again
// on next use).
type AccountInput struct {
	Key        string
	Username   string
	Password   string
	LoginURL   string
	LoginSteps *[]Step
	Actor      string
}

// Draft is one runnable case a model proposed. The text came from a model
// that read an untrusted page: the dashboard shows it as text, the tester
// decides, and a run checks every step again.
type Draft struct {
	Title    string `json:"title"`
	Priority string `json:"priority"` // p0..p3
	Why      string `json:"why"`
	Steps    []Step `json:"steps"`
}

// Status of a draft request.
const (
	StatusQueued = "queued"
	StatusDone   = "done"
	StatusError  = "error"
)

type DraftRequest struct {
	ID            string
	ProjectID     string
	EnvironmentID string
	Target        string
	Account       string
	Inputs        map[string]string
	Context       string
	Count         int
	TestType      string
	Status        string
	Drafts        []Draft
	PageRead      bool
	Model         string
	KeySource     string
	InputTokens   int
	OutputTokens  int
	Error         string
	RequestedBy   string
	CreatedAt     time.Time
	FinishedAt    time.Time
}

type NewDraft struct {
	ProjectID     string
	OrgID         string // whose key pays
	EnvironmentID string
	Target        string
	Account       string
	Inputs        map[string]string
	Context       string // what to cover, ≤ 4000 bytes
	Count         int    // 0 = 5
	TestType      string // "" or one of the plan test types
	Actor         string
}

// Status of a run.
const (
	RunQueued  = "queued"
	RunRunning = "running"
	RunDone    = "done"
	RunError   = "error"
)

// Status of a case in a run.
const (
	CasePending = "pending"
	CasePassed  = "passed"
	CaseFailed  = "failed" // a step did not pass: an expectation, a missing element, a page off the site
	CaseError   = "error"  // the case could not run: no sign-in, the page did not open, no account
)

// Triggers of a run.
const (
	TriggerDashboard = "dashboard"
	TriggerCI        = "ci"
)

type Run struct {
	ID            string
	ProjectID     string
	OrgID         string
	EnvironmentID string
	PlanID        string
	Status        string
	Trigger       string
	RequestedBy   string
	Heal          bool
	Total         int
	Passed        int
	Failed        int
	Errored       int
	Healed        int
	InputTokens   int
	OutputTokens  int
	TestRunID     string
	Error         string
	CreatedAt     time.Time
	StartedAt     time.Time
	FinishedAt    time.Time
	Cases         []RunCase // on GetRun, by position
}

// RunCase is one case of a run.
type RunCase struct {
	CaseID         string
	Position       int
	Title          string
	Spec           Spec // as it was when the run started
	Status         string
	Results        []StepResult // one per step of Spec, once finished
	Console        []string
	Healed         int
	DurationMs     int
	Error          string
	HasFinalShot   bool
	HasFailureShot bool
	ExecutionID    string
	FinishedAt     time.Time
}

// StepResult is what happened to one step.
type StepResult struct {
	Status string `json:"status"` // passed | failed | skipped
	Kind   string `json:"kind,omitempty"`
	Error  string `json:"error,omitempty"`
	Ms     int    `json:"ms"`
	Healed *Heal  `json:"healed,omitempty"`
}

// Heal is a model's repair of a step that lost its element: the steps run
// in its place and why. Apply it to the spec with ApplyHeal.
type Heal struct {
	Steps  []Step `json:"steps"`
	Reason string `json:"reason"`
}

type NewRun struct {
	ProjectID     string
	OrgID         string
	EnvironmentID string
	PlanID        string            // the testmgmt plan the cases are recorded in
	Cases         []string          // case ids with a spec, in order
	Titles        map[string]string // case id → title
	Trigger       string
	Heal          bool
	Actor         string
}

// --- what the module needs from elsewhere ----------------------------------

// Browser opens sessions in the checker service (NewHTTPBrowser).
type Browser interface {
	Open(ctx context.Context, base string, login *Login) (Session, LoginResult, error)
}

// Login is how a session signs in: the sign-in page, the steps, and the
// variables they use — {{username}} and {{password}} with real values.
type Login struct {
	URL   string            `json:"url"`
	Steps []Step            `json:"steps"`
	Vars  map[string]string `json:"vars"`
}

type LoginResult struct {
	LoggedIn bool
	Error    string
}

// Session is one open browser.
type Session interface {
	// Run runs steps until one does not pass. When one fails, the result
	// carries the page as a model reads it and a screenshot.
	Run(ctx context.Context, steps []Step, vars map[string]string) (Outcome, error)
	// Look reads the page (with a screenshot when shot).
	Look(ctx context.Context, shot bool) (View, error)
	Close(ctx context.Context) error
}

type Outcome struct {
	OK      bool
	Results []StepOutcome
	URL     string
	Console []string
	View    *View // on a failure
}

type StepOutcome struct {
	Status string // passed | failed
	Kind   string // input | locator | assert | navigation | page
	Error  string
	Ms     int
}

// View is a page as a model reads it: address, title and accessibility
// tree. Untrusted: whoever runs the site wrote it.
type View struct {
	URL        string
	Title      string
	Tree       string
	Screenshot []byte // JPEG, when asked
}

// LLM is what this module needs of platform/llm.
type LLM interface {
	JSON(ctx context.Context, req llm.Request) (llm.Result, error)
}

// Keys picks the API key and model an organization's jobs use now
// (aisuggest's settings, QĐ-15); ok false: no key anywhere.
type Keys interface {
	Credentials(ctx context.Context, orgID string) (key, model, source string, ok bool, err error)
}

// Recorder writes a run where people read results; cmd/worker implements
// it with testmgmt (plan executions with screenshots) and ingest (a test
// run, through the JUnit import, with the cases linked to its tests).
type Recorder interface {
	// RecordCase records a finished case in the run's plan; it returns the
	// execution id ("" when nothing was recorded).
	RecordCase(ctx context.Context, run Run, c RunCase, finalShot, failureShot []byte) (string, error)
	// RecordRun records the finished run as a test run; it returns its id.
	RecordRun(ctx context.Context, run Run) (string, error)
}

type Deps struct {
	DB       *pg.DB
	Pepper   []byte // TOKEN_PEPPER: seals test accounts' passwords
	Tenancy  tenancy.Service
	Browser  Browser  // nil in the api: it only queues
	LLM      LLM      // nil: no drafting or repairs
	Keys     Keys     // nil: no drafting or repairs
	Recorder Recorder // nil: results stay in auto_runs only
}

var (
	ErrNotFound = errors.New("autotest: not found")
	ErrInvalid  = errors.New("autotest: invalid input")
	// ErrNotConfigured: drafting needs an API key and there is none.
	ErrNotConfigured = errors.New("autotest: no API key configured")
	// ErrBusy: too many requests waiting, or the day's limit reached.
	ErrBusy = errors.New("autotest: too many requests")
)

type Service interface {
	// Test accounts.
	Accounts(ctx context.Context, projectID, environmentID string) ([]Account, error)
	SaveAccount(ctx context.Context, projectID, environmentID string, in AccountInput) (Account, error)
	DeleteAccount(ctx context.Context, projectID, environmentID, key string) (bool, error)
	// FindLogin queues reading the account's sign-in page with a model and
	// trying the steps it writes (needs an API key for orgID).
	FindLogin(ctx context.Context, projectID, orgID, environmentID, key string) (Account, error)

	// Specs.
	Spec(ctx context.Context, projectID, caseID string) (Spec, error)
	Specs(ctx context.Context, projectID string) ([]Spec, error)
	// SaveSpec validates and stores a case's spec (the caller has checked
	// the case is the project's).
	SaveSpec(ctx context.Context, projectID, caseID string, in SpecInput) (Spec, error)
	DeleteSpec(ctx context.Context, projectID, caseID string) (bool, error)
	// ApplyHeal puts a run's repair of step `index` into the case's spec.
	ApplyHeal(ctx context.Context, projectID, runID, caseID string, index int, actor string) (Spec, error)

	// Drafts.
	RequestDraft(ctx context.Context, in NewDraft) (DraftRequest, error)
	Draft(ctx context.Context, projectID, id string) (DraftRequest, error)

	// Runs.
	CreateRun(ctx context.Context, in NewRun) (Run, error)
	Runs(ctx context.Context, projectID string, limit int) ([]Run, error)
	GetRun(ctx context.Context, projectID, id string) (Run, error)
	// Shot returns a case's screenshot (JPEG): which is "final" or "failure".
	Shot(ctx context.Context, projectID, runID, caseID, which string) ([]byte, error)
	// LastResults returns the newest finished status of each case with a
	// spec, keyed by case id.
	LastResults(ctx context.Context, projectID string) (map[string]LastResult, error)

	// Logins hands the checker an account's sign-in for a site scan:
	// nil when the account has no working steps yet.
	LoginFor(ctx context.Context, projectID, environmentID, key string) (*Login, error)

	// Workers for cmd/worker.
	DraftWorker(concurrency int) *queue.Worker
	LoginWorker(concurrency int) *queue.Worker
	CaseWorker(concurrency int) *queue.Worker

	Prune(ctx context.Context, before time.Time, limit int) (int, error)
	PurgeProject(ctx context.Context, projectID string) error
}

// LastResult is a case's newest finished result in a run.
type LastResult struct {
	RunID      string
	Status     string
	FinishedAt time.Time
}

// stepsJSON keeps nil slices out of the database.
func stepsJSON(steps []Step) []byte {
	if steps == nil {
		steps = []Step{}
	}
	b, _ := json.Marshal(steps)
	return b
}
