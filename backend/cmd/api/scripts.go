package main

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/autotest"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/sandbox"
)

// Script runs (QĐ-19 tier 4), testmgmt.write like Auto test:
//
//	POST /v1/projects/{projectId}/script-runs    multipart: archive (.tar.gz), kind, environmentId, entry?, project?, grep?, vus?, seconds?, timeoutSeconds?   → 202
//	GET  /v1/projects/{projectId}/script-runs
//	GET  /v1/projects/{projectId}/script-runs/{scriptRunId}
func (d *dashboard) scriptRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("POST /v1/projects/{projectId}/script-runs", d.createScriptRun)
	handle("GET /v1/projects/{projectId}/script-runs", d.scriptRuns)
	handle("GET /v1/projects/{projectId}/script-runs/{scriptRunId}", d.scriptRun)
}

type scriptRunDTO struct {
	ID             string     `json:"id"`
	EnvironmentID  string     `json:"environmentId"`
	Kind           string     `json:"kind"`
	Base           string     `json:"base"`
	Entry          string     `json:"entry"`
	Project        string     `json:"project"`
	Grep           string     `json:"grep"`
	VUs            *int       `json:"vus"`
	Seconds        *int       `json:"seconds"`
	TimeoutSeconds int        `json:"timeoutSeconds"`
	ArchiveBytes   int        `json:"archiveBytes"`
	Status         string     `json:"status"`
	Verdict        string     `json:"verdict"`
	ExitCode       *int       `json:"exitCode"`
	TimedOut       bool       `json:"timedOut"`
	Tests          int        `json:"tests"`
	Failures       int        `json:"failures"`
	Log            *string    `json:"log,omitempty"` // the detail only
	Error          string     `json:"error"`
	TestRunID      *string    `json:"testRunId"`
	RequestedBy    string     `json:"requestedBy"`
	CreatedAt      time.Time  `json:"createdAt"`
	StartedAt      *time.Time `json:"startedAt"`
	FinishedAt     *time.Time `json:"finishedAt"`
}

func scriptRunOut(s autotest.ScriptRun, withLog bool) scriptRunDTO {
	out := scriptRunDTO{ID: s.ID, EnvironmentID: s.EnvironmentID, Kind: s.Kind, Base: s.Base, Entry: s.Entry, Project: s.Project, Grep: s.Grep,
		TimeoutSeconds: s.TimeoutSeconds, ArchiveBytes: s.ArchiveBytes, Status: s.Status, Verdict: s.Verdict, ExitCode: s.ExitCode, TimedOut: s.TimedOut,
		Tests: s.Tests, Failures: s.Failures, Error: s.Error, RequestedBy: s.RequestedBy, CreatedAt: s.CreatedAt,
		StartedAt: timePtr(s.StartedAt), FinishedAt: timePtr(s.FinishedAt)}
	if s.Kind == sandbox.KindK6 {
		v, sec := s.VUs, s.Seconds
		out.VUs, out.Seconds = &v, &sec
	}
	if s.TestRunID != "" {
		id := s.TestRunID
		out.TestRunID = &id
	}
	if withLog {
		l := s.Log
		out.Log = &l
	}
	return out
}

// scriptsOn answers 409 when sandboxd is off (SANDBOX).
func (d *dashboard) scriptsOn(w http.ResponseWriter) bool {
	if !d.sandboxOn {
		httpx.WriteError(w, http.StatusConflict, "SCRIPTS_OFF", "scripts run in sandboxd, which is off on this server (the operator turns it on with SANDBOX=on)", nil)
		return false
	}
	return true
}

func (d *dashboard) createScriptRun(w http.ResponseWriter, r *http.Request) {
	p, scope, ok := d.tester(w, r)
	if !ok || !d.scriptsOn(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, sandbox.MaxArchiveBytes+(1<<20))
	if err := r.ParseMultipartForm(sandbox.MaxArchiveBytes + (1 << 20)); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "the archive is over 10 MB", map[string]any{"maxBytes": sandbox.MaxArchiveBytes})
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "multipart/form-data with the archive and its fields", nil)
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, _, err := r.FormFile("archive")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "archive: the project or script as a .tar.gz", map[string]any{"field": "archive"})
		return
	}
	archive, err := io.ReadAll(io.LimitReader(file, sandbox.MaxArchiveBytes+1))
	file.Close()
	if err != nil {
		d.internal(w, r, err)
		return
	}
	if len(archive) > sandbox.MaxArchiveBytes {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "the archive is over 10 MB", map[string]any{"maxBytes": sandbox.MaxArchiveBytes})
		return
	}
	num := func(field string) (int, bool) {
		v := strings.TrimSpace(r.FormValue(field))
		if v == "" {
			return 0, true
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", field+": a whole number", map[string]any{"field": field})
			return 0, false
		}
		return n, true
	}
	vus, ok1 := num("vus")
	seconds, ok2 := num("seconds")
	timeout, ok3 := num("timeoutSeconds")
	if !ok1 || !ok2 || !ok3 {
		return
	}
	envID := strings.TrimSpace(r.FormValue("environmentId"))
	if envID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "environmentId: the site the script tests", map[string]any{"field": "environmentId"})
		return
	}
	if _, ok := d.planEnvironment(w, r, string(scope.ProjectID), envID); !ok {
		return
	}
	s, err := d.auto.CreateScriptRun(r.Context(), autotest.NewScriptRun{ProjectID: string(scope.ProjectID), OrgID: string(scope.OrgID), EnvironmentID: envID,
		Kind: strings.TrimSpace(r.FormValue("kind")), Archive: archive, Entry: r.FormValue("entry"), Project: r.FormValue("project"), Grep: r.FormValue("grep"),
		VUs: vus, Seconds: seconds, TimeoutSeconds: timeout, Actor: d.actor(r, p)})
	if err != nil {
		d.scriptErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, scriptRunOut(s, true))
}

func (d *dashboard) scriptRuns(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	runs, err := d.auto.ScriptRuns(r.Context(), string(scope.ProjectID), 50)
	if err != nil {
		d.scriptErr(w, r, err)
		return
	}
	items := make([]scriptRunDTO, 0, len(runs))
	for _, s := range runs {
		items = append(items, scriptRunOut(s, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": nil, "enabled": d.sandboxOn})
}

func (d *dashboard) scriptRun(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	s, err := d.auto.GetScriptRun(r.Context(), string(scope.ProjectID), r.PathValue("scriptRunId"))
	if err != nil {
		d.scriptErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, scriptRunOut(s, true))
}

func (d *dashboard) scriptErr(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, autotest.ErrBusy) {
		httpx.WriteError(w, http.StatusTooManyRequests, "TOO_MANY_SCRIPT_RUNS", "the organization has a script running, or the project reached today's limit",
			map[string]any{"maxActive": autotest.MaxActiveScripts, "perDay": autotest.DailyScripts})
		return
	}
	d.loadErr(w, r, err)
}
