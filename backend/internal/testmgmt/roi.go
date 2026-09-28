package testmgmt

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ROI (§6.8.3, QĐ-13). The three rules are the design:
//
//  1. coverage and untriaged rate travel with every number (the ROI value
//     carries them; the screen shows them first);
//  2. hours, never money — a rate is the reader's, applied in the browser;
//  3. a measured baseline (§5.4) or "not enough data": SavedHours is nil
//     without one, and nothing here invents a default.
//
// Every fact comes from Evidence (the Intelligence Plane); this file only
// does the arithmetic of §6.8.3.

const baselineColumns = `id, project_id, kind, failures, median_seconds, coalesce(p90_seconds, 0), rerun_pass_rate,
	coalesce(flaky_investigation_seconds, 0), period_from, period_to, note, entered_by, entered_at`

func scanBaseline(row interface{ Scan(...any) error }) (Baseline, error) {
	var b Baseline
	var median, p90, invest int
	err := row.Scan(&b.ID, &b.ProjectID, &b.Kind, &b.Failures, &median, &p90, &b.RerunPassRate,
		&invest, &b.PeriodFrom, &b.PeriodTo, &b.Note, &b.EnteredBy, &b.EnteredAt)
	b.Median, b.P90, b.FlakyInvestigation = time.Duration(median)*time.Second, time.Duration(p90)*time.Second, time.Duration(invest)*time.Second
	b.PeriodFrom, b.PeriodTo, b.EnteredAt = b.PeriodFrom.UTC(), b.PeriodTo.UTC(), b.EnteredAt.UTC()
	return b, err
}

func (s *service) AddBaseline(ctx context.Context, projectID string, in NewBaseline, actor string) (Baseline, error) {
	if !isUUID(projectID) {
		return Baseline{}, ErrNotFound
	}
	switch {
	case in.Kind != Retrospective && in.Kind != Prospective:
		return Baseline{}, fmt.Errorf("%w: kind must be retrospective or prospective (§5.4)", ErrInvalid)
	case in.Failures <= 0 || in.Failures > 1_000_000:
		return Baseline{}, fmt.Errorf("%w: failures: how many failures the study measured", ErrInvalid)
	case in.Median < time.Minute || in.Median > 90*24*time.Hour:
		return Baseline{}, fmt.Errorf("%w: medianMinutes: between 1 minute and 90 days", ErrInvalid)
	case in.P90 != 0 && (in.P90 < in.Median || in.P90 > 365*24*time.Hour):
		return Baseline{}, fmt.Errorf("%w: p90Minutes: at least the median", ErrInvalid)
	case in.RerunPassRate != nil && (*in.RerunPassRate < 0 || *in.RerunPassRate > 1):
		return Baseline{}, fmt.Errorf("%w: rerunPassRate: a fraction between 0 and 1", ErrInvalid)
	case in.FlakyInvestigation < 0 || in.FlakyInvestigation > 30*24*time.Hour:
		return Baseline{}, fmt.Errorf("%w: flakyInvestigationMinutes: at most 30 days", ErrInvalid)
	case in.PeriodFrom.IsZero() || in.PeriodTo.IsZero() || in.PeriodTo.Before(in.PeriodFrom):
		return Baseline{}, fmt.Errorf("%w: periodFrom/periodTo: the dates the study covered, from ≤ to", ErrInvalid)
	case in.PeriodTo.After(s.now().UTC().Add(24 * time.Hour)):
		return Baseline{}, fmt.Errorf("%w: periodTo: a baseline is measured, so it cannot end in the future", ErrInvalid)
	case len(in.Note) > 2000 || strings.TrimSpace(actor) == "":
		return Baseline{}, fmt.Errorf("%w: note too long or no actor", ErrInvalid)
	}
	var p90, invest *int
	if in.P90 > 0 {
		v := int(in.P90 / time.Second)
		p90 = &v
	}
	if in.FlakyInvestigation > 0 {
		v := int(in.FlakyInvestigation / time.Second)
		invest = &v
	}
	b, err := scanBaseline(s.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO ttrc_baselines (project_id, kind, failures, median_seconds, p90_seconds, rerun_pass_rate,
			flaky_investigation_seconds, period_from, period_to, note, entered_by, entered_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING `+baselineColumns,
		projectID, string(in.Kind), in.Failures, int(in.Median/time.Second), p90, in.RerunPassRate, invest,
		in.PeriodFrom.UTC().Format("2006-01-02"), in.PeriodTo.UTC().Format("2006-01-02"), strings.TrimSpace(in.Note),
		strings.TrimSpace(actor), s.now().UTC()))
	return b, mapErr(err)
}

func (s *service) Baselines(ctx context.Context, projectID string) ([]Baseline, error) {
	if !isUUID(projectID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT `+baselineColumns+` FROM ttrc_baselines WHERE project_id = $1 ORDER BY entered_at DESC LIMIT 100`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Baseline{}
	for rows.Next() {
		b, err := scanBaseline(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// applicable is §5.4's rule: prospective is the official baseline;
// retrospective stands in until there is one.
func applicable(all []Baseline) *Baseline {
	for _, kind := range []BaselineKind{Prospective, Retrospective} {
		for i := range all { // newest first
			if all[i].Kind == kind {
				return &all[i]
			}
		}
	}
	return nil
}

func (s *service) ROI(ctx context.Context, projectID string, month time.Time) (ROI, error) {
	if !isUUID(projectID) {
		return ROI{}, ErrNotFound
	}
	m := month.UTC()
	from := time.Date(m.Year(), m.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	facts, err := s.evidence.ROIFacts(ctx, projectID, from, to)
	if err != nil {
		return ROI{}, err
	}
	all, err := s.Baselines(ctx, projectID)
	if err != nil {
		return ROI{}, err
	}
	return computeROI(from, to, facts, applicable(all)), nil
}

// computeROI is §6.8.3's arithmetic, pure.
func computeROI(from, to time.Time, f ROIFacts, b *Baseline) ROI {
	out := ROI{From: from, To: to, Facts: f, Baseline: b, FewTriaged: f.Triaged < ROIMinTriaged}
	if f.Failures > 0 {
		out.TriageCoverage = float64(f.Triaged) / float64(f.Failures)
		out.UntriagedRate = 1 - out.TriageCoverage
	}
	flaky := f.FlakyRetry
	if b != nil {
		// Σ (baseline − TTRC_i) = n × baseline − Σ TTRC_i, over the triaged.
		saved := (time.Duration(f.Triaged)*b.Median - f.TTRCSum).Hours()
		out.SavedHours = &saved
		if b.FlakyInvestigation > 0 {
			flaky += time.Duration(f.MarkedFlaky) * b.FlakyInvestigation
			out.InvestigationCounted = true
		}
	}
	out.FlakyHours = flaky.Hours()
	return out
}
