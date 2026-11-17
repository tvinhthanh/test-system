package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/triage"
)

func (d *dashboard) requestInvestigation(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionTriage)
	if !ok {
		return
	}
	in, created, err := d.triage.RequestInvestigation(r.Context(), triage.NewInvestigation{ProjectID: string(scope.ProjectID), OrgID: string(scope.OrgID), ResultID: r.PathValue("resultId"), Actor: p.UserID})
	if err != nil {
		d.investigationErr(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusAccepted
	}
	writeJSON(w, status, in)
}

func (d *dashboard) listInvestigations(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	items, err := d.triage.ListInvestigations(r.Context(), string(scope.ProjectID), r.PathValue("resultId"), limitParam(r, 20))
	if err != nil {
		d.investigationErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (d *dashboard) getInvestigation(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	in, err := d.triage.GetInvestigation(r.Context(), string(scope.ProjectID), r.PathValue("resultId"), r.PathValue("investigationId"))
	if err != nil {
		d.investigationErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, in)
}

type decisionRequest struct {
	Decision string          `json:"decision"`
	Category triage.Category `json:"category"`
	Note     string          `json:"note"`
}

func (d *dashboard) decideHypothesis(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionTriage)
	if !ok {
		return
	}
	var req decisionRequest
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "malformed JSON", nil)
		return
	}
	ev, err := d.triage.DecideHypothesis(r.Context(), triage.Decision{ProjectID: string(scope.ProjectID), ResultID: r.PathValue("resultId"), HypothesisID: r.PathValue("hypothesisId"), Actor: p.UserID, Decision: req.Decision, Category: req.Category, Note: req.Note})
	if err != nil {
		d.investigationErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, ev)
}

func (d *dashboard) investigationErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, triage.ErrNotConfigured):
		httpx.WriteError(w, http.StatusConflict, "AI_NOT_CONFIGURED", "configure an AI provider under Team → AI settings", nil)
	case errors.Is(err, triage.ErrDailyLimit):
		httpx.WriteError(w, http.StatusTooManyRequests, "AI_DAILY_LIMIT", "the organization reached its daily investigation limit", nil)
	case errors.Is(err, triage.ErrDecided):
		httpx.WriteError(w, http.StatusConflict, "HYPOTHESIS_DECIDED", "this hypothesis already has a decision", nil)
	case errors.Is(err, triage.ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error(), nil)
	case errors.Is(err, triage.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "INVESTIGATION_NOT_FOUND", "investigation or result not found", nil)
	default:
		d.internal(w, r, err)
	}
}
