package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/internal/testmgmt"
)

// The Test Management Plane over HTTP (§6.8, P2, QĐ-12). Reads take view;
// writes take testmgmt.write (member); deciding a release takes
// release.decide (admin). Every id in a path is resolved inside the project
// of the URL — a foreign id is a 404, never a leak (§11.3.3).
//
//	GET|POST   /v1/projects/{p}/suites
//	GET|POST   /v1/projects/{p}/requirements            PATCH /requirements/{requirementId}
//	GET|POST   /v1/projects/{p}/test-cases              GET|PATCH /test-cases/{testCaseId}
//	POST       /v1/projects/{p}/test-cases/import       text/csv body → ImportReport
//	POST       /v1/projects/{p}/test-cases/{testCaseId}/links {stableTestId, linkType}
//	DELETE     /v1/projects/{p}/test-cases/{testCaseId}/links/{linkId}
//	POST|DELETE /v1/projects/{p}/test-cases/{testCaseId}/requirements/{requirementId}
//	GET|POST   /v1/projects/{p}/test-plans              GET|PATCH /test-plans/{planId}
//	POST       /v1/projects/{p}/test-plans/{planId}/executions {testCaseId, status, note}
//	GET|POST   /v1/projects/{p}/releases                GET /releases/{releaseId}
//	POST       /v1/projects/{p}/releases/{releaseId}/evaluate   → release with gate
//	POST       /v1/projects/{p}/releases/{releaseId}/decision   {status, note}
//	GET        /v1/releases/{releaseId}/gate            bearer token with project:read — the CI answer (§6.8.4 #2)

func (d *dashboard) testmgmtRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("GET /v1/projects/{projectId}/suites", d.tmSuites)
	handle("POST /v1/projects/{projectId}/suites", d.tmCreateSuite)
	handle("GET /v1/projects/{projectId}/requirements", d.tmRequirements)
	handle("POST /v1/projects/{projectId}/requirements", d.tmCreateRequirement)
	handle("PATCH /v1/projects/{projectId}/requirements/{requirementId}", d.tmPatchRequirement)
	handle("GET /v1/projects/{projectId}/test-cases", d.tmCases)
	handle("POST /v1/projects/{projectId}/test-cases", d.tmCreateCase)
	handle("POST /v1/projects/{projectId}/test-cases/import", d.tmImport)
	handle("GET /v1/projects/{projectId}/test-cases/{testCaseId}", d.tmCase)
	handle("PATCH /v1/projects/{projectId}/test-cases/{testCaseId}", d.tmPatchCase)
	handle("POST /v1/projects/{projectId}/test-cases/{testCaseId}/links", d.tmLink)
	handle("DELETE /v1/projects/{projectId}/test-cases/{testCaseId}/links/{linkId}", d.tmUnlink)
	handle("POST /v1/projects/{projectId}/test-cases/{testCaseId}/requirements/{requirementId}", d.tmCover)
	handle("DELETE /v1/projects/{projectId}/test-cases/{testCaseId}/requirements/{requirementId}", d.tmUncover)
	handle("GET /v1/projects/{projectId}/test-plans", d.tmPlans)
	handle("POST /v1/projects/{projectId}/test-plans", d.tmCreatePlan)
	handle("GET /v1/projects/{projectId}/test-plans/{planId}", d.tmPlan)
	handle("PATCH /v1/projects/{projectId}/test-plans/{planId}", d.tmPatchPlan)
	handle("POST /v1/projects/{projectId}/test-plans/{planId}/executions", d.tmExecute)
	handle("GET /v1/projects/{projectId}/releases", d.tmReleases)
	handle("POST /v1/projects/{projectId}/releases", d.tmCreateRelease)
	handle("GET /v1/projects/{projectId}/releases/{releaseId}", d.tmRelease)
	handle("POST /v1/projects/{projectId}/releases/{releaseId}/evaluate", d.tmEvaluate)
	handle("POST /v1/projects/{projectId}/releases/{releaseId}/decision", d.tmDecide)
}

// ---- DTOs -------------------------------------------------------------------

type suiteDTO struct {
	ID        string    `json:"id"`
	ParentID  string    `json:"parentId"`
	Name      string    `json:"name"`
	Cases     int       `json:"cases"`
	CreatedAt time.Time `json:"createdAt"`
}

type requirementDTO struct {
	ID          string    `json:"id"`
	ExternalKey string    `json:"externalKey"`
	Title       string    `json:"title"`
	Priority    string    `json:"priority"`
	Status      string    `json:"status"`
	CoveredBy   int       `json:"coveredBy"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type linkDTO struct {
	ID           string    `json:"id"`
	TestCaseID   string    `json:"testCaseId"`
	StableTestID string    `json:"stableTestId"`
	LinkType     string    `json:"linkType"`
	CreatedBy    string    `json:"createdBy"`
	CreatedAt    time.Time `json:"createdAt"`
}

type testCaseDTO struct {
	ID               string          `json:"id"`
	SuiteID          string          `json:"suiteId"`
	SuiteName        string          `json:"suiteName"`
	ExternalKey      string          `json:"externalKey"`
	Title            string          `json:"title"`
	Steps            []testmgmt.Step `json:"steps"`
	Expected         string          `json:"expected"`
	Priority         string          `json:"priority"`
	IsCritical       bool            `json:"isCritical"`
	AutomationStatus string          `json:"automationStatus"`
	OwnerID          string          `json:"ownerId"`
	Links            []linkDTO       `json:"links,omitempty"`
	Requirements     []string        `json:"requirements,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

type executionDTO struct {
	ID         string    `json:"id"`
	PlanID     string    `json:"planId"`
	TestCaseID string    `json:"testCaseId"`
	CaseTitle  string    `json:"caseTitle"`
	ExecutedBy string    `json:"executedBy"`
	Status     string    `json:"status"`
	Note       string    `json:"note"`
	ExecutedAt time.Time `json:"executedAt"`
}

type planDTO struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	EnvironmentID string         `json:"environmentId"`
	Status        string         `json:"status"`
	Executions    []executionDTO `json:"executions,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type releaseDTO struct {
	ID           string               `json:"id"`
	Version      string               `json:"version"`
	Branch       string               `json:"branch"`
	Status       string               `json:"status"`
	DecidedBy    string               `json:"decidedBy"`
	DecidedAt    *time.Time           `json:"decidedAt"`
	DecisionNote string               `json:"decisionNote"`
	Gate         *testmgmt.GateResult `json:"gate"`
	EvaluatedAt  *time.Time           `json:"evaluatedAt"`
	CreatedAt    time.Time            `json:"createdAt"`
	UpdatedAt    time.Time            `json:"updatedAt"`
}

type importReportDTO struct {
	Created      int      `json:"created"`
	Updated      int      `json:"updated"`
	Skipped      int      `json:"skipped"`
	Suites       int      `json:"suites"`
	Requirements int      `json:"requirements"`
	Errors       []string `json:"errors"`
}

func suiteOut(s testmgmt.Suite) suiteDTO {
	return suiteDTO{ID: s.ID, ParentID: s.ParentID, Name: s.Name, Cases: s.Cases, CreatedAt: s.CreatedAt.UTC()}
}

func requirementOut(r testmgmt.Requirement) requirementDTO {
	return requirementDTO{ID: r.ID, ExternalKey: r.ExternalKey, Title: r.Title, Priority: string(r.Priority), Status: string(r.Status),
		CoveredBy: r.CoveredBy, CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC()}
}

func linkOut(l testmgmt.Link) linkDTO {
	return linkDTO{ID: l.ID, TestCaseID: l.TestCaseID, StableTestID: l.StableTestID, LinkType: string(l.LinkType), CreatedBy: l.CreatedBy, CreatedAt: l.CreatedAt.UTC()}
}

func caseOut(c testmgmt.TestCase) testCaseDTO {
	out := testCaseDTO{ID: c.ID, SuiteID: c.SuiteID, SuiteName: c.SuiteName, ExternalKey: c.ExternalKey, Title: c.Title, Steps: c.Steps,
		Expected: c.Expected, Priority: string(c.Priority), IsCritical: c.IsCritical, AutomationStatus: string(c.AutomationStatus),
		OwnerID: c.OwnerID, Requirements: c.Requirements, CreatedAt: c.CreatedAt.UTC(), UpdatedAt: c.UpdatedAt.UTC()}
	if out.Steps == nil {
		out.Steps = []testmgmt.Step{}
	}
	if c.Links != nil {
		out.Links = make([]linkDTO, 0, len(c.Links))
		for _, l := range c.Links {
			out.Links = append(out.Links, linkOut(l))
		}
	}
	return out
}

func executionOut(e testmgmt.Execution) executionDTO {
	return executionDTO{ID: e.ID, PlanID: e.PlanID, TestCaseID: e.TestCaseID, CaseTitle: e.CaseTitle, ExecutedBy: e.ExecutedBy, Status: string(e.Status), Note: e.Note, ExecutedAt: e.ExecutedAt.UTC()}
}

func planOut(p testmgmt.Plan) planDTO {
	out := planDTO{ID: p.ID, Name: p.Name, EnvironmentID: p.EnvironmentID, Status: string(p.Status), CreatedAt: p.CreatedAt.UTC(), UpdatedAt: p.UpdatedAt.UTC()}
	if p.Executions != nil {
		out.Executions = make([]executionDTO, 0, len(p.Executions))
		for _, e := range p.Executions {
			out.Executions = append(out.Executions, executionOut(e))
		}
	}
	return out
}

func releaseOut(r testmgmt.Release) releaseDTO {
	out := releaseDTO{ID: r.ID, Version: r.Version, Branch: r.Branch, Status: string(r.Status), DecidedBy: r.DecidedBy, DecisionNote: r.DecisionNote,
		Gate: r.Gate, CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC()}
	if !r.DecidedAt.IsZero() {
		t := r.DecidedAt.UTC()
		out.DecidedAt = &t
	}
	if !r.EvaluatedAt.IsZero() {
		t := r.EvaluatedAt.UTC()
		out.EvaluatedAt = &t
	}
	return out
}

// ---- prologues ---------------------------------------------------------------

// writer is the prologue of every testmgmt write: a member of the project,
// plus the actor name the audit columns record.
func (d *dashboard) writer(w http.ResponseWriter, r *http.Request, action authz.Action) (projectID, actor string, ok bool) {
	p, ok := d.user(w, r)
	if !ok {
		return "", "", false
	}
	scope, ok := d.project(w, r, p, action)
	if !ok {
		return "", "", false
	}
	return string(scope.ProjectID), d.actor(r, p), true
}

func (d *dashboard) tmErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, testmgmt.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "not found", nil)
	case errors.Is(err, testmgmt.ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error(), nil)
	case errors.Is(err, testmgmt.ErrConflict):
		httpx.WriteError(w, http.StatusConflict, "CONFLICT", err.Error(), nil)
	default:
		d.internal(w, r, err)
	}
}

func listOut[T any, D any](items []T, f func(T) D) page {
	out := make([]D, 0, len(items))
	for _, it := range items {
		out = append(out, f(it))
	}
	return page{Items: out}
}

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
	q := r.URL.Query()
	f := testmgmt.CaseFilter{SuiteID: q.Get("suite"), AutomationStatus: testmgmt.AutomationStatus(q.Get("automation")), Query: q.Get("q"), Limit: limitParam(r, 200)}
	switch q.Get("critical") {
	case "true":
		v := true
		f.Critical = &v
	case "false":
		v := false
		f.Critical = &v
	}
	items, err := d.tm.ListTestCases(r.Context(), string(scope.ProjectID), f)
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listOut(items, caseOut))
}

type caseBody struct {
	SuiteID          *string          `json:"suiteId"`
	ExternalKey      string           `json:"externalKey"`
	Title            *string          `json:"title"`
	Steps            *[]testmgmt.Step `json:"steps"`
	Expected         *string          `json:"expected"`
	Priority         *string          `json:"priority"`
	IsCritical       *bool            `json:"isCritical"`
	AutomationStatus *string          `json:"automationStatus"`
	OwnerID          *string          `json:"ownerId"`
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
	project, _, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	in := testmgmt.NewTestCase{ExternalKey: req.ExternalKey}
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
	project, _, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	out, err := d.tm.UpdateTestCase(r.Context(), project, r.PathValue("testCaseId"), testmgmt.TestCasePatch{
		SuiteID: req.SuiteID, Title: req.Title, Steps: req.Steps, Expected: req.Expected, Priority: (*testmgmt.Priority)(req.Priority),
		IsCritical: req.IsCritical, AutomationStatus: (*testmgmt.AutomationStatus)(req.AutomationStatus), OwnerID: req.OwnerID})
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, caseOut(out))
}

// tmImport takes the sheet as text/csv (or a JSON {csv} for clients that
// cannot post raw bodies), bounded at 8 MiB.
func (d *dashboard) tmImport(w http.ResponseWriter, r *http.Request) {
	project, _, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
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
	rep, err := d.tm.ImportCSV(r.Context(), project, text)
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

// ---- plans --------------------------------------------------------------------

func (d *dashboard) tmPlans(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	items, err := d.tm.ListPlans(r.Context(), string(scope.ProjectID))
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listOut(items, planOut))
}

func (d *dashboard) tmCreatePlan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string `json:"name"`
		EnvironmentID string `json:"environmentId"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	project, _, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	out, err := d.tm.CreatePlan(r.Context(), project, testmgmt.NewPlan{Name: req.Name, EnvironmentID: req.EnvironmentID})
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, planOut(out))
}

func (d *dashboard) tmPlan(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	out, err := d.tm.GetPlan(r.Context(), string(scope.ProjectID), r.PathValue("planId"))
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, planOut(out))
}

func (d *dashboard) tmPatchPlan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	project, _, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	out, err := d.tm.SetPlanStatus(r.Context(), project, r.PathValue("planId"), testmgmt.PlanStatus(req.Status))
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, planOut(out))
}

func (d *dashboard) tmExecute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TestCaseID string     `json:"testCaseId"`
		Status     string     `json:"status"`
		Note       string     `json:"note"`
		ExecutedAt *time.Time `json:"executedAt"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	project, actor, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	in := testmgmt.NewExecution{TestCaseID: req.TestCaseID, Status: testmgmt.ExecutionStatus(req.Status), Note: req.Note}
	if req.ExecutedAt != nil {
		in.ExecutedAt = *req.ExecutedAt
	}
	out, err := d.tm.RecordExecution(r.Context(), project, r.PathValue("planId"), in, actor)
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, executionOut(out))
}

// ---- releases ------------------------------------------------------------------

func (d *dashboard) tmReleases(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	items, err := d.tm.ListReleases(r.Context(), string(scope.ProjectID))
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listOut(items, releaseOut))
}

func (d *dashboard) tmCreateRelease(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version string `json:"version"`
		Branch  string `json:"branch"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	project, _, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	out, err := d.tm.CreateRelease(r.Context(), project, testmgmt.NewRelease{Version: req.Version, Branch: req.Branch})
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, releaseOut(out))
}

func (d *dashboard) tmRelease(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	out, err := d.tm.GetRelease(r.Context(), string(scope.ProjectID), r.PathValue("releaseId"))
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, releaseOut(out))
}

// tmEvaluate runs the gate. Members may ask for a fresh verdict — it is a
// read of the Intelligence Plane written onto the release, not a decision.
func (d *dashboard) tmEvaluate(w http.ResponseWriter, r *http.Request) {
	project, _, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	out, err := d.tm.Evaluate(r.Context(), project, r.PathValue("releaseId"))
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, releaseOut(out))
}

func (d *dashboard) tmDecide(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	project, actor, ok := d.writer(w, r, authz.ActionReleaseDecide)
	if !ok {
		return
	}
	out, err := d.tm.Decide(r.Context(), project, r.PathValue("releaseId"), testmgmt.Decision{Status: testmgmt.ReleaseStatus(req.Status), Note: req.Note}, actor)
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, releaseOut(out))
}

// ---- CI: the gate by bearer token ------------------------------------------------

// gateForCI answers GET /v1/releases/{releaseId}/gate for a token with
// project:read on the release's project (§6.8.4 #2: "API máy đọc được, để
// CI của khách gọi và tự chặn deploy"). The release id alone says nothing
// about the tenant, so the release is looked up by id and the token is then
// authorized against *that* project; a token of another tenant sees 404.
// Mounted behind authz.Middleware (bearer), outside the session-only
// dashboard routes.
func (d *dashboard) gateForCI(w http.ResponseWriter, r *http.Request) {
	p, ok := authz.PrincipalFrom(r.Context())
	if !ok || p.Kind != authz.PrincipalToken {
		httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "bearer token required", nil)
		return
	}
	projectID, err := d.tm.ProjectOfRelease(r.Context(), r.PathValue("releaseId"))
	if err != nil {
		if errors.Is(err, testmgmt.ErrNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "not found", nil)
			return
		}
		d.internal(w, r, err)
		return
	}
	scope, err := d.tenancy.ProjectScope(r.Context(), tenancy.ProjectID(projectID))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "not found", nil)
		return
	}
	res := authz.Resource{OrgID: string(scope.OrgID), WorkspaceID: string(scope.WorkspaceID), ProjectID: string(scope.ProjectID)}
	if err := d.authz.Authorize(r.Context(), p, res, authz.ActionView); err != nil {
		// No standing or no read permission: the release does not exist for you.
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "not found", nil)
		return
	}
	rel, err := d.tm.GetRelease(r.Context(), projectID, r.PathValue("releaseId"))
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, releaseOut(rel))
}

func decodeJSON(r *http.Request, v any) error { return json.NewDecoder(r.Body).Decode(v) }
