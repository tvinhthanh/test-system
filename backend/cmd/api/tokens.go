package main

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
)

// Ingest tokens from the dashboard (§11.3.2 "Tạo/thu hồi ingest token",
// §11.4): a maintainer and up issues a project token for CI and revokes it,
// without an operator running cmd/admin. The secret is shown once, in the
// answer to the POST (every /v1 answer is no-store); only the prefix is ever
// listed. A revoke is audited with the session user (§11.7).
//
//	GET    /v1/projects/{projectId}/tokens
//	POST   /v1/projects/{projectId}/tokens {name, permissions?, expiresInDays?}
//	DELETE /v1/projects/{projectId}/tokens/{tokenId}

func (d *dashboard) tokenRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("GET /v1/projects/{projectId}/tokens", d.tokens)
	handle("POST /v1/projects/{projectId}/tokens", d.issueToken)
	handle("DELETE /v1/projects/{projectId}/tokens/{tokenId}", d.revokeToken)
}

type tokenDTO struct {
	ID          string     `json:"id"`
	Prefix      string     `json:"prefix"`
	Name        string     `json:"name"`
	Permissions []string   `json:"permissions"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt"`
	ExpiresAt   time.Time  `json:"expiresAt"`
	RevokedAt   *time.Time `json:"revokedAt"`
}

func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

func tokenOut(t authz.Token) tokenDTO {
	perms := make([]string, 0, len(t.Permissions))
	for _, p := range t.Permissions {
		perms = append(perms, string(p))
	}
	// The prefix as the secret starts, so it can be matched in a CI settings page.
	return tokenDTO{ID: t.ID, Prefix: authz.TokenPrefix + t.Prefix, Name: t.Name, Permissions: perms, CreatedAt: t.CreatedAt.UTC(),
		LastUsedAt: timeOrNil(t.LastUsedAt), ExpiresAt: t.ExpiresAt.UTC(), RevokedAt: timeOrNil(t.RevokedAt)}
}

func (d *dashboard) projectTokens(w http.ResponseWriter, r *http.Request, project string) ([]authz.Token, bool) {
	list, err := d.authz.ListTokens(r.Context(), authz.ScopeRef{Level: authz.LevelProject, ID: project})
	if err != nil {
		d.internal(w, r, err)
		return nil, false
	}
	return list, true
}

func (d *dashboard) tokens(w http.ResponseWriter, r *http.Request) {
	project, _, ok := d.writer(w, r, authz.ActionTokenManage)
	if !ok {
		return
	}
	list, ok := d.projectTokens(w, r, project)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, listOut(list, tokenOut))
}

func (d *dashboard) issueToken(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionTokenManage)
	if !ok {
		return
	}
	var req struct {
		Name          string   `json:"name"`
		Permissions   []string `json:"permissions"`
		ExpiresInDays int      `json:"expiresInDays"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := decodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "malformed JSON", nil)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "name: 1 to 100 characters, e.g. the CI pipeline it is for", map[string]any{"field": "name"})
		return
	}
	perms := []authz.Permission{authz.PermProjectIngest}
	if len(req.Permissions) > 0 {
		perms = perms[:0]
		for _, s := range req.Permissions {
			perm := authz.Permission(s)
			if perm != authz.PermProjectIngest && perm != authz.PermProjectRead && perm != authz.PermProjectAutorun {
				httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "permissions: project:ingest, project:read and/or project:autorun", map[string]any{"field": "permissions"})
				return
			}
			perms = append(perms, perm)
		}
	}
	spec := authz.TokenSpec{
		Scope:       authz.ScopeRef{Level: authz.LevelProject, ID: string(scope.ProjectID)},
		Permissions: perms, Name: name, CreatedBy: p.UserID,
	}
	if req.ExpiresInDays != 0 {
		if req.ExpiresInDays < 1 || req.ExpiresInDays > 365 {
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "expiresInDays: 1 to 365 (default 365, §11.4)", map[string]any{"field": "expiresInDays"})
			return
		}
		spec.ExpiresAt = time.Now().UTC().AddDate(0, 0, req.ExpiresInDays)
	}
	issued, err := d.authz.IssueToken(r.Context(), spec)
	if err != nil {
		if errors.Is(err, authz.ErrInvalid) {
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error(), nil)
			return
		}
		d.internal(w, r, err)
		return
	}
	// The one time the secret leaves the server.
	writeJSON(w, http.StatusCreated, map[string]any{"token": tokenOut(issued.Token), "secret": issued.Plaintext})
}

func (d *dashboard) revokeToken(w http.ResponseWriter, r *http.Request) {
	project, actor, ok := d.writer(w, r, authz.ActionTokenManage)
	if !ok {
		return
	}
	list, ok := d.projectTokens(w, r, project)
	if !ok {
		return
	}
	id := r.PathValue("tokenId")
	found := false
	for _, t := range list {
		if t.ID == id {
			found = true
			break
		}
	}
	// A token of another project (or none) is not here to revoke.
	if !found {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "no token with that id in this project", nil)
		return
	}
	switch err := d.authz.RevokeToken(r.Context(), id, actor); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, authz.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "no token with that id in this project", nil)
	default:
		d.internal(w, r, err)
	}
}
