package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

// Creating a project from the dashboard (§11.3.2 "Tạo/xóa project" — the
// create half): workspace admins and org admins/owners, in a workspace they
// administer. Until now only cmd/admin could. Deleting a project is not
// here: it reaches into every module's data and gets its own design.
//
//	POST /v1/workspaces/{workspaceId}/projects {name, defaultBranch?}

func (d *dashboard) projectCreateRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("POST /v1/workspaces/{workspaceId}/projects", d.createProject)
}

func (d *dashboard) createProject(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	ws, err := d.tenancy.GetWorkspace(r.Context(), tenancy.WorkspaceID(r.PathValue("workspaceId")))
	if err != nil {
		d.nodeErr(w, r, err, "WORKSPACE_NOT_FOUND", "workspace not found")
		return
	}
	if !d.authorizeNode(w, r, p, authz.Resource{OrgID: string(ws.OrgID), WorkspaceID: string(ws.ID)}, authz.ActionProjectManage, "WORKSPACE_NOT_FOUND", "workspace not found") {
		return
	}
	var req struct {
		Name          string `json:"name"`
		DefaultBranch string `json:"defaultBranch"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := decodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "malformed JSON", nil)
		return
	}
	project, err := d.tenancy.CreateProject(r.Context(), ws.ID, strings.TrimSpace(req.Name), strings.TrimSpace(req.DefaultBranch))
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
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": string(project.ID), "name": project.Name, "workspaceName": ws.Name})
}
