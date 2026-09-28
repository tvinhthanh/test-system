package billing

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

type service struct {
	db  *pg.DB
	src Source
}

// New returns the Service over migrations/0023_meter_daily.sql.
func New(db *pg.DB, src Source) Service { return &service{db: db, src: src} }

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *service) Record(ctx context.Context, day time.Time) (int, error) {
	d := utcDay(day)
	facts, err := s.src.MeterFacts(ctx, d)
	if err != nil {
		return 0, err
	}
	err = s.db.WithTx(ctx, func(ctx context.Context) error {
		for _, f := range facts {
			if _, err := s.db.Q(ctx).Exec(ctx, `
				INSERT INTO meter_daily (project_id, org_id, day, active_tests, raw_fingerprints, merges, undo_merges, splits,
				                         review_decisions, runs, results, artifact_bytes, recorded_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, now())
				ON CONFLICT (project_id, day) DO UPDATE SET
				  org_id = EXCLUDED.org_id, active_tests = EXCLUDED.active_tests, raw_fingerprints = EXCLUDED.raw_fingerprints,
				  merges = EXCLUDED.merges, undo_merges = EXCLUDED.undo_merges, splits = EXCLUDED.splits,
				  review_decisions = EXCLUDED.review_decisions, runs = EXCLUDED.runs, results = EXCLUDED.results,
				  artifact_bytes = EXCLUDED.artifact_bytes, recorded_at = now()`,
				f.ProjectID, f.OrgID, d.Format("2006-01-02"), f.ActiveTests, f.RawFingerprints, f.Merges, f.UndoMerges, f.Splits,
				f.ReviewDecisions, f.Runs, f.Results, f.ArtifactBytes); err != nil {
				return fmt.Errorf("billing: record %s: %w", f.ProjectID, err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(facts), nil
}

func (s *service) Recorded(ctx context.Context, day time.Time) (bool, error) {
	var ok bool
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM meter_daily WHERE day = $1)`, utcDay(day).Format("2006-01-02")).Scan(&ok)
	return ok, err
}

func (s *service) Report(ctx context.Context, orgID string, from, to time.Time) (Report, error) {
	if !uuidRe.MatchString(orgID) {
		return Report{}, ErrNotFound
	}
	f, t := utcDay(from), utcDay(to)
	if t.Before(f) || t.Sub(f) > 400*24*time.Hour {
		return Report{}, fmt.Errorf("%w: window: from ≤ to, at most 400 days", ErrInvalid)
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT day, sum(active_tests), sum(raw_fingerprints), sum(merges + undo_merges + splits + review_decisions),
		       sum(runs), sum(results), sum(artifact_bytes)
		  FROM meter_daily
		 WHERE org_id = $1 AND day BETWEEN $2 AND $3
		 GROUP BY day ORDER BY day`, orgID, f.Format("2006-01-02"), t.Format("2006-01-02"))
	if err != nil {
		return Report{}, err
	}
	defer rows.Close()
	var days []Day
	for rows.Next() {
		var d Day
		if err := rows.Scan(&d.Day, &d.ActiveTests, &d.RawFingerprints, &d.Corrections, &d.Runs, &d.Results, &d.ArtifactBytes); err != nil {
			return Report{}, err
		}
		d.Day = d.Day.UTC()
		days = append(days, d)
	}
	if err := rows.Err(); err != nil {
		return Report{}, err
	}
	return judge(orgID, days), nil
}

// growth is the relative change from a to b; 0 when both are 0.
func growth(a, b int) float64 {
	switch {
	case a == 0 && b == 0:
		return 0
	case a == 0:
		return 1 // from nothing to something: the whole count is new
	default:
		return float64(b-a) / float64(a)
	}
}

// judge is §10.6's rule over recorded days, oldest first: drift per day
// against the previous one, a flag above DriftLimit, ready after
// RequiredDays with no flag.
func judge(orgID string, days []Day) Report {
	r := Report{OrgID: orgID, Days: days}
	flagged := 0
	for i := range r.Days {
		if i > 0 {
			prev, cur := r.Days[i-1], r.Days[i]
			r.Days[i].Drift = math.Abs(growth(prev.ActiveTests, cur.ActiveTests) - growth(prev.RawFingerprints, cur.RawFingerprints))
			r.Days[i].Flagged = r.Days[i].Drift > DriftLimit
		}
		if r.Days[i].Flagged {
			flagged++
		}
	}
	if len(days) < RequiredDays {
		r.Reasons = append(r.Reasons, fmt.Sprintf("%d of %d days recorded", len(days), RequiredDays))
	}
	if flagged > 0 {
		r.Reasons = append(r.Reasons, fmt.Sprintf("%d day(s) where identity changes moved the active-test count more than 2%% away from the raw fingerprints", flagged))
	}
	r.Ready = len(r.Reasons) == 0
	return r
}
