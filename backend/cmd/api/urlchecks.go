package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/urlcheck"
)

// URL checks (QĐ-14): a tester asks for a page — typed, or an environment's
// address — to be opened and checked by the checker service; the worker
// runs it (urlcheck). Members and up ask; anyone in the project reads.
// Asking needs URLCHECK=on; reading old checks does not.
//
//	POST /v1/projects/{projectId}/url-checks {url?, environmentId?}   → 202
//	GET  /v1/projects/{projectId}/url-checks                          → {items, nextCursor: null, enabled}
//	GET  /v1/projects/{projectId}/url-checks/{checkId}
//	GET  /v1/projects/{projectId}/url-checks/{checkId}/screenshots/{which}   desktop | mobile, image/jpeg

func (d *dashboard) urlCheckRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("POST /v1/projects/{projectId}/url-checks", d.createURLCheck)
	handle("GET /v1/projects/{projectId}/url-checks", d.urlChecks)
	handle("GET /v1/projects/{projectId}/url-checks/{checkId}", d.urlCheck)
	handle("GET /v1/projects/{projectId}/url-checks/{checkId}/screenshots/{which}", d.urlCheckScreenshot)
}

type urlCheckDTO struct {
	ID            string          `json:"id"`
	EnvironmentID string          `json:"environmentId"`
	URL           string          `json:"url"`
	Status        string          `json:"status"`
	Verdict       string          `json:"verdict"`
	Title         string          `json:"title"`
	FinalURL      string          `json:"finalUrl"`
	RequestedBy   string          `json:"requestedBy"`
	Statuses      []string        `json:"checkStatuses"`
	Report        json.RawMessage `json:"report,omitempty"`
	Error         string          `json:"error"`
	Screenshots   []string        `json:"screenshots"`
	CreatedAt     time.Time       `json:"createdAt"`
	FinishedAt    *time.Time      `json:"finishedAt"`
}

func urlCheckOut(c urlcheck.Check) urlCheckDTO {
	out := urlCheckDTO{ID: c.ID, EnvironmentID: c.EnvironmentID, URL: c.URL, Status: c.Status, Verdict: c.Verdict, Title: c.Title,
		FinalURL: c.FinalURL, RequestedBy: c.RequestedBy, Statuses: c.Statuses, Report: c.Report, Error: c.Error,
		Screenshots: []string{}, CreatedAt: c.CreatedAt}
	if c.HasDesktop {
		out.Screenshots = append(out.Screenshots, "desktop")
	}
	if c.HasMobile {
		out.Screenshots = append(out.Screenshots, "mobile")
	}
	if !c.FinishedAt.IsZero() {
		at := c.FinishedAt
		out.FinishedAt = &at
	}
	return out
}

func (d *dashboard) createURLCheck(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionTestmgmtWrite)
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
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	// An environment alone means its address; with a URL too, the URL is
	// checked and the environment recorded beside it.
	if req.EnvironmentID != "" {
		envs, ok := d.planEnvironment(w, r, string(scope.ProjectID), req.EnvironmentID)
		if !ok {
			return
		}
		if strings.TrimSpace(req.URL) == "" {
			if req.URL = envs[req.EnvironmentID].BaseURL; req.URL == "" {
				httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "that environment has no address; give a url or record one", map[string]any{"field": "url"})
				return
			}
		}
	}
	c, err := d.urlcheck.Create(r.Context(), string(scope.ProjectID), req.EnvironmentID, req.URL, d.actor(r, p))
	switch {
	case errors.Is(err, urlcheck.ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", strings.TrimPrefix(err.Error(), "urlcheck: invalid input: "), map[string]any{"field": "url"})
	case errors.Is(err, urlcheck.ErrBusy):
		httpx.WriteError(w, http.StatusTooManyRequests, "TOO_MANY_URL_CHECKS", "this project already has checks waiting; try again when they finish", map[string]any{"max": urlcheck.MaxQueued})
	case err != nil:
		d.internal(w, r, err)
	default:
		writeJSON(w, http.StatusAccepted, urlCheckOut(c))
	}
}

func (d *dashboard) urlChecks(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	items, err := d.urlcheck.List(r.Context(), string(scope.ProjectID), 50)
	if err != nil {
		d.internal(w, r, err)
		return
	}
	out := listOut(items, urlCheckOut)
	writeJSON(w, http.StatusOK, map[string]any{"items": out.Items, "nextCursor": nil, "enabled": d.urlChecksOn})
}

func (d *dashboard) urlCheck(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	c, err := d.urlcheck.Get(r.Context(), string(scope.ProjectID), r.PathValue("checkId"))
	if errors.Is(err, urlcheck.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "URL check not found", nil)
		return
	}
	if err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, urlCheckOut(c))
}

func (d *dashboard) urlCheckScreenshot(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	b, err := d.urlcheck.Screenshot(r.Context(), string(scope.ProjectID), r.PathValue("checkId"), r.PathValue("which"))
	if errors.Is(err, urlcheck.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "screenshot not found", nil)
		return
	}
	if err != nil {
		d.internal(w, r, err)
		return
	}
	// Only JPEGs are ever stored (urlcheck.jpegOrNil); the page that was
	// photographed is untrusted, its picture is not script.
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(b)
}
