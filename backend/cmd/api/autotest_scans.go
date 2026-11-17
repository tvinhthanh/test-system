package main

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/autotest"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/urlcheck"
)

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
