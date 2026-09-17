// Package results owns Run, TestResult and TestResultAttempt (§8.4) — the
// facts a reporter sends — and the two idempotency rules the protocol makes
// on them: a run is keyed by localRunId, an event by (localRunId, sequence)
// (§7.2). It does not parse HTTP, resolve identity or talk to storage; those
// are ingest, identity and artifact. A TestResult is created with no
// stableTestId and identity backfills it later (§7.6).
//
// This file is the whole contract (§6.9.4). Writes join the caller's
// transaction (§6.9.6): ingest wraps one batch in one WithTx.
package results

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type (
	RunID        string
	TestResultID string
)

// RunStatus per §8.4 / §7.8. Reporter-driven runs go running → passed |
// failed | error; queued and cancelled belong to the agent flow (P3).
type RunStatus string

const (
	RunQueued    RunStatus = "queued"
	RunRunning   RunStatus = "running"
	RunPassed    RunStatus = "passed"
	RunFailed    RunStatus = "failed"
	RunError     RunStatus = "error"
	RunCancelled RunStatus = "cancelled"
)

// AttemptStatus is what one attempt did (§8.4 TestResultAttempt.status).
type AttemptStatus string

const (
	AttemptPassed      AttemptStatus = "passed"
	AttemptFailed      AttemptStatus = "failed"
	AttemptSkipped     AttemptStatus = "skipped"
	AttemptTimedOut    AttemptStatus = "timed_out"
	AttemptInterrupted AttemptStatus = "interrupted"
)

// Outcome is the framework's own verdict for the test as of this attempt
// (Playwright test.status: expected | unexpected | flaky | skipped). It is
// carried, never re-derived (N36): flaky is only ever what Playwright said.
type Outcome string

const (
	OutcomeExpected   Outcome = "expected"
	OutcomeUnexpected Outcome = "unexpected"
	OutcomeFlaky      Outcome = "flaky"
	OutcomeSkipped    Outcome = "skipped"
)

// FinalStatus is TestResult.finalStatus (§8.4), derived by FinalStatusOf
// from the last attempt's (Outcome, AttemptStatus) per the N41 table.
type FinalStatus string

const (
	FinalPassed      FinalStatus = "passed"
	FinalFailed      FinalStatus = "failed"
	FinalSkipped     FinalStatus = "skipped"
	FinalTimedOut    FinalStatus = "timed_out"
	FinalInterrupted FinalStatus = "interrupted"
	FinalFlakyPass   FinalStatus = "flaky_pass"
)

// Run is §8.4 Run.
type Run struct {
	ID            RunID
	ProjectID     string
	EnvironmentID string
	LocalRunID    string
	Status        RunStatus
	FlakyCount    int
	ErrorReason   string
	Trigger       json.RawMessage
	CommitSHA     string
	Branch        string
	Framework     json.RawMessage
	Reporter      json.RawMessage
	StartedAt     time.Time
	FinishedAt    time.Time // zero while open
	CreatedAt     time.Time
}

// NewRun is what CreateRun needs. ProjectID/EnvironmentID come from the
// caller's tenancy lookups; LocalRunID is the reporter's UUID (§7.3).
type NewRun struct {
	ProjectID     string
	EnvironmentID string
	LocalRunID    string
	Trigger       json.RawMessage
	CommitSHA     string
	Branch        string
	Framework     json.RawMessage
	Reporter      json.RawMessage
	StartedAt     time.Time
}

// AttemptEvent is one test.finished event (§7.3): one attempt of one test.
type AttemptEvent struct {
	Sequence        int64
	ClientTestID    string
	OccurrenceIndex int
	Attempt         int
	DisplayName     string
	SuiteName       string
	IdentityRaw     json.RawMessage // {framework, filePath, titlePath, paramSignature}
	StableTestID    string          // tier-1 hit known at ingest, "" when unresolved
	Status          AttemptStatus
	Outcome         Outcome
	DurationMs      int
	Error           json.RawMessage // {message, stack, snippet}, nil when none
	WorkerID        string
	StartedAt       time.Time
	FinishedAt      time.Time
}

// Appended reports what a batch did. Duplicates are sequences the run had
// already accepted — a retried batch, not an error (§7.2).
type Appended struct {
	Accepted   int
	Duplicates int
}

// TestResult is §8.4 TestResult, as read back for finish/resolve/query.
type TestResult struct {
	ID                TestResultID
	RunID             RunID
	ProjectID         string
	FailureCategory   string // §8.7, "" until triaged
	StableTestID      string // "" until identity backfills it
	ClientTestID      string
	OccurrenceIndex   int
	DisplayName       string
	SuiteName         string
	IdentityRaw       json.RawMessage
	FinalStatus       FinalStatus
	AttemptCount      int
	FirstFailedAt     time.Time
	FailureObservedAt time.Time
	DurationMs        int
	CreatedAt         time.Time
}

var (
	// ErrNotFound: no run with that id / (project, localRunId).
	ErrNotFound = errors.New("results: not found")
	// ErrRunFinished: events for a run that is no longer running (→ 409).
	ErrRunFinished = errors.New("results: run already finished")
	// ErrInvalid: input failed shape validation.
	ErrInvalid = errors.New("results: invalid input")
)

// Service is the contract.
type Service interface {
	// CreateRun is idempotent on (ProjectID, LocalRunID): a repeat returns
	// the existing run with created=false and changes nothing (§7.2 #1).
	CreateRun(ctx context.Context, in NewRun) (run Run, created bool, err error)
	// GetRun looks a run up the way the protocol addresses it. The project
	// is part of the key so another tenant's run is simply not found.
	GetRun(ctx context.Context, projectID, localRunID string) (Run, error)
	// AppendAttempts applies a batch. Each event is deduplicated by
	// sequence; a duplicate is counted, not applied. Returns
	// ErrRunFinished when the run is not running.
	AppendAttempts(ctx context.Context, run RunID, events []AttemptEvent) (Appended, error)
	// FinishRun closes a running run, deriving status and flakyCount from
	// its results. Finishing twice returns the run unchanged (§7.2 #3).
	FinishRun(ctx context.Context, run RunID, finishedAt time.Time) (Run, error)
	// ListResults returns every TestResult of a run, for tier-2 resolve
	// and for finish-time summaries.
	ListResults(ctx context.Context, run RunID) ([]TestResult, error)
	// SetStableIDs backfills stableTestId for the run's results, keyed by
	// clientTestId (every occurrence of a fingerprint in a run shares one
	// identity, §9.7). Only empty cells are written: identity never
	// overwrites an attribution — Merge and Split exist for that. Returns
	// the number of rows filled.
	SetStableIDs(ctx context.Context, run RunID, byClient map[string]string) (int, error)
	// AutoClose marks every run still running that started before the
	// cutoff as error RUN_TIMED_OUT (§7.4) and returns them. Idempotent: a
	// closed run is no longer running.
	AutoClose(ctx context.Context, startedBefore time.Time) ([]Run, error)
	// OpenRuns counts runs still running that started before the time —
	// what AutoClose would close; a non-zero value that persists means the
	// sweep is not running (§6.4).
	OpenRuns(ctx context.Context, startedBefore time.Time) (int, error)
	// FindAttempt returns the id of one attempt by its protocol keys (§7.7
	// artifact linking), or ErrNotFound while the event has not arrived.
	FindAttempt(ctx context.Context, run RunID, clientTestID string, occurrenceIndex, attempt int) (string, error)
	// GetRunByID is GetRun for callers that hold the server id (jobs).
	GetRunByID(ctx context.Context, id RunID) (Run, error)
	// History returns, oldest first, every result of one identity in one
	// environment whose run started at or after since — the input of §8.6.
	// Uses the §8.9 (stableTestId, createdAt) index.
	History(ctx context.Context, stableTestID, environmentID string, since time.Time) ([]HistoryPoint, error)
	// HistoryMany is History for many identities in one query, keyed by id.
	HistoryMany(ctx context.Context, stableTestIDs []string, environmentID string, since time.Time) (map[string][]HistoryPoint, error)
	// GetResult returns one TestResult by id within a project, for triage.
	GetResult(ctx context.Context, projectID string, id TestResultID) (TestResult, error)
	// SetFailureCategory records the §8.7 category a human accepted for a
	// result (§8.7: "người accept → TestResult.failureCategory").
	SetFailureCategory(ctx context.Context, id TestResultID, category string) error
}

// HistoryPoint is one run's outcome for an identity, for flaky scoring.
type HistoryPoint struct {
	RunID       RunID
	StartedAt   time.Time
	FinalStatus FinalStatus
	ObservedAt  time.Time // failure_observed_at, zero when the run passed
	Occurrence  int       // occurrence_index: the order of a test's results inside one run
}

// FinalStatusOf is the N41 table: the framework's outcome decides, and the
// last attempt's status only refines unexpected/expected. It is exported so
// the reporter's tests and ingest's tests can pin the same mapping.
func FinalStatusOf(outcome Outcome, last AttemptStatus) FinalStatus {
	switch outcome {
	case OutcomeFlaky:
		return FinalFlakyPass
	case OutcomeSkipped:
		return FinalSkipped
	case OutcomeExpected:
		if last == AttemptSkipped {
			return FinalSkipped
		}
		return FinalPassed
	case OutcomeUnexpected:
		switch last {
		case AttemptTimedOut:
			return FinalTimedOut
		case AttemptInterrupted:
			return FinalInterrupted
		}
		return FinalFailed
	}
	return FinalFailed
}
