package main

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/artifact"
	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/internal/testmgmt"
)

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
	envs, err := d.projectEnvironments(r, string(scope.ProjectID))
	if err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listOut(items, func(p testmgmt.Plan) planDTO { return withEnvironment(planOut(p, nil), envs) }))
}

// withEnvironment fills a plan's environment from the project's.
func withEnvironment(p planDTO, envs map[string]environmentDTO) planDTO {
	if e, ok := envs[p.EnvironmentID]; ok {
		p.Environment = &e
	}
	return p
}

// planEnvironment refuses an environment id that is not the project's, so
// a plan never points at another tenant's environment. "" (none) passes.
func (d *dashboard) planEnvironment(w http.ResponseWriter, r *http.Request, project, id string) (map[string]environmentDTO, bool) {
	envs, err := d.projectEnvironments(r, project)
	if err != nil {
		d.internal(w, r, err)
		return nil, false
	}
	if _, ok := envs[id]; id != "" && !ok {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "environmentId is not an environment of this project", map[string]any{"field": "environmentId"})
		return nil, false
	}
	return envs, true
}

func (d *dashboard) tmCreatePlan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CaseIDs       *[]string `json:"caseIds"`
		Name          string    `json:"name"`
		EnvironmentID string    `json:"environmentId"`
		TestType      string    `json:"testType"`
	}
	if !decodeBounded(w, r, &req, planBodyMax) {
		return
	}
	project, actor, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	envs, ok := d.planEnvironment(w, r, project, req.EnvironmentID)
	if !ok {
		return
	}
	out, err := d.tm.CreatePlan(r.Context(), project, testmgmt.NewPlan{Name: req.Name, EnvironmentID: req.EnvironmentID, TestType: req.TestType, CaseIDs: req.CaseIDs, Actor: actor})
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, withEnvironment(planOut(out, nil), envs))
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
	d.writePlan(w, r, out)
}

func (d *dashboard) writePlan(w http.ResponseWriter, r *http.Request, out testmgmt.Plan) {
	ids := make([]string, 0, len(out.Executions))
	for _, e := range out.Executions {
		ids = append(ids, e.ID)
	}
	for _, e := range out.ExecutionHistory {
		ids = append(ids, e.ID)
	}
	evidence, err := d.artifact.ListEvidence(r.Context(), out.ProjectID, ids)
	if err != nil {
		d.internal(w, r, err)
		return
	}
	envs, err := d.projectEnvironments(r, out.ProjectID)
	if err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, withEnvironment(planOut(out, evidence), envs))
}

func (d *dashboard) tmPatchPlan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status            *string    `json:"status"`
		CaseIDs           *[]string  `json:"caseIds"`
		EnvironmentID     *string    `json:"environmentId"`
		ExpectedUpdatedAt *time.Time `json:"expectedUpdatedAt"`
		CaseUpdate        *struct {
			TestCaseID    string  `json:"testCaseId"`
			AssigneeEmail *string `json:"assigneeEmail"`
			WorkStatus    *string `json:"workStatus"`
		} `json:"caseUpdate"`
	}
	if !decodeBounded(w, r, &req, planBodyMax) {
		return
	}
	project, actor, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	patch := testmgmt.PlanPatch{Status: (*testmgmt.PlanStatus)(req.Status), CaseIDs: req.CaseIDs, EnvironmentID: req.EnvironmentID}
	if req.EnvironmentID != nil {
		if _, ok := d.planEnvironment(w, r, project, *req.EnvironmentID); !ok {
			return
		}
	}
	if req.ExpectedUpdatedAt != nil {
		patch.ExpectedUpdatedAt = *req.ExpectedUpdatedAt
	}
	if update := req.CaseUpdate; update != nil {
		patch.CaseUpdate = &testmgmt.PlanCaseUpdate{TestCaseID: update.TestCaseID, WorkStatus: update.WorkStatus}
		if update.AssigneeEmail != nil {
			email := strings.ToLower(strings.TrimSpace(*update.AssigneeEmail))
			userID := ""
			if email != "" {
				u, err := d.authz.GetUserByEmail(r.Context(), email)
				if err != nil && !errors.Is(err, authz.ErrNotFound) {
					d.internal(w, r, err)
					return
				}
				if errors.Is(err, authz.ErrNotFound) || u.EmailVerifiedAt.IsZero() {
					d.tmErr(w, r, testmgmt.ErrNotFound)
					return
				}
				scope, err := d.tenancy.ProjectScope(r.Context(), tenancy.ProjectID(project))
				if err != nil {
					d.internal(w, r, err)
					return
				}
				if err := d.authz.Authorize(r.Context(), authz.Principal{Kind: authz.PrincipalUser, UserID: u.ID}, resourceOf(scope), authz.ActionTestmgmtWrite); err != nil {
					if errors.Is(err, authz.ErrForbidden) || errors.Is(err, authz.ErrNoAccess) {
						d.tmErr(w, r, testmgmt.ErrNotFound)
					} else {
						d.internal(w, r, err)
					}
					return
				}
				userID = u.ID
			}
			patch.CaseUpdate.AssigneeUserID = &userID
			patch.CaseUpdate.AssigneeEmail = &email
		}
	}
	out, err := d.tm.UpdatePlan(r.Context(), project, r.PathValue("planId"), patch, actor)
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	d.writePlan(w, r, out)
}

func (d *dashboard) tmExecute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CaseUpdatedAt *time.Time            `json:"caseUpdatedAt"`
		TestCaseID    string                `json:"testCaseId"`
		Status        string                `json:"status"`
		Note          string                `json:"note"`
		ExecutedAt    *time.Time            `json:"executedAt"`
		StepResults   []testmgmt.StepResult `json:"stepResults"`
		BugURL        string                `json:"bugUrl"`
		RetestOf      string                `json:"retestOf"`
	}
	if !decodeBounded(w, r, &req, executionBodyMax) {
		return
	}
	project, actor, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	in := testmgmt.NewExecution{TestCaseID: req.TestCaseID, Status: testmgmt.ExecutionStatus(req.Status), Note: req.Note, StepResults: req.StepResults, BugURL: req.BugURL, RetestOf: req.RetestOf}
	if req.CaseUpdatedAt != nil {
		in.CaseUpdatedAt = *req.CaseUpdatedAt
	}
	if req.ExecutedAt != nil {
		in.ExecutedAt = *req.ExecutedAt
	}
	out, err := d.tm.RecordExecution(r.Context(), project, r.PathValue("planId"), in, actor)
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, executionOut(out, nil))
}

// evidenceMaxBytes bounds the whole multipart body: the kind's cap plus
// room for the part headers. Anything past it is a 413 before a byte is
// parsed, so a client cannot make the api buffer more than §10.4 allows.
var evidenceMaxBytes = artifact.Caps[artifact.KindManualEvidence] + 64<<10

// tmEvidence attaches one file to one manual execution (§6.8.2 "Artifact
// dùng lại"): `multipart/form-data` with a single `file` part. Ownership and
// the closed-plan rule are testmgmt's (ExecutionForEvidence); the bytes,
// the cap, the content-type policy and the sniff are artifact's
// (PutEvidence). The tenant check comes before the body is read.
func (d *dashboard) tmEvidence(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	project := string(scope.ProjectID)
	exec, err := d.tm.ExecutionForEvidence(r.Context(), project, r.PathValue("planId"), r.PathValue("executionId"))
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "multipart/form-data" {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "send multipart/form-data with one `file` part", map[string]any{"field": "file"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, evidenceMaxBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "malformed multipart body", nil)
		return
	}
	var (
		name, contentType string
		body              []byte
	)
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				httpx.WriteError(w, http.StatusRequestEntityTooLarge, "ARTIFACT_TOO_LARGE", "file exceeds the manual_evidence cap", map[string]any{"kind": artifact.KindManualEvidence, "limit": artifact.Caps[artifact.KindManualEvidence]})
				return
			}
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "malformed multipart body", nil)
			return
		}
		if part.FormName() != "file" {
			_, _ = io.Copy(io.Discard, part)
			continue
		}
		name, contentType = part.FileName(), part.Header.Get("Content-Type")
		body, err = io.ReadAll(part)
		part.Close()
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				httpx.WriteError(w, http.StatusRequestEntityTooLarge, "ARTIFACT_TOO_LARGE", "file exceeds the manual_evidence cap", map[string]any{"kind": artifact.KindManualEvidence, "limit": artifact.Caps[artifact.KindManualEvidence]})
				return
			}
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "malformed multipart body", nil)
			return
		}
		break
	}
	if body == nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "a `file` part is required", map[string]any{"field": "file"})
		return
	}
	// The browser's File.type; parameters (charset) are not part of the policy.
	contentType, _, _ = mime.ParseMediaType(contentType)
	// Retention follows the organization's plan, as on declare (§10.4).
	plan := "free"
	if org, err := d.tenancy.GetOrganization(r.Context(), scope.OrgID); err == nil {
		plan = org.PlanID
	}
	a, err := d.artifact.PutEvidence(r.Context(), artifact.Evidence{ProjectID: project, ExecutionID: exec.ID, Plan: plan, Name: name, ContentType: contentType}, body)
	if err != nil {
		var tooLarge *artifact.TooLargeError
		switch {
		case errors.As(err, &tooLarge):
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "ARTIFACT_TOO_LARGE", err.Error(), map[string]any{"kind": tooLarge.Kind, "limit": tooLarge.Limit, "sizeBytes": tooLarge.Got})
		case errors.Is(err, artifact.ErrInvalid):
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", strings.TrimPrefix(err.Error(), "artifact: invalid input: "), map[string]any{"field": "file"})
		case errors.Is(err, artifact.ErrNotFound):
			httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "not found", nil)
		case errors.Is(err, artifact.ErrStore):
			httpx.Cause(r.Context(), err)
			httpx.WriteError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "object store unavailable; retry", nil)
		default:
			d.internal(w, r, err)
		}
		return
	}
	writeJSON(w, http.StatusCreated, evidenceOut(a))
}
