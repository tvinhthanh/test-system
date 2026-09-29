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

// Deleting a project (§11.3.2 "Tạo/xóa project", the delete half) is soft:
// the project disappears at once — every route reaches it through tenancy,
// which no longer returns it, so the dashboard and its CI tokens both answer
// 404 — and it can be restored from its workspace for projectRestoreWindow.
// Then cmd/worker purges it, bucket included. Deleting asks for the
// project's name back, so a slip of the mouse deletes nothing. Delete,
// restore and purge are audited (§11.7).
//
//	DELETE /v1/projects/{projectId}                                   {confirmName}
//	GET    /v1/workspaces/{workspaceId}/projects/deleted
//	POST   /v1/workspaces/{workspaceId}/projects/{projectId}/restore

const projectRestoreWindow = 30 * 24 * time.Hour

func (d *dashboard) projectDeleteRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("DELETE /v1/projects/{projectId}", d.deleteProject)
	handle("GET /v1/workspaces/{workspaceId}/projects/deleted", d.deletedProjects)
	handle("POST /v1/workspaces/{workspaceId}/projects/{projectId}/restore", d.restoreProject)
}

type deletedProjectDTO struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	DeletedAt  time.Time `json:"deletedAt"`
	DeletedBy  string    `json:"deletedBy"`
	PurgeAfter time.Time `json:"purgeAfter"`
}

func deletedOut(p tenancy.DeletedProject) deletedProjectDTO {
	return deletedProjectDTO{ID: string(p.ID), Name: p.Name, DeletedAt: p.DeletedAt, DeletedBy: p.DeletedBy, PurgeAfter: p.PurgeAfter}
}

func (d *dashboard) deleteProject(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionProjectManage)
	if !ok {
		return
	}
	project, err := d.tenancy.GetProject(r.Context(), scope.ProjectID)
	if err != nil {
		d.nodeErr(w, r, err, "PROJECT_NOT_FOUND", "project not found")
		return
	}
	var req struct {
		ConfirmName string `json:"confirmName"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.ConfirmName) != project.Name {
		httpx.WriteError(w, http.StatusBadRequest, "CONFIRM_NAME", "type the project's name to delete it", map[string]any{"field": "confirmName"})
		return
	}
	actor := d.actor(r, p)
	gone, err := d.tenancy.DeleteProject(r.Context(), scope.ProjectID, actor, projectRestoreWindow)
	if err != nil {
		d.nodeErr(w, r, err, "PROJECT_NOT_FOUND", "project not found")
		return
	}
	if err := d.authz.AuditProject(r.Context(), "project.delete", string(gone.ID), string(gone.WorkspaceID), actor,
		map[string]any{"name": gone.Name, "purge_after": gone.PurgeAfter}); err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, deletedOut(gone))
}

// workspaceAdmin is project.manage on the workspace in the path.
func (d *dashboard) workspaceAdmin(w http.ResponseWriter, r *http.Request) (tenancy.Workspace, authz.Principal, bool) {
	p, ok := d.user(w, r)
	if !ok {
		return tenancy.Workspace{}, p, false
	}
	ws, err := d.tenancy.GetWorkspace(r.Context(), tenancy.WorkspaceID(r.PathValue("workspaceId")))
	if err != nil {
		d.nodeErr(w, r, err, "WORKSPACE_NOT_FOUND", "workspace not found")
		return tenancy.Workspace{}, p, false
	}
	ok = d.authorizeNode(w, r, p, authz.Resource{OrgID: string(ws.OrgID), WorkspaceID: string(ws.ID)}, authz.ActionProjectManage, "WORKSPACE_NOT_FOUND", "workspace not found")
	return ws, p, ok
}

func (d *dashboard) deletedProjects(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := d.workspaceAdmin(w, r)
	if !ok {
		return
	}
	list, err := d.tenancy.DeletedProjects(r.Context(), ws.ID)
	if err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listOut(list, deletedOut))
}

func (d *dashboard) restoreProject(w http.ResponseWriter, r *http.Request) {
	ws, p, ok := d.workspaceAdmin(w, r)
	if !ok {
		return
	}
	list, err := d.tenancy.DeletedProjects(r.Context(), ws.ID)
	if err != nil {
		d.internal(w, r, err)
		return
	}
	id := r.PathValue("projectId")
	found := false
	for _, x := range list {
		found = found || string(x.ID) == id
	}
	if !found { // not deleted, purged already, or another workspace's
		httpx.WriteError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "no deleted project with that id in this workspace", nil)
		return
	}
	project, err := d.tenancy.RestoreProject(r.Context(), tenancy.ProjectID(id))
	switch {
	case errors.Is(err, tenancy.ErrConflict):
		httpx.WriteError(w, http.StatusConflict, "CONFLICT", "another project in this workspace has that name now; rename it first", nil)
		return
	case errors.Is(err, tenancy.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "no deleted project with that id in this workspace", nil)
		return
	case err != nil:
		d.internal(w, r, err)
		return
	}
	if err := d.authz.AuditProject(r.Context(), "project.restore", id, string(ws.ID), d.actor(r, p), map[string]any{"name": project.Name}); err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": string(project.ID), "name": project.Name, "workspaceName": ws.Name})
}
