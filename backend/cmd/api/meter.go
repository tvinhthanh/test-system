package main

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/billing"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/query"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

// The §10.6 shadow meter for an organization (QĐ-13): the owner's, since
// billing is the owner's (§11.3.1 org_owner). Counted by the worker once a
// day, never charged; the answer carries §10.6's verdict on whether the
// active-test count could be a billing meter.
//
//	GET /v1/organizations/{organizationId}/meter?days=28

func (d *dashboard) meterRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("GET /v1/organizations/{organizationId}/meter", d.orgMeter)
}

type meterDayDTO struct {
	Day             string  `json:"day"`
	ActiveTests     int     `json:"activeTests"`
	RawFingerprints int     `json:"rawFingerprints"`
	Corrections     int     `json:"corrections"`
	Runs            int     `json:"runs"`
	Results         int     `json:"results"`
	ArtifactBytes   int64   `json:"artifactBytes"`
	Drift           float64 `json:"drift"`
	Flagged         bool    `json:"flagged"`
}

func (d *dashboard) orgMeter(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	org, err := d.tenancy.GetOrganization(r.Context(), tenancy.OrganizationID(r.PathValue("organizationId")))
	if err != nil {
		d.nodeErr(w, r, err, "ORGANIZATION_NOT_FOUND", "organization not found")
		return
	}
	if !d.authorizeNode(w, r, p, authz.Resource{OrgID: string(org.ID)}, authz.ActionOrgManage, "ORGANIZATION_NOT_FOUND", "organization not found") {
		return
	}
	days := 28
	if s := r.URL.Query().Get("days"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 366 {
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_QUERY", "days: 1 to 366", map[string]any{"field": "days"})
			return
		}
		days = n
	}
	// Up to yesterday: today is recorded tomorrow, once it is whole.
	to := time.Now().UTC().AddDate(0, 0, -1)
	rep, err := d.meter.Report(r.Context(), string(org.ID), to.AddDate(0, 0, -(days-1)), to)
	if err != nil {
		d.internal(w, r, err)
		return
	}
	out := make([]meterDayDTO, 0, len(rep.Days))
	for _, x := range rep.Days {
		out = append(out, meterDayDTO{Day: x.Day.Format("2006-01-02"), ActiveTests: x.ActiveTests, RawFingerprints: x.RawFingerprints,
			Corrections: x.Corrections, Runs: x.Runs, Results: x.Results, ArtifactBytes: x.ArtifactBytes, Drift: x.Drift, Flagged: x.Flagged})
	}
	reasons := rep.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"days": out, "ready": rep.Ready, "reasons": reasons,
		"requiredDays": billing.RequiredDays, "driftLimit": billing.DriftLimit,
	})
}

// meterSource adapts query's read model to billing.Source; the api only
// reads reports, the worker records (cmd/worker has the same adapter).
type meterSource struct{ q query.Service }

func (m meterSource) MeterFacts(ctx context.Context, day time.Time) ([]billing.Facts, error) {
	rows, err := m.q.MeterFacts(ctx, day)
	out := make([]billing.Facts, 0, len(rows))
	for _, r := range rows {
		out = append(out, billing.Facts{
			ProjectID: r.ProjectID, OrgID: r.OrgID, ActiveTests: r.ActiveTests, RawFingerprints: r.RawFingerprints,
			Merges: r.Merges, UndoMerges: r.UndoMerges, Splits: r.Splits, ReviewDecisions: r.ReviewDecisions,
			Runs: r.Runs, Results: r.Results, ArtifactBytes: r.ArtifactBytes,
		})
	}
	return out, err
}
