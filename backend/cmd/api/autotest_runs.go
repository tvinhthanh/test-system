package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/autotest"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/internal/testmgmt"
)

// ---------------------------------------------------------------- drafts

type autoDraftDTO struct {
	ID            string            `json:"id"`
	EnvironmentID string            `json:"environmentId"`
	Target        string            `json:"target"`
	Account       string            `json:"account"`
	Inputs        map[string]string `json:"inputs"`
	Context       string            `json:"context"`
	Count         int               `json:"count"`
	TestType      string            `json:"testType"`
	Status        string            `json:"status"`
	Drafts        []autotest.Draft  `json:"drafts"`
	PageRead      bool              `json:"pageRead"`
	Provider      string            `json:"provider"`
	Model         string            `json:"model"`
	KeySource     string            `json:"keySource"`
	Error         string            `json:"error"`
	RequestedBy   string            `json:"requestedBy"`
	CreatedAt     time.Time         `json:"createdAt"`
	FinishedAt    *time.Time        `json:"finishedAt"`
}

func autoDraftOut(dr autotest.DraftRequest) autoDraftDTO {
	out := autoDraftDTO{ID: dr.ID, EnvironmentID: dr.EnvironmentID, Target: dr.Target, Account: dr.Account, Inputs: dr.Inputs, Context: dr.Context,
		Count: dr.Count, TestType: dr.TestType, Status: dr.Status, Drafts: dr.Drafts, PageRead: dr.PageRead, Provider: dr.Provider, Model: dr.Model, KeySource: dr.KeySource,
		Error: dr.Error, RequestedBy: dr.RequestedBy, CreatedAt: dr.CreatedAt}
	if !dr.FinishedAt.IsZero() {
		at := dr.FinishedAt
		out.FinishedAt = &at
	}
	return out
}

func (d *dashboard) createAutoDraft(w http.ResponseWriter, r *http.Request) {
	p, scope, ok := d.tester(w, r)
	if !ok || !d.checkerOn(w) {
		return
	}
	var req struct {
		EnvironmentID string            `json:"environmentId"`
		Target        string            `json:"target"`
		Account       string            `json:"account"`
		Inputs        map[string]string `json:"inputs"`
		Context       string            `json:"context"`
		Count         int               `json:"count"`
		TestType      string            `json:"testType"`
	}
	if !decodeBounded(w, r, &req, 64<<10) {
		return
	}
	dr, err := d.auto.RequestDraft(r.Context(), autotest.NewDraft{ProjectID: string(scope.ProjectID), OrgID: string(scope.OrgID), EnvironmentID: req.EnvironmentID,
		Target: req.Target, Account: req.Account, Inputs: req.Inputs, Context: req.Context, Count: req.Count, TestType: req.TestType, Actor: d.actor(r, p)})
	if err != nil {
		if errors.Is(err, autotest.ErrNotFound) {
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "environmentId is not an environment of this project", map[string]any{"field": "environmentId"})
			return
		}
		d.autoErr(w, r, err, "TOO_MANY_AUTO_DRAFTS")
		return
	}
	writeJSON(w, http.StatusAccepted, autoDraftOut(dr))
}

func (d *dashboard) autoDraft(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	dr, err := d.auto.Draft(r.Context(), string(scope.ProjectID), r.PathValue("draftId"))
	if err != nil {
		d.autoErr(w, r, err, "")
		return
	}
	writeJSON(w, http.StatusOK, autoDraftOut(dr))
}

// ------------------------------------------------------------------ runs

type autoRunCaseDTO struct {
	CaseID      string                `json:"caseId"`
	Variant     string                `json:"variant"` // browser/device, "chromium/desktop" by default (QĐ-19)
	Position    int                   `json:"position"`
	Title       string                `json:"title"`
	Target      string                `json:"target"`
	Account     string                `json:"account"`
	Inputs      map[string]string     `json:"inputs"`
	Steps       []autotest.Step       `json:"steps"`
	Status      string                `json:"status"`
	Results     []autotest.StepResult `json:"results"`
	Console     []string              `json:"console"`
	Healed      int                   `json:"healed"`
	DurationMs  int                   `json:"durationMs"`
	Error       string                `json:"error"`
	Screenshots []string              `json:"screenshots"`
	ExecutionID string                `json:"executionId"`
	FinishedAt  *time.Time            `json:"finishedAt"`
}

type autoRunDTO struct {
	ID            string           `json:"id"`
	EnvironmentID string           `json:"environmentId"`
	PlanID        string           `json:"planId"`
	Status        string           `json:"status"`
	Trigger       string           `json:"trigger"`
	RequestedBy   string           `json:"requestedBy"`
	Heal          bool             `json:"heal"`
	Variants      []string         `json:"variants"`
	Total         int              `json:"total"`
	Passed        int              `json:"passed"`
	Failed        int              `json:"failed"`
	Errored       int              `json:"errored"`
	Healed        int              `json:"healed"`
	TestRunID     string           `json:"testRunId"`
	Error         string           `json:"error"`
	CreatedAt     time.Time        `json:"createdAt"`
	StartedAt     *time.Time       `json:"startedAt"`
	FinishedAt    *time.Time       `json:"finishedAt"`
	Cases         []autoRunCaseDTO `json:"cases,omitempty"`
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func autoRunOut(run autotest.Run) autoRunDTO {
	out := autoRunDTO{ID: run.ID, EnvironmentID: run.EnvironmentID, PlanID: run.PlanID, Status: run.Status, Trigger: run.Trigger, RequestedBy: run.RequestedBy,
		Heal: run.Heal, Variants: labels(run.Variants), Total: run.Total, Passed: run.Passed, Failed: run.Failed, Errored: run.Errored, Healed: run.Healed, TestRunID: run.TestRunID,
		Error: run.Error, CreatedAt: run.CreatedAt, StartedAt: timePtr(run.StartedAt), FinishedAt: timePtr(run.FinishedAt)}
	for _, c := range run.Cases {
		dto := autoRunCaseDTO{CaseID: c.CaseID, Variant: autotest.Label(c.Variant), Position: c.Position, Title: c.Title, Target: c.Spec.Target, Account: c.Spec.Account, Inputs: c.Spec.Inputs,
			Steps: c.Spec.Steps, Status: c.Status, Results: c.Results, Console: c.Console, Healed: c.Healed, DurationMs: c.DurationMs, Error: c.Error,
			Screenshots: []string{}, ExecutionID: c.ExecutionID, FinishedAt: timePtr(c.FinishedAt)}
		if c.HasFinalShot {
			dto.Screenshots = append(dto.Screenshots, "final")
		}
		if c.HasFailureShot {
			dto.Screenshots = append(dto.Screenshots, "failure")
		}
		out.Cases = append(out.Cases, dto)
	}
	return out
}

// startRun records a plan for the cases and queues the run, in one
// transaction. caseIDs nil: every runnable case of the project.
func (d *dashboard) startRun(ctx context.Context, scope tenancy.Scope, env environmentDTO, caseIDs, variants []string, heal bool, trigger, actor string) (autotest.Run, error) {
	project := string(scope.ProjectID)
	if caseIDs == nil {
		specs, err := d.auto.Specs(ctx, project)
		if err != nil {
			return autotest.Run{}, err
		}
		for _, sp := range specs {
			caseIDs = append(caseIDs, sp.CaseID)
		}
	}
	ids := []string{}
	titles := map[string]string{}
	for _, id := range caseIDs {
		tc, err := d.tm.GetTestCase(ctx, project, id)
		if errors.Is(err, testmgmt.ErrNotFound) {
			return autotest.Run{}, fmt.Errorf("%w: case %s is not in this project", autotest.ErrInvalid, id)
		}
		if err != nil {
			return autotest.Run{}, err
		}
		if tc.AutomationStatus == testmgmt.Obsolete {
			continue
		}
		ids = append(ids, tc.ID)
		titles[tc.ID] = tc.Title
	}
	if len(ids) == 0 {
		return autotest.Run{}, fmt.Errorf("%w: no runnable cases to run", autotest.ErrInvalid)
	}
	var run autotest.Run
	err := d.db.WithTx(ctx, func(ctx context.Context) error {
		plan, err := d.tm.CreatePlan(ctx, project, testmgmt.NewPlan{CaseIDs: &ids, Actor: actor, EnvironmentID: env.ID,
			Name: fmt.Sprintf("Auto test · %s · %s", env.Key, time.Now().UTC().Format("2006-01-02 15:04"))})
		if err != nil {
			return err
		}
		run, err = d.auto.CreateRun(ctx, autotest.NewRun{ProjectID: project, OrgID: string(scope.OrgID), EnvironmentID: env.ID, PlanID: plan.ID,
			Cases: ids, Titles: titles, Trigger: trigger, Heal: heal, Variants: variants, Actor: actor})
		return err
	})
	return run, err
}

func (d *dashboard) createAutoRun(w http.ResponseWriter, r *http.Request) {
	p, scope, ok := d.tester(w, r)
	if !ok || !d.checkerOn(w) {
		return
	}
	var req struct {
		EnvironmentID string   `json:"environmentId"`
		CaseIDs       []string `json:"caseIds"`
		Variants      []string `json:"variants"` // browser/device, e.g. "firefox/phone" (QĐ-19); none = chromium/desktop
		Heal          *bool    `json:"heal"`
	}
	if !decodeBounded(w, r, &req, 64<<10) {
		return
	}
	if req.EnvironmentID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "environmentId: the site to run on", map[string]any{"field": "environmentId"})
		return
	}
	envs, ok := d.planEnvironment(w, r, string(scope.ProjectID), req.EnvironmentID)
	if !ok {
		return
	}
	heal := req.Heal == nil || *req.Heal
	run, err := d.startRun(r.Context(), scope, envs[req.EnvironmentID], req.CaseIDs, req.Variants, heal, autotest.TriggerDashboard, d.actor(r, p))
	if err != nil {
		d.autoErr(w, r, err, "TOO_MANY_AUTO_RUNS")
		return
	}
	writeJSON(w, http.StatusAccepted, autoRunOut(run))
}

func (d *dashboard) autoRuns(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	runs, err := d.auto.Runs(r.Context(), string(scope.ProjectID), 50)
	if err != nil {
		d.autoErr(w, r, err, "")
		return
	}
	out := listOut(runs, autoRunOut)
	writeJSON(w, http.StatusOK, map[string]any{"items": out.Items, "nextCursor": nil, "enabled": d.urlChecksOn})
}

func (d *dashboard) autoRun(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	run, err := d.auto.GetRun(r.Context(), string(scope.ProjectID), r.PathValue("autoRunId"))
	if err != nil {
		d.autoErr(w, r, err, "")
		return
	}
	writeJSON(w, http.StatusOK, autoRunOut(run))
}

func (d *dashboard) autoRunShot(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	variant, err := variantParam(r.URL.Query().Get("variant"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "screenshot not found", nil)
		return
	}
	b, err := d.auto.Shot(r.Context(), string(scope.ProjectID), r.PathValue("autoRunId"), r.PathValue("testCaseId"), variant, r.PathValue("shot"))
	if errors.Is(err, autotest.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "screenshot not found", nil)
		return
	}
	if err != nil {
		d.internal(w, r, err)
		return
	}
	// Only JPEGs are stored (autotest.jpegOrNil): the page photographed is
	// untrusted, its picture is not script.
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(b)
}

func (d *dashboard) applyHeal(w http.ResponseWriter, r *http.Request) {
	p, scope, ok := d.tester(w, r)
	if !ok {
		return
	}
	tc, ok := d.specCase(w, r, string(scope.ProjectID))
	if !ok {
		return
	}
	var req struct {
		Step    *int   `json:"step"`
		Variant string `json:"variant"` // the run case's browser/device; "" = chromium/desktop
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	if req.Step == nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "step: the index of the repaired step", map[string]any{"field": "step"})
		return
	}
	variant, err := variantParam(req.Variant)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error(), map[string]any{"field": "variant"})
		return
	}
	actor := d.actor(r, p)
	project := string(scope.ProjectID)
	var sp autotest.Spec
	err = d.db.WithTx(r.Context(), func(ctx context.Context) error {
		var err error
		if sp, err = d.auto.ApplyHeal(ctx, project, r.PathValue("autoRunId"), tc.ID, variant, *req.Step, actor); err != nil {
			return err
		}
		sp, tc, err = d.saveSpecAndCase(ctx, project, tc.ID, autotest.SpecInput{Target: sp.Target, Account: sp.Account, Inputs: sp.Inputs, Steps: sp.Steps, Source: sp.Source, Actor: actor})
		return err
	})
	if err != nil {
		d.autoErr(w, r, err, "")
		return
	}
	writeJSON(w, http.StatusOK, autoSpecOut(sp, tc, nil))
}

// ------------------------------------------------------------- CI token

// ciProject resolves a bearer token with project:autorun to its project.
func (d *dashboard) ciProject(w http.ResponseWriter, r *http.Request) (authz.Principal, tenancy.Scope, bool) {
	p, ok := authz.PrincipalFrom(r.Context())
	if !ok || p.Kind != authz.PrincipalToken {
		httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "bearer token required", nil)
		return p, tenancy.Scope{}, false
	}
	if p.TokenScope.Level != authz.LevelProject {
		httpx.WriteError(w, http.StatusForbidden, "TOKEN_SCOPE", "auto-run tokens are scoped to one project", nil)
		return p, tenancy.Scope{}, false
	}
	scope, err := d.tenancy.ProjectScope(r.Context(), tenancy.ProjectID(p.TokenScope.ID))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "project not found", nil)
		return p, tenancy.Scope{}, false
	}
	res := authz.Resource{OrgID: string(scope.OrgID), WorkspaceID: string(scope.WorkspaceID), ProjectID: string(scope.ProjectID)}
	switch err := d.authz.Authorize(r.Context(), p, res, authz.ActionAutoRun); {
	case err == nil:
		return p, scope, true
	case errors.Is(err, authz.ErrForbidden):
		httpx.WriteError(w, http.StatusForbidden, "FORBIDDEN", "token lacks project:autorun", nil)
	case errors.Is(err, authz.ErrNoAccess):
		httpx.WriteError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "project not found", nil)
	default:
		d.internal(w, r, err)
	}
	return p, tenancy.Scope{}, false
}

type ciRunDTO struct {
	ID         string      `json:"id"`
	Status     string      `json:"status"`
	Total      int         `json:"total"`
	Passed     int         `json:"passed"`
	Failed     int         `json:"failed"`
	Errored    int         `json:"errored"`
	Healed     int         `json:"healed"`
	FinishedAt *time.Time  `json:"finishedAt"`
	URL        string      `json:"url"`
	Cases      []ciCaseDTO `json:"cases"`
}

type ciCaseDTO struct {
	Title   string `json:"title"`
	Variant string `json:"variant"`
	Status  string `json:"status"`
	Error   string `json:"error"`
}

func (d *dashboard) ciRunOut(run autotest.Run) ciRunDTO {
	out := ciRunDTO{ID: run.ID, Status: run.Status, Total: run.Total, Passed: run.Passed, Failed: run.Failed, Errored: run.Errored, Healed: run.Healed,
		FinishedAt: timePtr(run.FinishedAt), URL: strings.TrimSuffix(d.publicURL, "/") + "/p/" + run.ProjectID + "/auto/runs/" + run.ID, Cases: []ciCaseDTO{}}
	for _, c := range run.Cases {
		out.Cases = append(out.Cases, ciCaseDTO{Title: c.Title, Variant: autotest.Label(c.Variant), Status: c.Status, Error: c.Error})
	}
	return out
}

func (d *dashboard) ciCreateAutoRun(w http.ResponseWriter, r *http.Request) {
	p, scope, ok := d.ciProject(w, r)
	if !ok || !d.checkerOn(w) {
		return
	}
	var req struct {
		Environment string   `json:"environment"`
		CaseIDs     []string `json:"caseIds"`
		Variants    []string `json:"variants"`
		Heal        *bool    `json:"heal"`
	}
	if !decodeBounded(w, r, &req, 64<<10) {
		return
	}
	envs, err := d.projectEnvironments(r, string(scope.ProjectID))
	if err != nil {
		d.internal(w, r, err)
		return
	}
	var env environmentDTO
	for _, e := range envs {
		if e.Key == req.Environment {
			env = e
		}
	}
	if env.ID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "environment: the key of one of the project's environments", map[string]any{"field": "environment"})
		return
	}
	heal := req.Heal == nil || *req.Heal
	run, err := d.startRun(r.Context(), scope, env, req.CaseIDs, req.Variants, heal, autotest.TriggerCI, "token:"+p.TokenID)
	if err != nil {
		d.autoErr(w, r, err, "TOO_MANY_AUTO_RUNS")
		return
	}
	writeJSON(w, http.StatusAccepted, d.ciRunOut(run))
}

func (d *dashboard) ciAutoRun(w http.ResponseWriter, r *http.Request) {
	_, scope, ok := d.ciProject(w, r)
	if !ok {
		return
	}
	run, err := d.auto.GetRun(r.Context(), string(scope.ProjectID), r.PathValue("autoRunId"))
	if err != nil {
		d.autoErr(w, r, err, "")
		return
	}
	writeJSON(w, http.StatusOK, d.ciRunOut(run))
}

// variantParam reads a run case's variant as the api shows it
// ("firefox/phone", "chromium/desktop" or "") into its stored form.
func variantParam(s string) (string, error) {
	v, err := autotest.ParseVariant(strings.ToLower(strings.TrimSpace(s)))
	if err != nil {
		return "", err
	}
	return v.String(), nil
}

// labels are a run's variants as the api shows them.
func labels(variants []string) []string {
	out := make([]string, 0, len(variants))
	for _, v := range variants {
		out = append(out, autotest.Label(v))
	}
	return out
}
