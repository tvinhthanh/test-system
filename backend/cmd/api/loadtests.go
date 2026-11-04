package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/autotest"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
)

// Load tests (QĐ-19 tier 3), testmgmt.write like Auto test:
//
//	GET  /v1/projects/{projectId}/environments/{environmentId}/ownership         the proof to publish, and its state
//	POST /v1/projects/{projectId}/environments/{environmentId}/ownership/check   {method: file|dns}   → 202
//	POST /v1/projects/{projectId}/load-runs {environmentId, paths?, vus, seconds, rampSeconds?, maxP95Ms?, maxErrorRate?}   → 202
//	GET  /v1/projects/{projectId}/load-runs
//	GET  /v1/projects/{projectId}/load-runs/{loadRunId}
func (d *dashboard) loadRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("GET /v1/projects/{projectId}/environments/{environmentId}/ownership", d.ownership)
	handle("POST /v1/projects/{projectId}/environments/{environmentId}/ownership/check", d.checkOwnership)
	handle("POST /v1/projects/{projectId}/load-runs", d.createLoadRun)
	handle("GET /v1/projects/{projectId}/load-runs", d.loadRuns)
	handle("GET /v1/projects/{projectId}/load-runs/{loadRunId}", d.loadRun)
}

type ownershipDTO struct {
	Host       string     `json:"host"`
	Token      string     `json:"token"`
	Method     string     `json:"method"`
	Status     string     `json:"status"`
	Error      string     `json:"error"`
	VerifiedAt *time.Time `json:"verifiedAt"`
	CheckedAt  *time.Time `json:"checkedAt"`
	// How to prove it: either one.
	FileURL  string `json:"fileUrl"`
	FileText string `json:"fileText"`
	DNSName  string `json:"dnsName"`
	DNSText  string `json:"dnsText"`
}

func ownershipOut(o autotest.Ownership, scheme string) ownershipDTO {
	return ownershipDTO{Host: o.Host, Token: o.Token, Method: o.Method, Status: o.Status, Error: o.Error,
		VerifiedAt: timePtr(o.VerifiedAt), CheckedAt: timePtr(o.CheckedAt),
		FileURL: scheme + "://" + o.Host + autotest.WellKnownPath, FileText: o.FileText(), DNSName: o.DNSName(), DNSText: o.FileText()}
}

func (d *dashboard) ownership(w http.ResponseWriter, r *http.Request) {
	p, scope, ok := d.tester(w, r)
	if !ok {
		return
	}
	env, ok := d.pathEnvironment(w, r, string(scope.ProjectID))
	if !ok {
		return
	}
	o, err := d.auto.Ownership(r.Context(), string(scope.ProjectID), string(scope.OrgID), env.ID, d.actor(r, p))
	if err != nil {
		d.loadErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ownershipOut(o, schemeOf(env.BaseURL)))
}

func (d *dashboard) checkOwnership(w http.ResponseWriter, r *http.Request) {
	p, scope, ok := d.tester(w, r)
	if !ok {
		return
	}
	env, ok := d.pathEnvironment(w, r, string(scope.ProjectID))
	if !ok || !d.checkerOn(w) {
		return
	}
	var req struct {
		Method string `json:"method"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	o, err := d.auto.CheckOwnership(r.Context(), string(scope.ProjectID), string(scope.OrgID), env.ID, req.Method, d.actor(r, p))
	if err != nil {
		d.loadErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, ownershipOut(o, schemeOf(env.BaseURL)))
}

type loadRunDTO struct {
	ID            string          `json:"id"`
	EnvironmentID string          `json:"environmentId"`
	Host          string          `json:"host"`
	Paths         []string        `json:"paths"`
	VUs           int             `json:"vus"`
	Seconds       int             `json:"seconds"`
	RampSeconds   int             `json:"rampSeconds"`
	MaxP95Ms      *int            `json:"maxP95Ms"`
	MaxErrorRate  *float64        `json:"maxErrorRate"`
	Status        string          `json:"status"`
	Verdict       string          `json:"verdict"`
	Reasons       []string        `json:"reasons"`
	Result        json.RawMessage `json:"result"`
	Error         string          `json:"error"`
	RequestedBy   string          `json:"requestedBy"`
	CreatedAt     time.Time       `json:"createdAt"`
	StartedAt     *time.Time      `json:"startedAt"`
	FinishedAt    *time.Time      `json:"finishedAt"`
}

func loadRunOut(l autotest.LoadRun) loadRunDTO {
	out := loadRunDTO{ID: l.ID, EnvironmentID: l.EnvironmentID, Host: l.Host, Paths: l.Paths, VUs: l.VUs, Seconds: l.Seconds, RampSeconds: l.RampSeconds,
		Status: l.Status, Verdict: l.Verdict, Reasons: l.Reasons, Result: l.Result, Error: l.Error, RequestedBy: l.RequestedBy,
		CreatedAt: l.CreatedAt, StartedAt: timePtr(l.StartedAt), FinishedAt: timePtr(l.FinishedAt)}
	if l.MaxP95Ms > 0 {
		v := l.MaxP95Ms
		out.MaxP95Ms = &v
	}
	if l.MaxErrorRate >= 0 {
		v := l.MaxErrorRate
		out.MaxErrorRate = &v
	}
	if out.Result == nil {
		out.Result = json.RawMessage("null")
	}
	return out
}

func (d *dashboard) createLoadRun(w http.ResponseWriter, r *http.Request) {
	p, scope, ok := d.tester(w, r)
	if !ok || !d.checkerOn(w) {
		return
	}
	var req struct {
		EnvironmentID string   `json:"environmentId"`
		Paths         []string `json:"paths"`
		VUs           int      `json:"vus"`
		Seconds       int      `json:"seconds"`
		RampSeconds   int      `json:"rampSeconds"`
		MaxP95Ms      int      `json:"maxP95Ms"`
		MaxErrorRate  *float64 `json:"maxErrorRate"`
	}
	if !decodeBounded(w, r, &req, 64<<10) {
		return
	}
	if req.EnvironmentID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "environmentId: the site to load", map[string]any{"field": "environmentId"})
		return
	}
	if _, ok := d.planEnvironment(w, r, string(scope.ProjectID), req.EnvironmentID); !ok {
		return
	}
	rate := -1.0
	if req.MaxErrorRate != nil {
		if *req.MaxErrorRate < 0 {
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "maxErrorRate: a percent, 0 to 100", map[string]any{"field": "maxErrorRate"})
			return
		}
		rate = *req.MaxErrorRate
	}
	l, err := d.auto.CreateLoadRun(r.Context(), autotest.NewLoadRun{ProjectID: string(scope.ProjectID), OrgID: string(scope.OrgID), EnvironmentID: req.EnvironmentID,
		Paths: req.Paths, VUs: req.VUs, Seconds: req.Seconds, RampSeconds: req.RampSeconds, MaxP95Ms: req.MaxP95Ms, MaxErrorRate: rate, Actor: d.actor(r, p)})
	if err != nil {
		d.loadErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, loadRunOut(l))
}

func (d *dashboard) loadRuns(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	runs, err := d.auto.LoadRuns(r.Context(), string(scope.ProjectID), 50)
	if err != nil {
		d.loadErr(w, r, err)
		return
	}
	out := listOut(runs, loadRunOut)
	writeJSON(w, http.StatusOK, map[string]any{"items": out.Items, "nextCursor": nil, "enabled": d.urlChecksOn})
}

func (d *dashboard) loadRun(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	l, err := d.auto.GetLoadRun(r.Context(), string(scope.ProjectID), r.PathValue("loadRunId"))
	if err != nil {
		d.loadErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, loadRunOut(l))
}

// loadErr answers a load-test error.
func (d *dashboard) loadErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, autotest.ErrNotProved):
		httpx.WriteError(w, http.StatusConflict, "OWNERSHIP_NOT_PROVED",
			"a load test runs only against a host the organization proved it owns: publish the token (GET …/environments/{id}/ownership), then check it", nil)
	case errors.Is(err, autotest.ErrBusy):
		httpx.WriteError(w, http.StatusTooManyRequests, "TOO_MANY_LOAD_RUNS", "the organization has a load test running, or the project reached today's limit",
			map[string]any{"maxActive": autotest.MaxActiveLoads, "perDay": autotest.DailyLoads})
	default:
		d.autoErr(w, r, err, "")
	}
}

func schemeOf(base string) string {
	if len(base) >= 5 && base[:5] == "http:" {
		return "http"
	}
	return "https"
}

// pathEnvironment is the environment named in the path, a 404 when it is
// not this project's (a path names a resource; a body field is a 400).
func (d *dashboard) pathEnvironment(w http.ResponseWriter, r *http.Request, project string) (environmentDTO, bool) {
	envs, err := d.projectEnvironments(r, project)
	if err != nil {
		d.internal(w, r, err)
		return environmentDTO{}, false
	}
	env, ok := envs[r.PathValue("environmentId")]
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "environment not found", nil)
		return environmentDTO{}, false
	}
	return env, true
}
