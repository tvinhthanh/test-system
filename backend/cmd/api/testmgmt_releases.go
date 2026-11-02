package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/internal/testmgmt"
)

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
	writeJSON(w, http.StatusOK, ciReleaseDTO{releaseDTO: releaseOut(rel), CI: ciVerdict(rel)})
}

// ciDTO is what a deploy job acts on: go on or stop, and why. The rule lives
// here rather than in each pipeline's jq (21-10 review, finding 4, which
// found the documented jq let a release through when a person had blocked
// it, when nothing was evaluated, and on no evidence). A person's decision
// wins over the gate — approved or shipped goes, blocked stops — and an
// open release goes only on an evaluated gate that is approved or warn.
type ciDTO struct {
	Pass   bool   `json:"pass"`
	Reason string `json:"reason"`
}

type ciReleaseDTO struct {
	releaseDTO
	CI ciDTO `json:"ci"`
}

func ciVerdict(r testmgmt.Release) ciDTO {
	switch r.Status {
	case testmgmt.ReleaseApproved, testmgmt.ReleaseShipped:
		return ciDTO{Pass: true, Reason: "release " + string(r.Status) + " by " + r.DecidedBy}
	case testmgmt.ReleaseBlocked:
		return ciDTO{Reason: "release blocked by " + r.DecidedBy}
	}
	switch {
	case r.Gate == nil:
		return ciDTO{Reason: "gate not evaluated; evaluate it or have the release approved"}
	case r.Gate.Verdict == testmgmt.VerdictApproved || r.Gate.Verdict == testmgmt.VerdictWarn:
		return ciDTO{Pass: true, Reason: "gate " + string(r.Gate.Verdict)}
	default:
		return ciDTO{Reason: "gate " + string(r.Gate.Verdict)}
	}
}

func decodeJSON(r *http.Request, v any) error { return json.NewDecoder(r.Body).Decode(v) }
