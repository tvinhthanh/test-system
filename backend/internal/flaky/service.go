package flaky

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
)

type service struct {
	db      *pg.DB
	results results.Service
	now     func() time.Time
}

// New returns the Service over migrations/0007_flaky.sql.
func New(db *pg.DB, res results.Service) Service {
	return &service{db: db, results: res, now: time.Now}
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func isUUID(s string) bool { return uuidRe.MatchString(s) }

// Stats is the pure part of §8.6: counts, flips and the score, from an
// ordered history. Exported so the formula can be pinned by tests without
// a database and so a future recalibration is a diff on one function.
type Stats struct {
	TotalRuns, FailedRuns, FlakyPassRuns, FlipCount int
	LastOutcome                                     int // fold state: 1 pass, -1 fail, 0 none yet
	LastFailedAt                                    time.Time
	Score                                           float64
	HasScore                                        bool
	Verdict                                         Verdict
}

// Compute applies the §8.6 formula. A skipped result carries no signal and
// is dropped before counting; flaky_pass counts as a pass for the flip
// series (the test did pass) and separately as a flaky pass.
func Compute(history []results.HistoryPoint, now time.Time) Stats {
	var st Stats
	for _, h := range history {
		st.Apply(h)
	}
	return st.Finalize(now)
}

// Apply folds one more result into the counts. It is the whole of the
// incremental path: a finish applies its results to the stored fold state
// and the numbers are what Compute over the history would give.
func (st *Stats) Apply(h results.HistoryPoint) {
	var cur int
	switch h.FinalStatus {
	case results.FinalPassed:
		cur = 1
	case results.FinalFlakyPass:
		cur = 1
		st.FlakyPassRuns++
	case results.FinalFailed, results.FinalTimedOut, results.FinalInterrupted:
		cur = -1
		st.FailedRuns++
		at := h.ObservedAt
		if at.IsZero() {
			at = h.StartedAt
		}
		if at.After(st.LastFailedAt) {
			st.LastFailedAt = at
		}
	default:
		return // skipped: no evidence either way
	}
	st.TotalRuns++
	if st.LastOutcome != 0 && cur != st.LastOutcome {
		st.FlipCount++
	}
	st.LastOutcome = cur
}

// Finalize derives score and verdict from the counts (§8.6 formula).
func (st Stats) Finalize(now time.Time) Stats {
	st.Score, st.HasScore = 0, false
	if st.TotalRuns < 2 {
		st.Verdict = VerdictInsufficientData
		return st
	}
	recency := 0.0
	if !st.LastFailedAt.IsZero() {
		days := now.Sub(st.LastFailedAt).Hours() / 24
		recency = math.Max(0, math.Min(1, 1-days/WindowDays))
	}
	st.Score = WeightFlips*float64(st.FlipCount)/float64(st.TotalRuns-1) +
		WeightFlakyPass*float64(st.FlakyPassRuns)/float64(st.TotalRuns) +
		WeightRecency*recency
	st.Score = math.Round(st.Score*1000) / 1000
	st.HasScore = true
	switch {
	case st.Score >= QuarantineScore && st.TotalRuns >= QuarantineMinRuns:
		st.Verdict = VerdictQuarantine
	case st.Score >= ThresholdFlaky:
		st.Verdict = VerdictFlaky
	case st.Score >= ThresholdSuspect:
		st.Verdict = VerdictSuspect
	default:
		st.Verdict = VerdictStable
	}
	return st
}

const columns = `id, project_id, identity_id, environment_id, window_days, total_runs, failed_runs, flaky_pass_runs, flip_count, flaky_score, verdict, last_failed_at, computed_at, last_outcome, rebuilt_at`

func scan(row interface{ Scan(dest ...any) error }) (Signal, error) {
	var s Signal
	var score *float64
	var lastFailed *time.Time
	err := row.Scan(&s.ID, &s.ProjectID, &s.IdentityID, &s.EnvironmentID, &s.WindowDays, &s.TotalRuns, &s.FailedRuns, &s.FlakyPassRuns, &s.FlipCount, &score, &s.Verdict, &lastFailed, &s.ComputedAt, &s.LastOutcome, &s.RebuiltAt)
	if err != nil {
		return Signal{}, err
	}
	if score != nil {
		s.Score, s.HasScore = *score, true
	}
	if lastFailed != nil {
		s.LastFailedAt = lastFailed.UTC()
	}
	s.ComputedAt, s.RebuiltAt = s.ComputedAt.UTC(), s.RebuiltAt.UTC()
	return s, nil
}

func (s *service) Get(ctx context.Context, identityID, environmentID string) (Signal, error) {
	if !isUUID(identityID) || !isUUID(environmentID) {
		return Signal{}, ErrNotFound
	}
	sig, err := scan(s.db.Q(ctx).QueryRow(ctx, `SELECT `+columns+` FROM flaky_signals WHERE identity_id = $1 AND environment_id = $2`, identityID, environmentID))
	return sig, mapErr(err)
}

func (s *service) ListWorst(ctx context.Context, projectID string, limit int) ([]Signal, error) {
	if !isUUID(projectID) {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT `+columns+` FROM flaky_signals WHERE project_id = $1 AND flaky_score IS NOT NULL
		 ORDER BY flaky_score DESC, computed_at DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Signal
	for rows.Next() {
		sig, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sig)
	}
	return out, rows.Err()
}

func mapErr(err error) error {
	if errors.Is(err, pg.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (s *service) PurgeProject(ctx context.Context, projectID string) error {
	if !purgeUUID.MatchString(projectID) {
		return ErrNotFound
	}
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		for _, q := range []string{
			"DELETE FROM flaky_signals WHERE project_id = $1",
		} {
			if _, err := s.db.Q(ctx).Exec(ctx, q, projectID); err != nil {
				return fmt.Errorf("flaky: purge project: %w", err)
			}
		}
		return nil
	})
}

var purgeUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
