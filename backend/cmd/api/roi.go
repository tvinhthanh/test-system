package main

import (
	"net/http"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/testmgmt"
)

// ROI over HTTP (§6.8.3, QĐ-13). Reading is for everyone who can view the
// project — the EM who approves budget is the reader (§2.5); entering a
// measured baseline (§5.4) is configuring the project (maintainer and up).
// Hours only: money is the reader's own rate, applied in the browser and
// never sent here (rule 2).
//
//	GET  /v1/projects/{projectId}/roi?month=2026-09
//	GET  /v1/projects/{projectId}/roi/baselines
//	POST /v1/projects/{projectId}/roi/baselines

func (d *dashboard) roiRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("GET /v1/projects/{projectId}/roi", d.roi)
	handle("GET /v1/projects/{projectId}/roi/baselines", d.roiBaselines)
	handle("POST /v1/projects/{projectId}/roi/baselines", d.roiAddBaseline)
}

type baselineDTO struct {
	ID                        string    `json:"id"`
	Kind                      string    `json:"kind"`
	Failures                  int       `json:"failures"`
	MedianMinutes             float64   `json:"medianMinutes"`
	P90Minutes                *float64  `json:"p90Minutes"`
	RerunPassRate             *float64  `json:"rerunPassRate"`
	FlakyInvestigationMinutes *float64  `json:"flakyInvestigationMinutes"`
	PeriodFrom                string    `json:"periodFrom"`
	PeriodTo                  string    `json:"periodTo"`
	Note                      string    `json:"note"`
	EnteredBy                 string    `json:"enteredBy"`
	EnteredAt                 time.Time `json:"enteredAt"`
}

func minutesOrNil(d time.Duration) *float64 {
	if d <= 0 {
		return nil
	}
	m := d.Minutes()
	return &m
}

func baselineOut(b testmgmt.Baseline) baselineDTO {
	return baselineDTO{
		ID: b.ID, Kind: string(b.Kind), Failures: b.Failures, MedianMinutes: b.Median.Minutes(),
		P90Minutes: minutesOrNil(b.P90), RerunPassRate: b.RerunPassRate, FlakyInvestigationMinutes: minutesOrNil(b.FlakyInvestigation),
		PeriodFrom: b.PeriodFrom.Format("2006-01-02"), PeriodTo: b.PeriodTo.Format("2006-01-02"),
		Note: b.Note, EnteredBy: b.EnteredBy, EnteredAt: b.EnteredAt,
	}
}

type roiDTO struct {
	Month          string       `json:"month"`
	From           time.Time    `json:"from"`
	To             time.Time    `json:"to"`
	Failures       int          `json:"failures"`
	Triaged        int          `json:"triaged"`
	TriageCoverage float64      `json:"triageCoverage"`
	UntriagedRate  float64      `json:"untriagedRate"`
	TTRCMedianMin  *float64     `json:"ttrcMedianMinutes"` // null when nothing was triaged
	Baseline       *baselineDTO `json:"baseline"`          // null: "not enough data"
	SavedHours     *float64     `json:"savedHours"`        // null without a baseline
	FlakyHours     float64      `json:"flakyHours"`
	FlakyRetryHrs  float64      `json:"flakyRetryHours"`
	MarkedFlaky    int          `json:"markedFlaky"`
	// investigationCounted: the baseline measured an investigation time, so
	// markedFlaky × it is in flakyHours.
	InvestigationCounted bool `json:"investigationCounted"`
	FewTriaged           bool `json:"fewTriaged"`
	MinTriaged           int  `json:"minTriaged"`
}

func (d *dashboard) roi(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionView)
	if !ok {
		return
	}
	month := time.Now().UTC()
	if s := r.URL.Query().Get("month"); s != "" {
		t, err := time.Parse("2006-01", s)
		if err != nil || t.Year() < 2000 || t.After(month) {
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_QUERY", "month: YYYY-MM, not in the future", map[string]any{"field": "month"})
			return
		}
		month = t
	}
	roi, err := d.tm.ROI(r.Context(), string(scope.ProjectID), month)
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	out := roiDTO{
		Month: roi.From.Format("2006-01"), From: roi.From, To: roi.To,
		Failures: roi.Facts.Failures, Triaged: roi.Facts.Triaged,
		TriageCoverage: roi.TriageCoverage, UntriagedRate: roi.UntriagedRate,
		TTRCMedianMin: minutesOrNil(roi.Facts.TTRCMedian),
		SavedHours:    roi.SavedHours, FlakyHours: roi.FlakyHours, FlakyRetryHrs: roi.Facts.FlakyRetry.Hours(),
		MarkedFlaky: roi.Facts.MarkedFlaky, InvestigationCounted: roi.InvestigationCounted,
		FewTriaged: roi.FewTriaged, MinTriaged: testmgmt.ROIMinTriaged,
	}
	if roi.Baseline != nil {
		b := baselineOut(*roi.Baseline)
		out.Baseline = &b
	}
	writeJSON(w, http.StatusOK, out)
}

func (d *dashboard) roiBaselines(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionView)
	if !ok {
		return
	}
	list, err := d.tm.Baselines(r.Context(), string(scope.ProjectID))
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listOut(list, baselineOut))
}

func (d *dashboard) roiAddBaseline(w http.ResponseWriter, r *http.Request) {
	project, actor, ok := d.writer(w, r, authz.ActionProjectConfigure)
	if !ok {
		return
	}
	var req struct {
		Kind                      string   `json:"kind"`
		Failures                  int      `json:"failures"`
		MedianMinutes             float64  `json:"medianMinutes"`
		P90Minutes                *float64 `json:"p90Minutes"`
		RerunPassRate             *float64 `json:"rerunPassRate"`
		FlakyInvestigationMinutes *float64 `json:"flakyInvestigationMinutes"`
		PeriodFrom                string   `json:"periodFrom"`
		PeriodTo                  string   `json:"periodTo"`
		Note                      string   `json:"note"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := decodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "malformed JSON", nil)
		return
	}
	minutes := func(m *float64) time.Duration {
		if m == nil {
			return 0
		}
		return time.Duration(*m * float64(time.Minute))
	}
	from, ferr := time.Parse("2006-01-02", req.PeriodFrom)
	to, terr := time.Parse("2006-01-02", req.PeriodTo)
	if ferr != nil || terr != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "periodFrom/periodTo: YYYY-MM-DD", map[string]any{"field": "periodFrom"})
		return
	}
	b, err := d.tm.AddBaseline(r.Context(), project, testmgmt.NewBaseline{
		Kind: testmgmt.BaselineKind(req.Kind), Failures: req.Failures, Median: minutes(&req.MedianMinutes),
		P90: minutes(req.P90Minutes), RerunPassRate: req.RerunPassRate, FlakyInvestigation: minutes(req.FlakyInvestigationMinutes),
		PeriodFrom: from, PeriodTo: to, Note: req.Note,
	}, actor)
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, baselineOut(b))
}
