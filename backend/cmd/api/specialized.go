package main

import (
	"io"
	"net/http"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/testmgmt"
)

func (d *dashboard) specializedRoutes(handle func(string, http.HandlerFunc)) {
	handle("GET /v1/projects/{projectId}/specialized-results", d.tmSpecializedList)
	handle("POST /v1/projects/{projectId}/specialized-results", d.tmSpecializedCreate)
	handle("GET /v1/projects/{projectId}/specialized-results/{specializedId}", d.tmSpecializedGet)
	handle("GET /v1/projects/{projectId}/specialized-results/{specializedId}/original", d.tmSpecializedOriginal)
}
func (d *dashboard) tmSpecializedCreate(w http.ResponseWriter, r *http.Request) {
	project, actor, ok := d.writer(w, r, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	var in testmgmt.SpecializedInput
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	if decodeJSON(r, &in) != nil {
		httpx.WriteError(w, 400, "INVALID_PAYLOAD", "malformed or oversized ATP report v1", nil)
		return
	}
	out, err := d.tm.AddSpecializedResult(r.Context(), project, in, actor)
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, out)
}
func (d *dashboard) tmSpecializedList(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	out, err := d.tm.ListSpecializedResults(r.Context(), string(scope.ProjectID), testmgmt.SpecializedFilter{Kind: q.Get("kind"), Outcome: q.Get("outcome"), TestCaseID: q.Get("testCaseId"), Before: q.Get("cursor")})
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}
func (d *dashboard) tmSpecializedGet(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	out, err := d.tm.GetSpecializedResult(r.Context(), string(scope.ProjectID), r.PathValue("specializedId"))
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}
func (d *dashboard) tmSpecializedOriginal(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	text, err := d.tm.SpecializedOriginal(r.Context(), string(scope.ProjectID), r.PathValue("specializedId"))
	if err != nil {
		d.tmErr(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="original-report.txt"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	io.WriteString(w, text)
}
