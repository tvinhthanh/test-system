// Package flaky owns FlakySignal (§6.9.5, §8.6): one row per
// (identity, environment) holding the current verdict, rebuilt from the
// last 30 days of results whenever a run of that identity finishes. Its
// only input is results.History — it never reads results' tables — and
// its output is what the dashboard and, later, quarantine act on.
//
// The formula and thresholds are §8.6's starting points, marked
// [ASSUMPTION] there: recalibrate on partner data and record changes in the
// schema changelog. A human MARKED_FLAKY (§8.3) is ground truth for that
// recalibration and never overrides the score.
//
// This file is the whole contract (§6.9.4). Domain fan-out: results only.
package flaky

import (
	"context"
	"errors"
	"time"
)

type Verdict string

const (
	VerdictInsufficientData Verdict = "insufficient_data"
	VerdictStable           Verdict = "stable"
	VerdictSuspect          Verdict = "suspect"
	VerdictFlaky            Verdict = "flaky"
	VerdictQuarantine       Verdict = "quarantine_suggested"
)

// Signal is §8.6 FlakySignal.
type Signal struct {
	ID            string
	ProjectID     string
	IdentityID    string
	EnvironmentID string
	WindowDays    int
	TotalRuns     int
	FailedRuns    int
	FlakyPassRuns int
	FlipCount     int
	Score         float64 // meaningful only when HasScore
	HasScore      bool
	Verdict       Verdict
	LastFailedAt  time.Time
	ComputedAt    time.Time
}

// Tunables of §8.6, all [ASSUMPTION].
const (
	WindowDays        = 30
	WeightFlips       = 0.5
	WeightFlakyPass   = 0.3
	WeightRecency     = 0.2
	ThresholdFlaky    = 0.6
	ThresholdSuspect  = 0.3
	QuarantineScore   = 0.8 // flaky this strongly, with enough runs, deserves a quarantine suggestion
	QuarantineMinRuns = 10
)

var ErrNotFound = errors.New("flaky: not found")

// Service is the contract.
type Service interface {
	// RecomputeRun rebuilds the signal of every resolved identity in a
	// finished run (§8.6: "recompute on run finish, incremental"). Results
	// without a stable id are skipped. Returns how many signals it wrote.
	RecomputeRun(ctx context.Context, projectID, runID string) (int, error)
	// Recompute rebuilds one signal from results.History.
	Recompute(ctx context.Context, projectID, identityID, environmentID string) (Signal, error)
	Get(ctx context.Context, identityID, environmentID string) (Signal, error)
	// ListWorst returns a project's signals with a score, highest first.
	ListWorst(ctx context.Context, projectID string, limit int) ([]Signal, error)
}
