package main

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/testmgmt"
)

// ---- suites ------------------------------------------------------------------

func (d *dashboard) tmSuites(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	items, err := d.tm.ListSuites(r.Context(), string(scope.ProjectID))
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listOut(items, suiteOut))
}

func (d *dashboard) tmCreateSuite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ParentID string `json:"parentId"`
		Name     string `json:"name"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	project, _, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	s, err := d.tm.CreateSuite(r.Context(), project, req.ParentID, req.Name)
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, suiteOut(s))
}

// ---- requirements --------------------------------------------------------------

func (d *dashboard) tmRequirements(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	items, err := d.tm.ListRequirements(r.Context(), string(scope.ProjectID), limitParam(r, 200))
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listOut(items, requirementOut))
}

func (d *dashboard) tmCreateRequirement(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ExternalKey string `json:"externalKey"`
		Title       string `json:"title"`
		Priority    string `json:"priority"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	project, _, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	out, err := d.tm.CreateRequirement(r.Context(), project, testmgmt.NewRequirement{ExternalKey: req.ExternalKey, Title: req.Title, Priority: testmgmt.Priority(req.Priority)})
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, requirementOut(out))
}

func (d *dashboard) tmPatchRequirement(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title    *string `json:"title"`
		Priority *string `json:"priority"`
		Status   *string `json:"status"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	project, _, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	out, err := d.tm.UpdateRequirement(r.Context(), project, r.PathValue("requirementId"), testmgmt.RequirementPatch{
		Title: req.Title, Priority: (*testmgmt.Priority)(req.Priority), Status: (*testmgmt.RequirementStatus)(req.Status)})
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, requirementOut(out))
}

// ---- test cases ----------------------------------------------------------------

func (d *dashboard) tmCases(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	f := caseFilterFromRequest(r)
	items, err := d.tm.ListTestCases(r.Context(), string(scope.ProjectID), f)
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listOut(items, caseOut))
}

func caseFilterFromRequest(r *http.Request) testmgmt.CaseFilter {
	q := r.URL.Query()
	f := testmgmt.CaseFilter{SuiteID: q.Get("suite"), AutomationStatus: testmgmt.AutomationStatus(q.Get("automation")), Query: q.Get("q"), Limit: limitParam(r, 200)}
	f.Level, f.Purpose, f.Tag, f.ExecutionMode, f.PerformanceType = q.Get("level"), q.Get("purpose"), q.Get("tag"), q.Get("executionMode"), q.Get("performanceType")
	if q.Get("critical") == "true" || q.Get("critical") == "false" {
		v := q.Get("critical") == "true"
		f.Critical = &v
	}
	return f
}

func (d *dashboard) tmExportCases(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	var ids []string
	if text, exists := r.URL.Query()["ids"]; exists {
		ids = []string{}
		if len(text) == 1 && text[0] != "" {
			ids = strings.Split(text[0], ",")
		}
		if len(text) != 1 {
			d.tmErr(w, r, testmgmt.ErrInvalid)
			return
		}
	}
	out, err := d.tm.ExportCases(r.Context(), string(scope.ProjectID), caseFilterFromRequest(r), ids)
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

type bulkCaseResult struct {
	ID      string `json:"id"`
	Updated bool   `json:"updated"`
	Code    string `json:"code"`
}

func (d *dashboard) tmBulkCases(w http.ResponseWriter, r *http.Request) {
	project, actor, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	var req struct {
		Items []struct {
			ID                string    `json:"id"`
			ExpectedUpdatedAt time.Time `json:"expectedUpdatedAt"`
		} `json:"items"`
		Patch struct {
			Taxonomy         *testmgmt.Taxonomy         `json:"taxonomy"`
			Priority         *testmgmt.Priority         `json:"priority"`
			IsCritical       *bool                      `json:"isCritical"`
			SuiteID          *string                    `json:"suiteId"`
			AutomationStatus *testmgmt.AutomationStatus `json:"automationStatus"`
		} `json:"patch"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	if len(req.Items) == 0 || len(req.Items) > 100 {
		d.tmErr(w, r, testmgmt.ErrInvalid)
		return
	}
	if req.Patch.Taxonomy == nil && req.Patch.Priority == nil && req.Patch.IsCritical == nil && req.Patch.SuiteID == nil && req.Patch.AutomationStatus == nil {
		d.tmErr(w, r, testmgmt.ErrInvalid)
		return
	}
	if req.Patch.AutomationStatus != nil && *req.Patch.AutomationStatus == testmgmt.Automated {
		d.tmErr(w, r, testmgmt.ErrInvalid)
		return
	}
	seen := map[string]bool{}
	for _, item := range req.Items {
		if seen[item.ID] || item.ExpectedUpdatedAt.IsZero() {
			d.tmErr(w, r, testmgmt.ErrInvalid)
			return
		}
		seen[item.ID] = true
	}
	out := struct {
		Items []bulkCaseResult `json:"items"`
	}{Items: []bulkCaseResult{}}
	for _, item := range req.Items {
		if r.Context().Err() != nil {
			return
		}
		_, err := d.tm.UpdateTestCase(r.Context(), project, item.ID, testmgmt.TestCasePatch{Taxonomy: req.Patch.Taxonomy, Actor: actor, ExpectedUpdatedAt: item.ExpectedUpdatedAt, Priority: req.Patch.Priority, IsCritical: req.Patch.IsCritical, SuiteID: req.Patch.SuiteID, AutomationStatus: req.Patch.AutomationStatus})
		result := bulkCaseResult{ID: item.ID, Updated: err == nil, Code: ""}
		switch {
		case err == nil:
		case errors.Is(err, testmgmt.ErrConflict):
			result.Code = "CONFLICT"
		case errors.Is(err, testmgmt.ErrNotFound):
			result.Code = "NOT_FOUND"
		case errors.Is(err, testmgmt.ErrInvalid):
			result.Code = "INVALID_PAYLOAD"
		default:
			result.Code = "INTERNAL"
			httpx.Cause(r.Context(), err)
		}
		out.Items = append(out.Items, result)
	}
	writeJSON(w, http.StatusOK, out)
}

type caseBody struct {
	Taxonomy          *testmgmt.Taxonomy `json:"taxonomy"`
	Preconditions     *string            `json:"preconditions"`
	TestData          *string            `json:"testData"`
	ExpectedUpdatedAt *time.Time         `json:"expectedUpdatedAt"`
	SuiteID           *string            `json:"suiteId"`
	ExternalKey       string             `json:"externalKey"`
	Title             *string            `json:"title"`
	Steps             *[]testmgmt.Step   `json:"steps"`
	Expected          *string            `json:"expected"`
	Priority          *string            `json:"priority"`
	IsCritical        *bool              `json:"isCritical"`
	AutomationStatus  *string            `json:"automationStatus"`
	OwnerID           *string            `json:"ownerId"`
}

// A plan scope carries up to 5000 case ids (~200 KB) and an execution up to
// 200 step results of 4000 bytes each plus a note: both are past the 16 KiB
// of decodeSmall, so each route gets the bound its contract implies.
const (
	planBodyMax      = 256 << 10
	executionBodyMax = 2 << 20
)

// decodeBounded is decodeSmall with the route's own bound, and a body past
// it is a 413, not "malformed JSON".
func decodeBounded(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := decodeJSON(r, v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "request body too large", map[string]any{"limit": limit})
			return false
		}
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "malformed JSON", nil)
		return false
	}
	return true
}

func decodeCase(w http.ResponseWriter, r *http.Request) (caseBody, bool) {
	var req caseBody
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10) // steps can be long; still bounded
	if err := decodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "malformed JSON", nil)
		return caseBody{}, false
	}
	return req, true
}

func (d *dashboard) tmCreateCase(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeCase(w, r)
	if !ok {
		return
	}
	project, actor, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	in := testmgmt.NewTestCase{ExternalKey: req.ExternalKey, Actor: actor}
	if req.Taxonomy != nil {
		in.Taxonomy = *req.Taxonomy
	}
	if req.Preconditions != nil {
		in.Preconditions = *req.Preconditions
	}
	if req.TestData != nil {
		in.TestData = *req.TestData
	}
	if req.SuiteID != nil {
		in.SuiteID = *req.SuiteID
	}
	if req.Title != nil {
		in.Title = *req.Title
	}
	if req.Steps != nil {
		in.Steps = *req.Steps
	}
	if req.Expected != nil {
		in.Expected = *req.Expected
	}
	if req.Priority != nil {
		in.Priority = testmgmt.Priority(*req.Priority)
	}
	if req.IsCritical != nil {
		in.IsCritical = *req.IsCritical
	}
	if req.OwnerID != nil {
		in.OwnerID = *req.OwnerID
	}
	out, err := d.tm.CreateTestCase(r.Context(), project, in)
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, caseOut(out))
}

func (d *dashboard) tmCase(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	out, err := d.tm.GetTestCase(r.Context(), string(scope.ProjectID), r.PathValue("testCaseId"))
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, caseOut(out))
}

func (d *dashboard) tmPatchCase(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeCase(w, r)
	if !ok {
		return
	}
	project, actor, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	patch := testmgmt.TestCasePatch{
		Taxonomy:      req.Taxonomy,
		Preconditions: req.Preconditions, TestData: req.TestData, Actor: actor,
		SuiteID: req.SuiteID, Title: req.Title, Steps: req.Steps, Expected: req.Expected, Priority: (*testmgmt.Priority)(req.Priority),
		IsCritical: req.IsCritical, AutomationStatus: (*testmgmt.AutomationStatus)(req.AutomationStatus), OwnerID: req.OwnerID}
	if req.ExpectedUpdatedAt != nil {
		patch.ExpectedUpdatedAt = *req.ExpectedUpdatedAt
	}
	out, err := d.tm.UpdateTestCase(r.Context(), project, r.PathValue("testCaseId"), patch)
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, caseOut(out))
}

// tmImport takes the sheet as text/csv (or a JSON {csv} for clients that
// cannot post raw bodies), bounded at 8 MiB.
func (d *dashboard) tmImport(w http.ResponseWriter, r *http.Request) {
	project, actor, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	var text string
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var req struct {
			CSV string `json:"csv"`
		}
		if err := decodeJSON(r, &req); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "malformed JSON", nil)
			return
		}
		text = req.CSV
	} else {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "sheet over 8 MiB", nil)
			return
		}
		text = string(raw)
	}
	rep, err := d.tm.ImportCSVAs(r.Context(), project, text, actor)
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, importReportDTO{Created: rep.Created, Updated: rep.Updated, Skipped: rep.Skipped, Suites: rep.Suites, Requirements: rep.Requirements, Errors: rep.Errors})
}

func (d *dashboard) tmLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StableTestID string `json:"stableTestId"`
		LinkType     string `json:"linkType"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	project, actor, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	out, err := d.tm.LinkAutomation(r.Context(), project, r.PathValue("testCaseId"), req.StableTestID, testmgmt.LinkType(req.LinkType), actor)
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, linkOut(out))
}

func (d *dashboard) tmUnlink(w http.ResponseWriter, r *http.Request) {
	project, _, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	if err := d.tm.UnlinkAutomation(r.Context(), project, r.PathValue("testCaseId"), r.PathValue("linkId")); err != nil {
		d.tmErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *dashboard) tmCover(w http.ResponseWriter, r *http.Request) {
	project, _, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	if err := d.tm.Cover(r.Context(), project, r.PathValue("requirementId"), r.PathValue("testCaseId")); err != nil {
		d.tmErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *dashboard) tmUncover(w http.ResponseWriter, r *http.Request) {
	project, _, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	if err := d.tm.Uncover(r.Context(), project, r.PathValue("requirementId"), r.PathValue("testCaseId")); err != nil {
		d.tmErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
