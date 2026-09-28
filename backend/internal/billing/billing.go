// Package billing is the §10.6 shadow meter (QĐ-13): it counts what a bill
// would be made of, every day, and never charges — §10.5 "trước Gate-1.5
// chỉ chạy shadow meter", no invoice, no quota stop reads it.
//
// The trial billing axis is the active-test count (stableTestIds with a
// result in 30 days). It is not trusted yet because identity merges and
// splits move it (N19); §10.6's check is kept here as code: the meter is
// compared day by day with the raw distinct client fingerprints, and a day
// where the two grow apart by more than 2 % is flagged, with the identity
// corrections of that day beside it. Four weeks without a flagged day is
// the condition for the axis to be a billing meter at all.
//
// Facts come through Source (the dashboard read model measures them; this
// module does not read other modules' tables). Owns migration 0023.
//
// This file is the whole contract (§6.9.4).
package billing

import (
	"context"
	"errors"
	"time"
)

// Facts is one project's counts for one UTC day.
type Facts struct {
	ProjectID       string
	OrgID           string
	ActiveTests     int // stable ids (merged folded into their target) with a result in the 30 days ending that day
	RawFingerprints int // distinct clientTestIds over the same 30 days
	Merges          int // identity corrections that day
	UndoMerges      int
	Splits          int
	ReviewDecisions int
	Runs            int   // runs started that day
	Results         int   // results of those runs
	ArtifactBytes   int64 // verified, unexpired artifact bytes at the end of the day
}

// Source measures a day for every project; cmd/worker satisfies it with
// query.
type Source interface {
	MeterFacts(ctx context.Context, day time.Time) ([]Facts, error)
}

// Day is one recorded day, summed over an organization's projects.
type Day struct {
	Day             time.Time
	ActiveTests     int
	RawFingerprints int
	Corrections     int // merges + undo merges + splits + review decisions
	Runs            int
	Results         int
	ArtifactBytes   int64
	// Drift is |growth(active) − growth(raw)| against the previous recorded
	// day, as a fraction; 0 on the first day or when both were 0.
	Drift   float64
	Flagged bool // Drift > DriftLimit
}

// Report is §10.6's evidence for one organization over a window.
type Report struct {
	OrgID string
	Days  []Day // oldest first
	// Ready: at least RequiredDays recorded and no flagged day — only then
	// is the active-test count usable as a billing meter. Reasons says why
	// not, in words, when it is not.
	Ready   bool
	Reasons []string
}

const (
	// DriftLimit is §10.6's 2 %.
	DriftLimit = 0.02
	// RequiredDays is §10.6's four weeks.
	RequiredDays = 28
)

var (
	ErrNotFound = errors.New("billing: not found")
	ErrInvalid  = errors.New("billing: invalid input")
)

type Service interface {
	// Record measures one UTC day through Source and stores it; recording
	// the same day again replaces it. It returns how many projects it wrote.
	Record(ctx context.Context, day time.Time) (int, error)
	// Recorded reports whether a day has any row — the worker's "already
	// done" check.
	Recorded(ctx context.Context, day time.Time) (bool, error)
	// Report sums an organization's days in [from, to] and judges them.
	Report(ctx context.Context, orgID string, from, to time.Time) (Report, error)
}
