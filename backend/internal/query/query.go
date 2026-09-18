// Package query is the dashboard read model (§6.9.5): the one module
// allowed to JOIN across other modules' tables, and forbidden to write a
// single row — test/arch scans its source for INSERT/UPDATE/DELETE. It
// carries no business rule, only the DTOs of frontend/API_CONTRACT.md, and
// it is the module we agree to rewrite the day results is split out.
//
// Every query carries the project id in its WHERE clause (§11.3.3): a
// caller that has authorized the project gets that project's rows and
// nothing else. Lists are cursor-paged (§7.10: pagination mandatory, page
// ≤ 100) with a stable tie-breaker on id; a cursor is opaque to the client
// and bound to the project, filter and sort that produced it.
//
// This file is the whole contract (§6.9.4). query imports platform/* and
// tenancy only (§6.9.3).
package query

import (
	"context"
	"errors"
	"time"
)

// Page is the contract's list envelope.
type Page[T any] struct {
	Items      []T
	NextCursor string // "" = no more
}

// Run is the contract's Run DTO. passedCount excludes flaky passes;
// failedCount is failed + timed_out + interrupted; DurationMs is nil while
// the run is open.
type Run struct {
	ID              string     `json:"id"`
	Branch          string     `json:"branch"`
	CommitSHA       string     `json:"commitSha"`
	Status          string     `json:"status"`
	EnvironmentName string     `json:"environmentName"`
	StartedAt       time.Time  `json:"startedAt"`
	DurationMs      *int64     `json:"durationMs"`
	TotalCount      int        `json:"totalCount"`
	PassedCount     int        `json:"passedCount"`
	FailedCount     int        `json:"failedCount"`
	FlakyCount      int        `json:"flakyCount"`
	FinishedAt      *time.Time `json:"finishedAt,omitempty"`
}

// RunFilter is the run list's query string.
type RunFilter struct {
	Q           string // ≤ 200 chars; matches branch, commit and localRunId
	Status      string
	Environment string // environment key
	Limit       int
	Cursor      string
}

// Result is the contract's TestResult DTO.
type Result struct {
	ID           string  `json:"id"`
	DisplayName  string  `json:"displayName"`
	FilePath     string  `json:"filePath"`
	FinalStatus  string  `json:"finalStatus"`
	AttemptCount int     `json:"attemptCount"`
	DurationMs   int     `json:"durationMs"`
	StableTestID *string `json:"stableTestId"`
	// Not in the list DTO but cheap and used by the detail view.
	FailureCategory   string     `json:"failureCategory,omitempty"`
	FailureObservedAt *time.Time `json:"failureObservedAt,omitempty"`
}

// Attempt is one row of the result detail, attempt ASC.
type Attempt struct {
	Attempt    int           `json:"attempt"`
	Status     string        `json:"status"`
	DurationMs int           `json:"durationMs"`
	Error      *AttemptError `json:"error"`
	// Artifacts are the verified uploads linked to this attempt (§7.7), oldest
	// first, at most 20; bytes come from GET …/artifacts/{id}/content.
	Artifacts []ArtifactRef `json:"artifacts"`
}

// ArtifactRef is what the failure detail needs to list and link an upload.
type ArtifactRef struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	ContentType string    `json:"contentType"`
	SizeBytes   int64     `json:"sizeBytes"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type AttemptError struct {
	Message *string `json:"message"`
	Stack   *string `json:"stack"`
}

// TriageEvent is one row of the result detail's events.
type TriageEvent struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	ActorName  string    `json:"actorName"`
	OccurredAt time.Time `json:"occurredAt"`
}

// ResultDetail is GET /results/{id}.
type ResultDetail struct {
	Result   Result        `json:"result"`
	RunID    string        `json:"runId"`
	Attempts []Attempt     `json:"attempts"`
	Events   []TriageEvent `json:"events"`
}

// Identity is the contract's identity DTO. FlakyScore is nil and Verdict
// "insufficient_data" until §8.6 has two runs; the score shown is the worst
// across environments.
type Identity struct {
	ID                string   `json:"id"`
	CanonicalTitle    string   `json:"canonicalTitle"`
	CanonicalFilePath string   `json:"canonicalFilePath"`
	Verdict           string   `json:"verdict"`
	FlakyScore        *float64 `json:"flakyScore"`
	TotalRuns         int      `json:"totalRuns"`
	RecentStatuses    []string `json:"recentStatuses"` // newest first, ≤ 30
}

// TriageMetrics is §8.3 / §6.4 product row over a window.
type TriageMetrics struct {
	Failures          int
	Triaged           int
	Seen              int
	UntriagedRate     float64
	TriageCoverage    float64
	TTRCMedian        time.Duration
	TimeToFirstView   time.Duration
	HasTTRC, HasFirst bool
}

// TrendBucket is §8.7's "failure category trends".
type TrendBucket struct {
	Week     time.Time `json:"week"`     // start of the ISO week, UTC
	Category string    `json:"category"` // §8.7 enum; "unknown" = not triaged yet
	Count    int       `json:"count"`
}

var (
	ErrNotFound = errors.New("query: not found")
	// ErrBadCursor: the cursor is not one this endpoint issued for this
	// project/filter — the client must restart from the first page.
	ErrBadCursor = errors.New("query: invalid cursor")
	ErrInvalid   = errors.New("query: invalid input")
)

// Service is the contract. Limits above 100 are clamped.
type Service interface {
	Runs(ctx context.Context, projectID string, f RunFilter) (Page[Run], error)
	Run(ctx context.Context, projectID, runID string) (Run, error)
	Results(ctx context.Context, projectID, runID string, limit int, cursor string) (Page[Result], error)
	ResultDetail(ctx context.Context, projectID, resultID string) (ResultDetail, error)
	// Identities lists a project's active identities; view "flaky" keeps
	// only suspect/flaky/quarantine_suggested (server-side filter).
	Identities(ctx context.Context, projectID, view string, limit int, cursor string) (Page[Identity], error)
	Identity(ctx context.Context, projectID, identityID string) (Identity, error)
	// IdentityRuns is the identity's history as Run DTOs, newest first.
	IdentityRuns(ctx context.Context, projectID, identityID string, limit int, cursor string) (Page[Run], error)

	Triage(ctx context.Context, projectID string, since time.Time) (TriageMetrics, error)
	CategoryTrend(ctx context.Context, projectID string, since time.Time) ([]TrendBucket, error)
}
