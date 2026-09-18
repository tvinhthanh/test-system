package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
)

// The manual identity tools of §9.4 ("bắt buộc có ở P1"), over HTTP for the
// dashboard — until 18-09-2026 only cmd/admin drove them (§4.4 P1-min: CSV +
// script). Same use cases, same audit rows (identity_audit), same three
// distinct operations (N43): Merge folds one identity into another, UndoMerge
// puts a wrong merge back exactly, SplitFrom says "this test diverged at t".
// Reading the queue takes triage rights; every change takes identity.merge
// (admin in the three-role P1-min matrix). The actor recorded is the
// session user's email, never anything the body claims.
//
//	GET  /v1/projects/{projectId}/review                          pending pairs, oldest first (≤ 100)
//	POST /v1/projects/{projectId}/review/{itemId}                 {decision: merged|kept}
//	GET  /v1/projects/{projectId}/identities/{identityId}/aliases fingerprints in force and historical
//	POST /v1/projects/{projectId}/identities/{identityId}/merge   {into}: fold this identity into another
//	POST /v1/projects/{projectId}/identities/{identityId}/undo-merge
//	POST /v1/projects/{projectId}/identities/{identityId}/split   {fingerprint, at} → {newIdentityId}

func (d *dashboard) identityOpsRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("GET /v1/projects/{projectId}/review", d.reviewList)
	handle("POST /v1/projects/{projectId}/review/{itemId}", d.reviewDecide)
	handle("GET /v1/projects/{projectId}/identities/{identityId}/aliases", d.identityAliases)
	handle("POST /v1/projects/{projectId}/identities/{identityId}/merge", d.identityMerge)
	handle("POST /v1/projects/{projectId}/identities/{identityId}/undo-merge", d.identityUndoMerge)
	handle("POST /v1/projects/{projectId}/identities/{identityId}/split", d.identitySplit)
}

// identityRef is the little the UI needs to show one side of a pair.
type identityRef struct {
	ID                string `json:"id"`
	CanonicalTitle    string `json:"canonicalTitle"`
	CanonicalFilePath string `json:"canonicalFilePath"`
	Status            string `json:"status"` // active | merged | ""
}

type reviewItemDTO struct {
	ID            string             `json:"id"`
	CreatedAt     time.Time          `json:"createdAt"`
	Identity      identityRef        `json:"identity"`  // created at resolve time
	Candidate     identityRef        `json:"candidate"` // the suggested rename source
	Fingerprint   string             `json:"fingerprint"`
	Score         float64            `json:"score"`
	RunnerUpID    string             `json:"runnerUpId"`
	RunnerUpScore float64            `json:"runnerUpScore"`
	Gap           float64            `json:"gap"`
	Breakdown     map[string]float64 `json:"breakdown"`
	RunID         string             `json:"runId"`
}

// ref describes an identity for a pair; a missing or merged one still gets
// its id so the row can be shown and decided.
func (d *dashboard) ref(r *http.Request, projectID, id string) identityRef {
	out := identityRef{ID: id}
	if it, err := d.idents.Get(r.Context(), id); err == nil && it.ProjectID == projectID {
		out.Status = string(it.Status)
		out.CanonicalFilePath = it.CanonicalFilePath
		out.CanonicalTitle = strings.Join(it.CanonicalTitlePath, " › ")
	}
	return out
}

func (d *dashboard) reviewList(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionTriage)
	if !ok {
		return
	}
	items, err := d.idents.ListReview(r.Context(), string(scope.ProjectID), limitParam(r, 100))
	if err != nil {
		d.identityErr(w, r, err)
		return
	}
	out := make([]reviewItemDTO, 0, len(items))
	for _, it := range items {
		out = append(out, reviewItemDTO{
			ID: it.ID, CreatedAt: it.CreatedAt,
			Identity:    d.ref(r, it.ProjectID, it.IdentityID),
			Candidate:   d.ref(r, it.ProjectID, it.CandidateID),
			Fingerprint: it.Fingerprint, Score: it.Score, RunnerUpID: it.RunnerUpID, RunnerUpScore: it.RunnerUpScore,
			Gap: it.Gap, Breakdown: it.Breakdown, RunID: it.RunID,
		})
	}
	writeJSON(w, http.StatusOK, page{Items: out})
}

// actor is the session user's email, the name identity_audit records.
func (d *dashboard) actor(r *http.Request, p authz.Principal) string {
	if u, err := d.authz.GetUser(r.Context(), p.UserID); err == nil && u.Email != "" {
		return u.Email
	}
	return "user:" + p.UserID
}

func (d *dashboard) reviewDecide(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	var req struct {
		Decision string `json:"decision"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	decision := identity.ReviewStatus(req.Decision)
	if decision != identity.ReviewMerged && decision != identity.ReviewKept {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "decision must be merged or kept", map[string]any{"field": "decision"})
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionIdentityMerge)
	if !ok {
		return
	}
	// The item must belong to this project before its id reaches a service
	// that trusts it (§11.3.3: the tenant condition is in the lookup).
	item, err := d.idents.ReviewItem(r.Context(), string(scope.ProjectID), r.PathValue("itemId"))
	if err != nil {
		d.identityErr(w, r, err)
		return
	}
	actor := d.actor(r, p)
	if err := d.idents.DecideReview(r.Context(), item.ID, decision, actor); err != nil {
		d.identityErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": item.ID, "status": string(decision), "decidedBy": actor, "decidedAt": time.Now().UTC()})
}

type aliasDTO struct {
	ID          string     `json:"id"`
	Fingerprint string     `json:"fingerprint"`
	Source      string     `json:"source"`
	MatchScore  float64    `json:"matchScore"`
	CreatedBy   string     `json:"createdBy"`
	ValidFrom   time.Time  `json:"validFrom"`
	ValidTo     *time.Time `json:"validTo"` // null while in force
}

func (d *dashboard) identityAliases(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	id := r.PathValue("identityId")
	// Tenant check through the read model: an identity of another project
	// is a 404 here, so its aliases are never listed.
	if _, err := d.query.Identity(r.Context(), string(scope.ProjectID), id); err != nil {
		d.queryErr(w, r, err)
		return
	}
	aliases, err := d.idents.Aliases(r.Context(), id)
	if err != nil {
		d.identityErr(w, r, err)
		return
	}
	out := make([]aliasDTO, 0, len(aliases))
	for _, a := range aliases {
		dto := aliasDTO{ID: a.ID, Fingerprint: a.Fingerprint, Source: string(a.Source), MatchScore: a.MatchScore, CreatedBy: a.CreatedBy, ValidFrom: a.ValidFrom.UTC()}
		if !a.ValidTo.IsZero() {
			t := a.ValidTo.UTC()
			dto.ValidTo = &t
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, page{Items: out})
}

func (d *dashboard) identityMerge(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	var req struct {
		Into string `json:"into"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	if req.Into == "" {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "into (the identity to merge into) is required", map[string]any{"field": "into"})
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionIdentityMerge)
	if !ok {
		return
	}
	// dst = into, src = the identity in the path; both are looked up inside
	// the project by the service (lockIdentity), so a foreign id is a 404.
	if err := d.idents.Merge(r.Context(), string(scope.ProjectID), req.Into, r.PathValue("identityId"), d.actor(r, p)); err != nil {
		d.identityErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mergedInto": req.Into})
}

func (d *dashboard) identityUndoMerge(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionIdentityMerge)
	if !ok {
		return
	}
	if err := d.idents.UndoMerge(r.Context(), string(scope.ProjectID), r.PathValue("identityId"), d.actor(r, p)); err != nil {
		d.identityErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"restored": r.PathValue("identityId")})
}

func (d *dashboard) identitySplit(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	var req struct {
		Fingerprint string    `json:"fingerprint"`
		At          time.Time `json:"at"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	if req.Fingerprint == "" || req.At.IsZero() {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "fingerprint and at (RFC 3339) are required", nil)
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionIdentityMerge)
	if !ok {
		return
	}
	// The fingerprint must be an alias of the identity named in the path —
	// otherwise the path is decoration and a caller could split any alias
	// in the project by fingerprint alone.
	id := r.PathValue("identityId")
	if _, err := d.query.Identity(r.Context(), string(scope.ProjectID), id); err != nil {
		d.queryErr(w, r, err)
		return
	}
	aliases, err := d.idents.Aliases(r.Context(), id)
	if err != nil {
		d.identityErr(w, r, err)
		return
	}
	owns := false
	for _, a := range aliases {
		if a.Fingerprint == req.Fingerprint {
			owns = true
		}
	}
	if !owns {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "fingerprint is not an alias of this identity", map[string]any{"field": "fingerprint"})
		return
	}
	newID, err := d.idents.SplitFrom(r.Context(), string(scope.ProjectID), req.Fingerprint, req.At, d.actor(r, p))
	if err != nil {
		d.identityErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"newIdentityId": newID})
}

// decodeSmall reads a small JSON body or answers 400.
func decodeSmall(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "malformed JSON", nil)
		return false
	}
	return true
}

func (d *dashboard) identityErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "not found", nil)
	case errors.Is(err, identity.ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error(), nil)
	case errors.Is(err, identity.ErrConflict):
		httpx.WriteError(w, http.StatusConflict, "CONFLICT", err.Error(), nil)
	default:
		d.internal(w, r, err)
	}
}
