package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/artifact"
	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
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
//	POST       /v1/projects/{p}/test-plans/{planId}/executions/{executionId}/evidence   multipart `file` → ArtifactRef (§6.8.2)
//	GET|POST   /v1/projects/{p}/releases                GET /releases/{releaseId}
//	POST       /v1/projects/{p}/releases/{releaseId}/evaluate   → release with gate
//	POST       /v1/projects/{p}/releases/{releaseId}/decision   {status, note}
//	GET        /v1/releases/{releaseId}/gate            bearer token with project:read — the CI answer (§6.8.4 #2)

func (d *dashboard) testmgmtRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	d.specializedRoutes(handle)
	handle("GET /v1/projects/{projectId}/suites", d.tmSuites)
	handle("POST /v1/projects/{projectId}/suites", d.tmCreateSuite)
	handle("GET /v1/projects/{projectId}/requirements", d.tmRequirements)
	handle("POST /v1/projects/{projectId}/requirements", d.tmCreateRequirement)
	handle("PATCH /v1/projects/{projectId}/requirements/{requirementId}", d.tmPatchRequirement)
	handle("GET /v1/projects/{projectId}/test-cases", d.tmCases)
	handle("POST /v1/projects/{projectId}/test-cases", d.tmCreateCase)
	handle("POST /v1/projects/{projectId}/test-cases/import", d.tmImport)
	handle("POST /v1/projects/{projectId}/test-cases/bulk", d.tmBulkCases)
	handle("GET /v1/projects/{projectId}/test-cases/export", d.tmExportCases)
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
	handle("POST /v1/projects/{projectId}/test-plans/{planId}/executions/{executionId}/evidence", d.tmEvidence)
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
	Taxonomy         testmgmt.Taxonomy     `json:"taxonomy"`
	Preconditions    string                `json:"preconditions"`
	TestData         string                `json:"testData"`
	Audit            []testmgmt.CaseChange `json:"audit,omitempty"`
	AuditTruncated   bool                  `json:"auditTruncated"`
	ID               string                `json:"id"`
	SuiteID          string                `json:"suiteId"`
	SuiteName        string                `json:"suiteName"`
	ExternalKey      string                `json:"externalKey"`
	Title            string                `json:"title"`
	Steps            []testmgmt.Step       `json:"steps"`
	Expected         string                `json:"expected"`
	Priority         string                `json:"priority"`
	IsCritical       bool                  `json:"isCritical"`
	AutomationStatus string                `json:"automationStatus"`
	OwnerID          string                `json:"ownerId"`
	Links            []linkDTO             `json:"links,omitempty"`
	Requirements     []string              `json:"requirements,omitempty"`
	CreatedAt        time.Time             `json:"createdAt"`
	UpdatedAt        time.Time             `json:"updatedAt"`
}

type executionDTO struct {
	ID           string                 `json:"id"`
	PlanID       string                 `json:"planId"`
	TestCaseID   string                 `json:"testCaseId"`
	CaseTitle    string                 `json:"caseTitle"`
	ExecutedBy   string                 `json:"executedBy"`
	Status       string                 `json:"status"`
	Note         string                 `json:"note"`
	ExecutedAt   time.Time              `json:"executedAt"`
	Evidence     []evidenceDTO          `json:"evidence"`
	CaseSnapshot *testmgmt.CaseSnapshot `json:"caseSnapshot"`
	StepResults  []testmgmt.StepResult  `json:"stepResults"`
	BugURL       string                 `json:"bugUrl"`
	RetestOf     string                 `json:"retestOf"`
}

// evidenceDTO is the ArtifactRef of the contract, as the failure detail
// lists it (query.ArtifactRef), for a manual_evidence artifact.
type evidenceDTO struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	ContentType string    `json:"contentType"`
	SizeBytes   int64     `json:"sizeBytes"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

func evidenceOut(a artifact.Artifact) evidenceDTO {
	return evidenceDTO{ID: a.ID, Name: a.Name, Kind: string(a.Kind), ContentType: a.ContentType, SizeBytes: a.ActualBytes, ExpiresAt: a.ExpiresAt.UTC()}
}

type planDTO struct {
	Cases            []planCaseDTO   `json:"cases"`
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	EnvironmentID    string          `json:"environmentId"`
	Environment      *environmentDTO `json:"environment"` // with the address testers open (QĐ-14); null without one
	TestType         string          `json:"testType"`    // smoke, regression, … (QĐ-15); "" when not chosen
	Status           string          `json:"status"`
	Executions       []executionDTO  `json:"executions,omitempty"`
	ExecutionHistory []executionDTO  `json:"executionHistory,omitempty"`
	HistoryTruncated bool            `json:"historyTruncated"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

type planCaseDTO struct {
	TestCase       testCaseDTO `json:"testCase"`
	AssigneeUserID string      `json:"assigneeUserId"`
	AssigneeEmail  string      `json:"assigneeEmail"`
	AssignedBy     string      `json:"assignedBy"`
	AssignedAt     *time.Time  `json:"assignedAt"`
	WorkStatus     string      `json:"workStatus"`
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
	out.Preconditions, out.TestData = c.Preconditions, c.TestData
	out.Taxonomy = c.Taxonomy
	out.Audit, out.AuditTruncated = c.Audit, c.AuditTruncated
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

func executionOut(e testmgmt.Execution, evidence []artifact.Artifact) executionDTO {
	out := executionDTO{ID: e.ID, PlanID: e.PlanID, TestCaseID: e.TestCaseID, CaseTitle: e.CaseTitle, ExecutedBy: e.ExecutedBy, Status: string(e.Status), Note: e.Note, ExecutedAt: e.ExecutedAt.UTC(),
		Evidence: make([]evidenceDTO, 0, len(evidence))}
	out.CaseSnapshot = e.CaseSnapshot
	out.StepResults = e.StepResults
	if out.StepResults == nil {
		out.StepResults = []testmgmt.StepResult{}
	}
	out.BugURL = e.BugURL
	out.RetestOf = e.RetestOf
	for _, a := range evidence {
		out.Evidence = append(out.Evidence, evidenceOut(a))
	}
	return out
}

// planOut renders a plan; evidence is the artifact module's answer for the
// plan's executions (cmd/api joins the two modules, testmgmt never imports
// artifact — §6.8.6, test/arch).
func planOut(p testmgmt.Plan, evidence map[string][]artifact.Artifact) planDTO {
	out := planDTO{ID: p.ID, Name: p.Name, EnvironmentID: p.EnvironmentID, TestType: p.TestType, Status: string(p.Status), CreatedAt: p.CreatedAt.UTC(), UpdatedAt: p.UpdatedAt.UTC()}
	if p.Executions != nil {
		out.Executions = make([]executionDTO, 0, len(p.Executions))
		for _, e := range p.Executions {
			out.Executions = append(out.Executions, executionOut(e, evidence[e.ID]))
		}
	}
	out.HistoryTruncated = p.HistoryTruncated
	if p.Cases != nil {
		out.Cases = make([]planCaseDTO, 0, len(p.Cases))
		for _, c := range p.Cases {
			out.Cases = append(out.Cases, planCaseDTO{TestCase: caseOut(c.TestCase), AssigneeUserID: c.AssigneeUserID, AssigneeEmail: c.AssigneeEmail, AssignedBy: c.AssignedBy, AssignedAt: c.AssignedAt, WorkStatus: c.WorkStatus})
		}
	}
	if p.ExecutionHistory != nil {
		out.ExecutionHistory = make([]executionDTO, 0, len(p.ExecutionHistory))
		for _, e := range p.ExecutionHistory {
			out.ExecutionHistory = append(out.ExecutionHistory, executionOut(e, evidence[e.ID]))
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
