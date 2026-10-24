package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/autotest"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/internal/testmgmt"
	"github.com/vinh/automation-testing-platform/backend/internal/urlcheck"
)

// Auto test (QĐ-16): test cases the server runs by itself. Testers
// (testmgmt.write) keep an environment's test accounts, write or draft
// runnable cases, run them and scan a site; anyone in the project reads.
// A runnable case is a testmgmt case plus its spec (autotest): creating
// one makes both in one transaction, and saving a spec rewrites the case's
// human-readable steps from it. A run is recorded as a testmgmt plan (made
// here, with the run) and, by the worker, as a test run. Running needs the
// checker service (URLCHECK=on); drafting needs an AI key. Test accounts'
// passwords are never returned; changes to accounts are audited as
// project.configure.
//
//	GET    /v1/projects/{projectId}/environments/{environmentId}/accounts
//	POST   /v1/projects/{projectId}/environments/{environmentId}/accounts {key, username, password?, loginUrl?, loginSteps?}
//	DELETE /v1/projects/{projectId}/environments/{environmentId}/accounts/{accountKey}
//	POST   /v1/projects/{projectId}/environments/{environmentId}/accounts/{accountKey}/find-login   → 202
//	GET    /v1/projects/{projectId}/auto-specs                                  → {items, enabled}
//	POST   /v1/projects/{projectId}/auto-specs {title, priority?, testType?, suiteId?, target, account?, inputs?, steps, source?}   → 201
//	GET    /v1/projects/{projectId}/auto-specs/export?environmentId=&caseIds=a,b   → {filename, content}
//	GET    /v1/projects/{projectId}/test-cases/{testCaseId}/auto-spec
//	POST   /v1/projects/{projectId}/test-cases/{testCaseId}/auto-spec {target, account?, inputs?, steps}
//	DELETE /v1/projects/{projectId}/test-cases/{testCaseId}/auto-spec
//	POST   /v1/projects/{projectId}/auto-drafts {environmentId, target, account?, inputs?, context?, count?, testType?}   → 202
//	GET    /v1/projects/{projectId}/auto-drafts/{draftId}
//	POST   /v1/projects/{projectId}/auto-runs {environmentId, caseIds?, heal?}   → 202
//	GET    /v1/projects/{projectId}/auto-runs
//	GET    /v1/projects/{projectId}/auto-runs/{autoRunId}
//	GET    /v1/projects/{projectId}/auto-runs/{autoRunId}/cases/{testCaseId}/screenshots/{shot}   final | failure, image/jpeg
//	POST   /v1/projects/{projectId}/auto-runs/{autoRunId}/cases/{testCaseId}/apply-heal {step}
//	POST   /v1/projects/{projectId}/site-scans {url?, environmentId?, account?, maxPages?}   → 202
//	GET    /v1/projects/{projectId}/site-scans
//	GET    /v1/projects/{projectId}/site-scans/{scanId}
//	POST   /v1/auto-runs {environment, caseIds?, heal?}   bearer token with project:autorun — a deploy pipeline
//	GET    /v1/auto-runs/{autoRunId}                          bearer token with project:autorun

func (d *dashboard) autotestRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("GET /v1/projects/{projectId}/environments/{environmentId}/accounts", d.accounts)
	handle("POST /v1/projects/{projectId}/environments/{environmentId}/accounts", d.saveAccount)
	handle("DELETE /v1/projects/{projectId}/environments/{environmentId}/accounts/{accountKey}", d.deleteAccount)
	handle("POST /v1/projects/{projectId}/environments/{environmentId}/accounts/{accountKey}/find-login", d.findLogin)
	handle("GET /v1/projects/{projectId}/auto-specs", d.autoSpecs)
	handle("POST /v1/projects/{projectId}/auto-specs", d.createAutoCase)
	handle("GET /v1/projects/{projectId}/auto-specs/export", d.exportAutoSpecs)
	handle("GET /v1/projects/{projectId}/test-cases/{testCaseId}/auto-spec", d.autoSpec)
	handle("POST /v1/projects/{projectId}/test-cases/{testCaseId}/auto-spec", d.saveAutoSpec)
	handle("DELETE /v1/projects/{projectId}/test-cases/{testCaseId}/auto-spec", d.deleteAutoSpec)
	handle("POST /v1/projects/{projectId}/auto-drafts", d.createAutoDraft)
	handle("GET /v1/projects/{projectId}/auto-drafts/{draftId}", d.autoDraft)
	handle("POST /v1/projects/{projectId}/auto-runs", d.createAutoRun)
	handle("GET /v1/projects/{projectId}/auto-runs", d.autoRuns)
	handle("GET /v1/projects/{projectId}/auto-runs/{autoRunId}", d.autoRun)
	handle("GET /v1/projects/{projectId}/auto-runs/{autoRunId}/cases/{testCaseId}/screenshots/{shot}", d.autoRunShot)
	handle("POST /v1/projects/{projectId}/auto-runs/{autoRunId}/cases/{testCaseId}/apply-heal", d.applyHeal)
	handle("POST /v1/projects/{projectId}/site-scans", d.createSiteScan)
	handle("GET /v1/projects/{projectId}/site-scans", d.siteScans)
	handle("GET /v1/projects/{projectId}/site-scans/{scanId}", d.siteScan)
}

// autotestCIRoutes are the deploy pipeline's: bearer tokens, outside the
// session-only dashboard routes (like the release gate for CI).
func (d *dashboard) autotestCIRoutes(mux httpx.Mux) {
	mux.Handle("POST /v1/auto-runs", authz.Middleware(d.authz)(http.HandlerFunc(d.ciCreateAutoRun)))
	mux.Handle("GET /v1/auto-runs/{autoRunId}", authz.Middleware(d.authz)(http.HandlerFunc(d.ciAutoRun)))
}

// autoErr answers an autotest error; busy is the 429's code.
func (d *dashboard) autoErr(w http.ResponseWriter, r *http.Request, err error, busy string) {
	switch {
	case errors.Is(err, autotest.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "not found", nil)
	case errors.Is(err, autotest.ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", strings.TrimPrefix(err.Error(), "autotest: invalid input: "), nil)
	case errors.Is(err, autotest.ErrNotConfigured):
		httpx.WriteError(w, http.StatusConflict, "AI_NOT_CONFIGURED", "no AI key: an organization admin adds one under Team → AI settings, or the operator sets LLM_API_KEY", nil)
	case errors.Is(err, autotest.ErrBusy):
		limits := map[string]any{"maxWaiting": autotest.MaxQueuedDrafts, "perDay": autotest.DailyDrafts}
		if busy == "TOO_MANY_AUTO_RUNS" {
			limits = map[string]any{"maxActive": autotest.MaxActiveRuns, "perDay": autotest.DailyRuns}
		}
		httpx.WriteError(w, http.StatusTooManyRequests, busy, "this project has requests waiting or reached today's limit", limits)
	case errors.Is(err, testmgmt.ErrNotFound), errors.Is(err, testmgmt.ErrInvalid), errors.Is(err, testmgmt.ErrConflict):
		d.tmErr(w, r, err)
	default:
		d.internal(w, r, err)
	}
}

// checkerOn answers 409 when the checker service is off (URLCHECK).
func (d *dashboard) checkerOn(w http.ResponseWriter) bool {
	if !d.urlChecksOn {
		httpx.WriteError(w, http.StatusConflict, "AUTO_TEST_OFF", "cases run in the checker service, which is off on this server (the operator turns it on with URLCHECK=on)", nil)
		return false
	}
	return true
}

// tester resolves a tester of the project (testmgmt.write) with the scope.
func (d *dashboard) tester(w http.ResponseWriter, r *http.Request) (authz.Principal, tenancy.Scope, bool) {
	p, ok := d.user(w, r)
	if !ok {
		return p, tenancy.Scope{}, false
	}
	scope, ok := d.project(w, r, p, authz.ActionTestmgmtWrite)
	return p, scope, ok
}

// ------------------------------------------------------------- accounts

type accountDTO struct {
	Key            string          `json:"key"`
	Username       string          `json:"username"`
	LoginURL       string          `json:"loginUrl"`
	LoginSteps     []autotest.Step `json:"loginSteps"` // null until found or written
	LoginStatus    string          `json:"loginStatus"`
	LoginError     string          `json:"loginError"`
	LoginCheckedAt *time.Time      `json:"loginCheckedAt"`
	UpdatedBy      string          `json:"updatedBy"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

func accountOut(a autotest.Account) accountDTO {
	out := accountDTO{Key: a.Key, Username: a.Username, LoginURL: a.LoginURL, LoginSteps: a.LoginSteps, LoginStatus: a.LoginStatus,
		LoginError: a.LoginError, UpdatedBy: a.UpdatedBy, UpdatedAt: a.UpdatedAt}
	if !a.LoginCheckedAt.IsZero() {
		at := a.LoginCheckedAt
		out.LoginCheckedAt = &at
	}
	return out
}

func (d *dashboard) accounts(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	items, err := d.auto.Accounts(r.Context(), string(scope.ProjectID), r.PathValue("environmentId"))
	if err != nil {
		d.autoErr(w, r, err, "")
		return
	}
	writeJSON(w, http.StatusOK, listOut(items, accountOut))
}

func (d *dashboard) saveAccount(w http.ResponseWriter, r *http.Request) {
	p, scope, ok := d.tester(w, r)
	if !ok {
		return
	}
	var req struct {
		Key        string           `json:"key"`
		Username   string           `json:"username"`
		Password   string           `json:"password"`
		LoginURL   string           `json:"loginUrl"`
		LoginSteps *[]autotest.Step `json:"loginSteps"`
	}
	if !decodeBounded(w, r, &req, 128<<10) {
		return
	}
	actor := d.actor(r, p)
	a, err := d.auto.SaveAccount(r.Context(), string(scope.ProjectID), r.PathValue("environmentId"), autotest.AccountInput{
		Key: req.Key, Username: req.Username, Password: req.Password, LoginURL: req.LoginURL, LoginSteps: req.LoginSteps, Actor: actor})
	if err != nil {
		d.autoErr(w, r, err, "")
		return
	}
	if err := d.authz.AuditProject(r.Context(), "project.configure", string(scope.ProjectID), string(scope.WorkspaceID), actor,
		map[string]any{"testAccount": a.Key, "environmentId": a.EnvironmentID, "username": a.Username, "passwordChanged": req.Password != "",
			"loginStepsChanged": req.LoginSteps != nil}); err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, accountOut(a))
}

func (d *dashboard) deleteAccount(w http.ResponseWriter, r *http.Request) {
	p, scope, ok := d.tester(w, r)
	if !ok {
		return
	}
	removed, err := d.auto.DeleteAccount(r.Context(), string(scope.ProjectID), r.PathValue("environmentId"), r.PathValue("accountKey"))
	if err != nil {
		d.autoErr(w, r, err, "")
		return
	}
	if !removed {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "test account not found", nil)
		return
	}
	if err := d.authz.AuditProject(r.Context(), "project.configure", string(scope.ProjectID), string(scope.WorkspaceID), d.actor(r, p),
		map[string]any{"testAccount": r.PathValue("accountKey"), "environmentId": r.PathValue("environmentId"), "removed": true}); err != nil {
		d.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *dashboard) findLogin(w http.ResponseWriter, r *http.Request) {
	_, scope, ok := d.tester(w, r)
	if !ok {
		return
	}
	// The account is the project's before anything else is said about it.
	if _, err := d.auto.LoginFor(r.Context(), string(scope.ProjectID), r.PathValue("environmentId"), r.PathValue("accountKey")); errors.Is(err, autotest.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "test account not found", nil)
		return
	}
	if !d.checkerOn(w) {
		return
	}
	a, err := d.auto.FindLogin(r.Context(), string(scope.ProjectID), string(scope.OrgID), r.PathValue("environmentId"), r.PathValue("accountKey"))
	if err != nil {
		d.autoErr(w, r, err, "")
		return
	}
	writeJSON(w, http.StatusAccepted, accountOut(a))
}

// ----------------------------------------------------------------- specs

type autoLastDTO struct {
	RunID      string    `json:"runId"`
	Status     string    `json:"status"`
	FinishedAt time.Time `json:"finishedAt"`
}

type autoSpecDTO struct {
	CaseID    string            `json:"caseId"`
	Title     string            `json:"title"`
	Priority  string            `json:"priority"`
	Target    string            `json:"target"`
	Account   string            `json:"account"`
	Inputs    map[string]string `json:"inputs"`
	Steps     []autotest.Step   `json:"steps"`
	Source    string            `json:"source"`
	UpdatedBy string            `json:"updatedBy"`
	UpdatedAt time.Time         `json:"updatedAt"`
	Last      *autoLastDTO      `json:"last"`
}

func autoSpecOut(sp autotest.Spec, tc testmgmt.TestCase, last map[string]autotest.LastResult) autoSpecDTO {
	out := autoSpecDTO{CaseID: sp.CaseID, Title: tc.Title, Priority: string(tc.Priority), Target: sp.Target, Account: sp.Account,
		Inputs: sp.Inputs, Steps: sp.Steps, Source: sp.Source, UpdatedBy: sp.UpdatedBy, UpdatedAt: sp.UpdatedAt}
	if l, ok := last[sp.CaseID]; ok {
		out.Last = &autoLastDTO{RunID: l.RunID, Status: l.Status, FinishedAt: l.FinishedAt}
	}
	return out
}

// caseSteps is a spec in words, for the testmgmt case: one step each.
func caseSteps(steps []autotest.Step) []testmgmt.Step {
	out := make([]testmgmt.Step, 0, len(steps))
	for _, s := range steps {
		action := s.Say
		if action == "" {
			action = autotest.Describe(s)
		}
		out = append(out, testmgmt.Step{Action: action, Expected: s.Expected})
	}
	return out
}

// caseContext is the start page, account and inputs, written on the case.
func caseContext(target, account string, inputs map[string]string) (preconditions, testData string) {
	preconditions = "Starts at " + target
	if account != "" {
		preconditions += ", signed in with the environment's test account “" + account + "”"
	}
	keys := make([]string, 0, len(inputs))
	for k := range inputs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+" = "+inputs[k])
	}
	return preconditions + ".", strings.Join(lines, "\n")
}

// testTypeTaxonomy files a runnable case under its test type, as automated.
func testTypeTaxonomy(testType string) testmgmt.Taxonomy {
	t := testmgmt.Taxonomy{Level: "system", Purposes: []string{}, Tags: []string{}, ExecutionModes: []string{"automated"}, PerformanceTypes: []string{}}
	switch testType {
	case "":
	case "smoke", "regression", "e2e":
		t.Tags = []string{testType}
	default:
		t.Purposes = []string{testType}
	}
	return t
}

func (d *dashboard) autoSpecs(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	specs, err := d.auto.Specs(ctx, string(scope.ProjectID))
	if err != nil {
		d.autoErr(w, r, err, "")
		return
	}
	last, err := d.auto.LastResults(ctx, string(scope.ProjectID))
	if err != nil {
		d.internal(w, r, err)
		return
	}
	items := make([]autoSpecDTO, 0, len(specs))
	for _, sp := range specs {
		tc, err := d.tm.GetTestCase(ctx, string(scope.ProjectID), sp.CaseID)
		if errors.Is(err, testmgmt.ErrNotFound) {
			continue // the case went (project purge in progress)
		}
		if err != nil {
			d.internal(w, r, err)
			return
		}
		if tc.AutomationStatus == testmgmt.Obsolete {
			continue
		}
		items = append(items, autoSpecOut(sp, tc, last))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": nil, "enabled": d.urlChecksOn})
}

type specRequest struct {
	Title    string            `json:"title"`
	Priority string            `json:"priority"`
	TestType string            `json:"testType"`
	SuiteID  string            `json:"suiteId"`
	Target   string            `json:"target"`
	Account  string            `json:"account"`
	Inputs   map[string]string `json:"inputs"`
	Steps    []autotest.Step   `json:"steps"`
	Source   string            `json:"source"`
}

func (d *dashboard) createAutoCase(w http.ResponseWriter, r *http.Request) {
	p, scope, ok := d.tester(w, r)
	if !ok {
		return
	}
	var req specRequest
	if !decodeBounded(w, r, &req, 256<<10) {
		return
	}
	if req.TestType != "" && !slices.Contains(testmgmt.PlanTestTypes, req.TestType) {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "testType is one of the plan test types", map[string]any{"field": "testType"})
		return
	}
	actor := d.actor(r, p)
	project := string(scope.ProjectID)
	pre, data := caseContext(req.Target, req.Account, req.Inputs)
	var tc testmgmt.TestCase
	var sp autotest.Spec
	err := d.db.WithTx(r.Context(), func(ctx context.Context) error {
		var err error
		tc, err = d.tm.CreateTestCase(ctx, project, testmgmt.NewTestCase{Title: req.Title, Priority: testmgmt.Priority(req.Priority), SuiteID: req.SuiteID,
			Steps: caseSteps(req.Steps), Preconditions: pre, TestData: data, Taxonomy: testTypeTaxonomy(req.TestType), Actor: actor})
		if err != nil {
			return err
		}
		sp, err = d.auto.SaveSpec(ctx, project, tc.ID, autotest.SpecInput{Target: req.Target, Account: req.Account, Inputs: req.Inputs, Steps: req.Steps, Source: req.Source, Actor: actor})
		return err
	})
	if err != nil {
		d.autoErr(w, r, err, "")
		return
	}
	writeJSON(w, http.StatusCreated, autoSpecOut(sp, tc, nil))
}

// specCase returns the project's case, or answers 404.
func (d *dashboard) specCase(w http.ResponseWriter, r *http.Request, project string) (testmgmt.TestCase, bool) {
	tc, err := d.tm.GetTestCase(r.Context(), project, r.PathValue("testCaseId"))
	if err != nil {
		d.tmErr(w, r, err)
		return tc, false
	}
	return tc, true
}

func (d *dashboard) autoSpec(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	tc, ok := d.specCase(w, r, string(scope.ProjectID))
	if !ok {
		return
	}
	sp, err := d.auto.Spec(r.Context(), string(scope.ProjectID), tc.ID)
	if err != nil {
		d.autoErr(w, r, err, "")
		return
	}
	last, err := d.auto.LastResults(r.Context(), string(scope.ProjectID))
	if err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, autoSpecOut(sp, tc, last))
}

// saveSpecAndCase stores a spec and rewrites the case's words from it, in
// one transaction.
func (d *dashboard) saveSpecAndCase(ctx context.Context, project, caseID string, in autotest.SpecInput) (autotest.Spec, testmgmt.TestCase, error) {
	var sp autotest.Spec
	var tc testmgmt.TestCase
	err := d.db.WithTx(ctx, func(ctx context.Context) error {
		var err error
		if sp, err = d.auto.SaveSpec(ctx, project, caseID, in); err != nil {
			return err
		}
		steps := caseSteps(sp.Steps)
		pre, data := caseContext(sp.Target, sp.Account, sp.Inputs)
		tc, err = d.tm.UpdateTestCase(ctx, project, caseID, testmgmt.TestCasePatch{Steps: &steps, Preconditions: &pre, TestData: &data, Actor: in.Actor})
		return err
	})
	return sp, tc, err
}

func (d *dashboard) saveAutoSpec(w http.ResponseWriter, r *http.Request) {
	p, scope, ok := d.tester(w, r)
	if !ok {
		return
	}
	tc, ok := d.specCase(w, r, string(scope.ProjectID))
	if !ok {
		return
	}
	var req specRequest
	if !decodeBounded(w, r, &req, 256<<10) {
		return
	}
	sp, tc, err := d.saveSpecAndCase(r.Context(), string(scope.ProjectID), tc.ID, autotest.SpecInput{Target: req.Target, Account: req.Account,
		Inputs: req.Inputs, Steps: req.Steps, Source: req.Source, Actor: d.actor(r, p)})
	if err != nil {
		d.autoErr(w, r, err, "")
		return
	}
	writeJSON(w, http.StatusOK, autoSpecOut(sp, tc, nil))
}

func (d *dashboard) deleteAutoSpec(w http.ResponseWriter, r *http.Request) {
	_, scope, ok := d.tester(w, r)
	if !ok {
		return
	}
	removed, err := d.auto.DeleteSpec(r.Context(), string(scope.ProjectID), r.PathValue("testCaseId"))
	if err != nil {
		d.autoErr(w, r, err, "")
		return
	}
	if !removed {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "this case has no runnable steps", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *dashboard) exportAutoSpecs(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	project := string(scope.ProjectID)
	envs, ok := d.planEnvironment(w, r, project, r.URL.Query().Get("environmentId"))
	if !ok {
		return
	}
	env := envs[r.URL.Query().Get("environmentId")]
	var want []string
	if ids := strings.TrimSpace(r.URL.Query().Get("caseIds")); ids != "" {
		want = strings.Split(ids, ",")
	}
	specs, err := d.auto.Specs(ctx, project)
	if err != nil {
		d.autoErr(w, r, err, "")
		return
	}
	var cases []autotest.ExportCase
	for _, sp := range specs {
		if want != nil && !slices.Contains(want, sp.CaseID) {
			continue
		}
		tc, err := d.tm.GetTestCase(ctx, project, sp.CaseID)
		if err != nil || tc.AutomationStatus == testmgmt.Obsolete {
			continue
		}
		cases = append(cases, autotest.ExportCase{Title: tc.Title, Spec: sp})
	}
	if len(cases) == 0 {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "no runnable cases to export", nil)
		return
	}
	accounts := map[string]autotest.Account{}
	if env.ID != "" {
		list, err := d.auto.Accounts(ctx, project, env.ID)
		if err != nil {
			d.autoErr(w, r, err, "")
			return
		}
		for _, a := range list {
			accounts[a.Key] = a
		}
	}
	base := env.BaseURL
	if base == "" {
		base = "https://staging.example.com"
	}
	key := env.Key
	if key == "" {
		key = "any"
	}
	writeJSON(w, http.StatusOK, map[string]any{"filename": "atp-auto.spec.ts", "content": string(autotest.Playwright(cases, accounts, base, key))})
}

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
	Model         string            `json:"model"`
	KeySource     string            `json:"keySource"`
	Error         string            `json:"error"`
	RequestedBy   string            `json:"requestedBy"`
	CreatedAt     time.Time         `json:"createdAt"`
	FinishedAt    *time.Time        `json:"finishedAt"`
}

func autoDraftOut(dr autotest.DraftRequest) autoDraftDTO {
	out := autoDraftDTO{ID: dr.ID, EnvironmentID: dr.EnvironmentID, Target: dr.Target, Account: dr.Account, Inputs: dr.Inputs, Context: dr.Context,
		Count: dr.Count, TestType: dr.TestType, Status: dr.Status, Drafts: dr.Drafts, PageRead: dr.PageRead, Model: dr.Model, KeySource: dr.KeySource,
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
		Heal: run.Heal, Total: run.Total, Passed: run.Passed, Failed: run.Failed, Errored: run.Errored, Healed: run.Healed, TestRunID: run.TestRunID,
		Error: run.Error, CreatedAt: run.CreatedAt, StartedAt: timePtr(run.StartedAt), FinishedAt: timePtr(run.FinishedAt)}
	for _, c := range run.Cases {
		dto := autoRunCaseDTO{CaseID: c.CaseID, Position: c.Position, Title: c.Title, Target: c.Spec.Target, Account: c.Spec.Account, Inputs: c.Spec.Inputs,
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
func (d *dashboard) startRun(ctx context.Context, scope tenancy.Scope, env environmentDTO, caseIDs []string, heal bool, trigger, actor string) (autotest.Run, error) {
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
			Cases: ids, Titles: titles, Trigger: trigger, Heal: heal, Actor: actor})
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
	run, err := d.startRun(r.Context(), scope, envs[req.EnvironmentID], req.CaseIDs, heal, autotest.TriggerDashboard, d.actor(r, p))
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
	b, err := d.auto.Shot(r.Context(), string(scope.ProjectID), r.PathValue("autoRunId"), r.PathValue("testCaseId"), r.PathValue("shot"))
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
		Step *int `json:"step"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	if req.Step == nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "step: the index of the repaired step", map[string]any{"field": "step"})
		return
	}
	actor := d.actor(r, p)
	project := string(scope.ProjectID)
	var sp autotest.Spec
	err := d.db.WithTx(r.Context(), func(ctx context.Context) error {
		var err error
		if sp, err = d.auto.ApplyHeal(ctx, project, r.PathValue("autoRunId"), tc.ID, *req.Step, actor); err != nil {
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
	Title  string `json:"title"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

func (d *dashboard) ciRunOut(run autotest.Run) ciRunDTO {
	out := ciRunDTO{ID: run.ID, Status: run.Status, Total: run.Total, Passed: run.Passed, Failed: run.Failed, Errored: run.Errored, Healed: run.Healed,
		FinishedAt: timePtr(run.FinishedAt), URL: strings.TrimSuffix(d.publicURL, "/") + "/p/" + run.ProjectID + "/auto/runs/" + run.ID, Cases: []ciCaseDTO{}}
	for _, c := range run.Cases {
		out.Cases = append(out.Cases, ciCaseDTO{Title: c.Title, Status: c.Status, Error: c.Error})
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
	run, err := d.startRun(r.Context(), scope, env, req.CaseIDs, heal, autotest.TriggerCI, "token:"+p.TokenID)
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

// ------------------------------------------------------------ site scans

type siteScanDTO struct {
	ID            string        `json:"id"`
	EnvironmentID string        `json:"environmentId"`
	StartURL      string        `json:"startUrl"`
	MaxPages      int           `json:"maxPages"`
	Account       string        `json:"account"`
	Status        string        `json:"status"`
	RequestedBy   string        `json:"requestedBy"`
	Error         string        `json:"error"`
	Pages         int           `json:"pages"`
	Checked       int           `json:"checked"`
	Passed        int           `json:"passed"`
	Warnings      int           `json:"warnings"`
	Failed        int           `json:"failed"`
	CreatedAt     time.Time     `json:"createdAt"`
	FinishedAt    *time.Time    `json:"finishedAt"`
	Checks        []scanPageDTO `json:"checks,omitempty"`
}

type scanPageDTO struct {
	urlCheckDTO
	Depth int `json:"depth"`
}

func siteScanOut(sc urlcheck.Scan) siteScanDTO {
	out := siteScanDTO{ID: sc.ID, EnvironmentID: sc.EnvironmentID, StartURL: sc.StartURL, MaxPages: sc.MaxPages, Account: sc.Account, Status: sc.Status,
		RequestedBy: sc.RequestedBy, Error: sc.Error, Pages: sc.Pages, Checked: sc.Checked, Passed: sc.Passed, Warnings: sc.Warnings, Failed: sc.Failed,
		CreatedAt: sc.CreatedAt, FinishedAt: timePtr(sc.FinishedAt)}
	for _, c := range sc.Checks {
		out.Checks = append(out.Checks, scanPageDTO{urlCheckDTO: urlCheckOut(c), Depth: c.Depth})
	}
	return out
}

func (d *dashboard) createSiteScan(w http.ResponseWriter, r *http.Request) {
	p, scope, ok := d.tester(w, r)
	if !ok {
		return
	}
	if !d.urlChecksOn {
		httpx.WriteError(w, http.StatusConflict, "URL_CHECKS_OFF", "URL checks are off on this server (the operator turns them on with URLCHECK=on)", nil)
		return
	}
	var req struct {
		URL           string `json:"url"`
		EnvironmentID string `json:"environmentId"`
		Account       string `json:"account"`
		MaxPages      int    `json:"maxPages"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	project := string(scope.ProjectID)
	if req.EnvironmentID != "" {
		envs, ok := d.planEnvironment(w, r, project, req.EnvironmentID)
		if !ok {
			return
		}
		if strings.TrimSpace(req.URL) == "" {
			req.URL = envs[req.EnvironmentID].BaseURL
		}
	}
	if strings.TrimSpace(req.URL) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "url: the page to start from, or an environment with an address", map[string]any{"field": "url"})
		return
	}
	// A signed-in scan needs an account whose sign-in already works.
	if req.Account != "" {
		login, err := d.auto.LoginFor(r.Context(), project, req.EnvironmentID, req.Account)
		switch {
		case errors.Is(err, autotest.ErrNotFound):
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "account: not a test account of that environment", map[string]any{"field": "account"})
			return
		case err != nil:
			d.autoErr(w, r, err, "")
			return
		case login == nil:
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "account: its sign-in steps are not known yet — find them first", map[string]any{"field": "account"})
			return
		}
	}
	sc, err := d.urlcheck.CreateScan(r.Context(), urlcheck.NewScan{ProjectID: project, EnvironmentID: req.EnvironmentID, URL: req.URL,
		Account: req.Account, MaxPages: req.MaxPages, Actor: d.actor(r, p)})
	switch {
	case errors.Is(err, urlcheck.ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", strings.TrimPrefix(err.Error(), "urlcheck: invalid input: "), nil)
	case errors.Is(err, urlcheck.ErrBusy):
		httpx.WriteError(w, http.StatusTooManyRequests, "SCAN_RUNNING", "this project already has a scan running", map[string]any{"max": urlcheck.MaxScansRunning})
	case err != nil:
		d.internal(w, r, err)
	default:
		writeJSON(w, http.StatusAccepted, siteScanOut(sc))
	}
}

func (d *dashboard) siteScans(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	scans, err := d.urlcheck.Scans(r.Context(), string(scope.ProjectID), 30)
	if err != nil {
		d.internal(w, r, err)
		return
	}
	out := listOut(scans, siteScanOut)
	writeJSON(w, http.StatusOK, map[string]any{"items": out.Items, "nextCursor": nil, "enabled": d.urlChecksOn})
}

func (d *dashboard) siteScan(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	sc, err := d.urlcheck.GetScan(r.Context(), string(scope.ProjectID), r.PathValue("scanId"))
	if errors.Is(err, urlcheck.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "scan not found", nil)
		return
	}
	if err != nil {
		d.internal(w, r, err)
		return
	}
	out := siteScanOut(sc)
	if out.Checks == nil {
		out.Checks = []scanPageDTO{}
	}
	writeJSON(w, http.StatusOK, out)
}
