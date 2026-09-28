package main

import (
	"net/http"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/ingest"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

// JUnit XML backfill from the dashboard (§6.8.5, QĐ-13): a person uploads
// one report and it becomes one historic run — the same ingest.ImportJUnit
// the token route (POST /v1/imports/junit) runs. Writing a project's run
// history is configuring the project, so it takes project.configure
// (maintainer and up, §11.3.2); a member triages runs, never makes them.
//
//	POST /v1/projects/{projectId}/imports/junit?environment=&framework=&branch=&commit=&startedAt=   application/xml body

func (d *dashboard) importRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("POST /v1/projects/{projectId}/imports/junit", d.importJUnit)
}

func (d *dashboard) importJUnit(w http.ResponseWriter, r *http.Request) {
	project, actor, ok := d.writer(w, r, authz.ActionProjectConfigure)
	if !ok {
		return
	}
	in, err := ingest.JUnitOptions(r.URL.Query())
	if err != nil {
		ingest.WriteJUnitError(w, err)
		return
	}
	body, ok := ingest.ReadJUnitBody(w, r)
	if !ok {
		return
	}
	in.Source = "user:" + actor
	res, err := d.imports.ImportJUnit(r.Context(), tenancy.ProjectID(project), in, body)
	if err != nil {
		if !ingest.WriteJUnitError(w, err) {
			d.internal(w, r, err)
		}
		return
	}
	status := http.StatusCreated
	if !res.Created {
		status = http.StatusOK
	}
	writeJSON(w, status, res)
}
