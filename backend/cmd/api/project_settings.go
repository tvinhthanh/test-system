package main

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/artifact"
	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

// A project's settings (§11.3.2 "Sửa retention/cấu hình project"): its
// name, default branch, and how long each kind of artifact is kept. Anyone
// in the project reads them; maintainers and up change them. Retention can
// only be shortened below the organization's plan (§10.4) — the plan is
// what is paid for, so a longer number is a 400 — and a change applies to
// artifacts recorded from then on. Every change is audited as
// project.configure with old → new.
//
//	GET   /v1/projects/{projectId}/settings
//	PATCH /v1/projects/{projectId}/settings {name?, defaultBranch?, retention?: {kind: days, 0 = plan's}}

func (d *dashboard) projectSettingsRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("GET /v1/projects/{projectId}/settings", d.projectSettings)
	handle("PATCH /v1/projects/{projectId}/settings", d.patchProjectSettings)
}

type retentionDTO struct {
	Kind      string     `json:"kind"`
	PlanDays  int        `json:"planDays"`
	Days      int        `json:"days"`
	Override  bool       `json:"override"`
	UpdatedBy string     `json:"updatedBy,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

type projectSettingsDTO struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	DefaultBranch string         `json:"defaultBranch"`
	Plan          string         `json:"plan"`
	Retention     []retentionDTO `json:"retention"`
}

func retentionOut(r artifact.Retention) retentionDTO {
	out := retentionDTO{Kind: string(r.Kind), PlanDays: r.PlanDays, Days: r.Days, Override: r.Override, UpdatedBy: r.UpdatedBy}
	if r.Override {
		at := r.UpdatedAt
		out.UpdatedAt = &at
	}
	return out
}

// settingsOf reads the settings of a project the caller is allowed on.
func (d *dashboard) settingsOf(r *http.Request, scope tenancy.Scope) (projectSettingsDTO, error) {
	project, err := d.tenancy.GetProject(r.Context(), scope.ProjectID)
	if err != nil {
		return projectSettingsDTO{}, err
	}
	org, err := d.tenancy.GetOrganization(r.Context(), scope.OrgID)
	if err != nil {
		return projectSettingsDTO{}, err
	}
	ret, err := d.artifact.Retention(r.Context(), string(project.ID), org.PlanID)
	if err != nil {
		return projectSettingsDTO{}, err
	}
	out := projectSettingsDTO{ID: string(project.ID), Name: project.Name, DefaultBranch: project.DefaultBranch, Plan: org.PlanID}
	for _, x := range ret {
		out.Retention = append(out.Retention, retentionOut(x))
	}
	return out, nil
}

func (d *dashboard) projectSettings(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionView)
	if !ok {
		return
	}
	out, err := d.settingsOf(r, scope)
	if err != nil {
		d.nodeErr(w, r, err, "PROJECT_NOT_FOUND", "project not found")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (d *dashboard) patchProjectSettings(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionProjectConfigure)
	if !ok {
		return
	}
	var req struct {
		Name          *string        `json:"name"`
		DefaultBranch *string        `json:"defaultBranch"`
		Retention     map[string]int `json:"retention"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := decodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "malformed JSON", nil)
		return
	}
	before, err := d.settingsOf(r, scope)
	if err != nil {
		d.nodeErr(w, r, err, "PROJECT_NOT_FOUND", "project not found")
		return
	}
	name, branch := "", ""
	if req.Name != nil {
		if name = strings.TrimSpace(*req.Name); name == "" {
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "name cannot be empty", map[string]any{"field": "name"})
			return
		}
	}
	if req.DefaultBranch != nil {
		if branch = strings.TrimSpace(*req.DefaultBranch); branch == "" {
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "defaultBranch cannot be empty", map[string]any{"field": "defaultBranch"})
			return
		}
	}
	// Every retention value is checked before anything is written, so a
	// rename never lands with a refused retention.
	plan := map[string]int{}
	for _, x := range before.Retention {
		plan[x.Kind] = x.PlanDays
	}
	days := map[artifact.Kind]int{}
	for k, v := range req.Retention {
		limit, known := plan[k]
		switch {
		case !known:
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "unknown artifact kind "+k, map[string]any{"field": "retention." + k})
			return
		case v < 0:
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "retention is at least 1 day, or 0 for the plan's", map[string]any{"field": "retention." + k})
			return
		case v > limit:
			httpx.WriteError(w, http.StatusBadRequest, "RETENTION_ABOVE_PLAN", "the "+before.Plan+" plan keeps "+k+" at most this many days",
				map[string]any{"field": "retention." + k, "kind": k, "planDays": limit, "days": v})
			return
		}
		days[artifact.Kind(k)] = v
	}
	actor := d.actor(r, p)
	details := map[string]any{}
	if (name != "" && name != before.Name) || (branch != "" && branch != before.DefaultBranch) {
		after, err := d.tenancy.UpdateProject(r.Context(), scope.ProjectID, name, branch)
		switch {
		case errors.Is(err, tenancy.ErrInvalid):
			field := "name"
			if strings.Contains(err.Error(), "branch") {
				field = "defaultBranch"
			}
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid "+strings.TrimPrefix(err.Error(), "tenancy: invalid input: "), map[string]any{"field": field})
			return
		case errors.Is(err, tenancy.ErrConflict):
			httpx.WriteError(w, http.StatusConflict, "CONFLICT", "a project with that name already exists in this workspace", map[string]any{"field": "name"})
			return
		case err != nil:
			d.nodeErr(w, r, err, "PROJECT_NOT_FOUND", "project not found")
			return
		}
		if after.Name != before.Name {
			details["name"] = []string{before.Name, after.Name}
		}
		if after.DefaultBranch != before.DefaultBranch {
			details["defaultBranch"] = []string{before.DefaultBranch, after.DefaultBranch}
		}
	}
	if len(days) > 0 {
		changed, err := d.artifact.SetRetention(r.Context(), string(scope.ProjectID), before.Plan, days, actor)
		if err != nil {
			d.internal(w, r, err)
			return
		}
		if len(changed) > 0 {
			ret := map[string][2]int{}
			for k, v := range changed {
				ret[string(k)] = v
			}
			details["retention"] = ret
		}
	}
	if len(details) > 0 {
		if err := d.authz.AuditProject(r.Context(), "project.configure", string(scope.ProjectID), string(scope.WorkspaceID), actor, details); err != nil {
			d.internal(w, r, err)
			return
		}
	}
	out, err := d.settingsOf(r, scope)
	if err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
