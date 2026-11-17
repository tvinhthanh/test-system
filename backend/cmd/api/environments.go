package main

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

// A project's environments with the address of the system under test there
// (QĐ-14): testers name the site a plan is for, and a URL check starts from
// it. Reporters still create environments by key on first sight (§7.3);
// these routes add one ahead of time or give one its address. Anyone in the
// project reads them; members and up — the people who write plans — add and
// change them. Every change is audited as project.configure.
//
//	GET   /v1/projects/{projectId}/environments
//	POST  /v1/projects/{projectId}/environments {key, baseUrl?}
//	PATCH /v1/projects/{projectId}/environments/{environmentId} {baseUrl}  ("" clears)

func (d *dashboard) environmentRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("GET /v1/projects/{projectId}/environments", d.environments)
	handle("POST /v1/projects/{projectId}/environments", d.createEnvironment)
	handle("PATCH /v1/projects/{projectId}/environments/{environmentId}", d.patchEnvironment)
}

type environmentDTO struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	BaseURL   string    `json:"baseUrl"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func environmentOut(e tenancy.Environment) environmentDTO {
	return environmentDTO{ID: string(e.ID), Key: e.Key, BaseURL: e.BaseURL, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
}

// projectEnvironments maps a project's environments by id — for plans,
// which name theirs by id, and for checking that an id is the project's.
func (d *dashboard) projectEnvironments(r *http.Request, project string) (map[string]environmentDTO, error) {
	envs, err := d.tenancy.ListEnvironments(r.Context(), tenancy.ProjectID(project))
	if err != nil {
		return nil, err
	}
	out := make(map[string]environmentDTO, len(envs))
	for _, e := range envs {
		out[string(e.ID)] = environmentOut(e)
	}
	return out, nil
}

func (d *dashboard) environments(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	envs, err := d.tenancy.ListEnvironments(r.Context(), scope.ProjectID)
	if err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listOut(envs, environmentOut))
}

func (d *dashboard) createEnvironment(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	var req struct {
		Key     string `json:"key"`
		BaseURL string `json:"baseUrl"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	e, err := d.tenancy.CreateEnvironment(r.Context(), scope.ProjectID, strings.TrimSpace(req.Key), req.BaseURL)
	if !d.environmentErr(w, r, err) {
		return
	}
	if err := d.authz.AuditProject(r.Context(), "project.configure", string(scope.ProjectID), string(scope.WorkspaceID), d.actor(r, p),
		map[string]any{"environment": e.Key, "baseUrl": []string{"", e.BaseURL}}); err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, environmentOut(e))
}

func (d *dashboard) patchEnvironment(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	var req struct {
		BaseURL *string `json:"baseUrl"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	if req.BaseURL == nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "baseUrl is required (\"\" clears it)", map[string]any{"field": "baseUrl"})
		return
	}
	envs, err := d.projectEnvironments(r, string(scope.ProjectID))
	if err != nil {
		d.internal(w, r, err)
		return
	}
	before, found := envs[r.PathValue("environmentId")]
	if !found {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "environment not found", nil)
		return
	}
	e, err := d.tenancy.SetEnvironmentURL(r.Context(), scope.ProjectID, tenancy.EnvironmentID(before.ID), *req.BaseURL)
	if !d.environmentErr(w, r, err) {
		return
	}
	if e.BaseURL != before.BaseURL {
		if err := d.authz.AuditProject(r.Context(), "project.configure", string(scope.ProjectID), string(scope.WorkspaceID), d.actor(r, p),
			map[string]any{"environment": e.Key, "baseUrl": []string{before.BaseURL, e.BaseURL}}); err != nil {
			d.internal(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, environmentOut(e))
}

// environmentErr writes the answer for a failed environment write and
// reports whether the write succeeded.
func (d *dashboard) environmentErr(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, tenancy.ErrInvalid):
		field := "baseUrl"
		if strings.Contains(err.Error(), "environment key") {
			field = "key"
		}
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", strings.TrimPrefix(err.Error(), "tenancy: invalid input: "), map[string]any{"field": field})
	case errors.Is(err, tenancy.ErrConflict):
		httpx.WriteError(w, http.StatusConflict, "CONFLICT", "an environment with that key already exists in this project", map[string]any{"field": "key"})
	case errors.Is(err, tenancy.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "environment not found", nil)
	default:
		d.internal(w, r, err)
	}
	return false
}
