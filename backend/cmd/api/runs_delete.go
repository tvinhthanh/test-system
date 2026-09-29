package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
)

// Deleting a run (§11.3.2 "Xóa dữ liệu run/artifact": owner, org admin,
// maintainer — not workspace admin). Soft, like a project: the run leaves
// every list, trend, flaky rate, ROI figure and release gate at once, and
// comes back with Restore until runRestoreWindow ends; then cmd/worker
// purges it, its artifacts' bytes included. Flaky signals of its tests are
// rebuilt on the next sweep either way. Audited (§11.7).
//
//	DELETE /v1/projects/{projectId}/runs/{runId}
//	GET    /v1/projects/{projectId}/runs/deleted
//	POST   /v1/projects/{projectId}/runs/{runId}/restore

const runRestoreWindow = 30 * 24 * time.Hour

func (d *dashboard) runDeleteRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("DELETE /v1/projects/{projectId}/runs/{runId}", d.deleteRun)
	handle("GET /v1/projects/{projectId}/runs/deleted", d.deletedRuns)
	handle("POST /v1/projects/{projectId}/runs/{runId}/restore", d.restoreRun)
}

type deletedRunDTO struct {
	ID         string    `json:"id"`
	Branch     string    `json:"branch"`
	CommitSHA  string    `json:"commitSha"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"startedAt"`
	DeletedAt  time.Time `json:"deletedAt"`
	DeletedBy  string    `json:"deletedBy"`
	PurgeAfter time.Time `json:"purgeAfter"`
}

func deletedRunOut(r results.DeletedRun) deletedRunDTO {
	return deletedRunDTO{ID: string(r.Run.ID), Branch: r.Run.Branch, CommitSHA: r.Run.CommitSHA, Status: string(r.Run.Status),
		StartedAt: r.Run.StartedAt, DeletedAt: r.DeletedAt, DeletedBy: r.DeletedBy, PurgeAfter: r.PurgeAfter}
}

func (d *dashboard) runErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, results.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "RUN_NOT_FOUND", "run not found", nil)
	case errors.Is(err, results.ErrRunOpen):
		httpx.WriteError(w, http.StatusConflict, "RUN_OPEN", "the run is still receiving results; delete it once it has finished", nil)
	default:
		d.internal(w, r, err)
	}
}

func (d *dashboard) deleteRun(w http.ResponseWriter, r *http.Request) {
	project, actor, ok := d.writer(w, r, authz.ActionDataDelete)
	if !ok {
		return
	}
	gone, err := d.runStore.DeleteRun(r.Context(), project, results.RunID(r.PathValue("runId")), actor, runRestoreWindow)
	if err != nil {
		d.runErr(w, r, err)
		return
	}
	if err := d.flaky.Invalidate(r.Context(), project, gone.StableTestIDs); err != nil {
		d.internal(w, r, err)
		return
	}
	if err := d.authz.AuditRun(r.Context(), "run.delete", string(gone.Run.ID), project, actor,
		map[string]any{"branch": gone.Run.Branch, "started_at": gone.Run.StartedAt, "purge_after": gone.PurgeAfter}); err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, deletedRunOut(gone))
}

func (d *dashboard) deletedRuns(w http.ResponseWriter, r *http.Request) {
	project, _, ok := d.writer(w, r, authz.ActionDataDelete)
	if !ok {
		return
	}
	list, err := d.runStore.DeletedRuns(r.Context(), project)
	if err != nil {
		d.runErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listOut(list, deletedRunOut))
}

func (d *dashboard) restoreRun(w http.ResponseWriter, r *http.Request) {
	project, actor, ok := d.writer(w, r, authz.ActionDataDelete)
	if !ok {
		return
	}
	back, err := d.runStore.RestoreRun(r.Context(), project, results.RunID(r.PathValue("runId")))
	if err != nil {
		d.runErr(w, r, err)
		return
	}
	if err := d.flaky.Invalidate(r.Context(), project, back.StableTestIDs); err != nil {
		d.internal(w, r, err)
		return
	}
	if err := d.authz.AuditRun(r.Context(), "run.restore", string(back.Run.ID), project, actor, map[string]any{"branch": back.Run.Branch}); err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": string(back.Run.ID)})
}
