package main

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

// Member management from a project (§11.3.2 "Quản lý member (tầng tương
// ứng)"): who can reach the project, and the project-level roles —
// maintainer, member, viewer — granted or revoked by someone holding
// member.manage (workspace admin and up; a maintainer cannot). Roles held on
// the workspace or organization show in the list as inherited and are
// changed at their own level (cmd/admin until those levels get screens).
//
// There is no mailer and no self-signup yet (§11.2), so a person is added by
// the email of an existing verified account. The answer tells the manager
// whether that account exists — an enumeration only a workspace admin of
// some tenant can run, through the per-user rate limit; recorded in §11.6.
// The actor recorded is the session user's email.
//
//	GET    /v1/projects/{projectId}/members?limit&cursor  chain members, by email
//	POST   /v1/projects/{projectId}/members               {email, role}: grant or change
//	DELETE /v1/projects/{projectId}/members/{userId}      revoke the project-level role

func (d *dashboard) memberRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("GET /v1/projects/{projectId}/members", d.members)
	handle("POST /v1/projects/{projectId}/members", d.memberSet)
	handle("DELETE /v1/projects/{projectId}/members/{userId}", d.memberRevoke)
}

// projectRoles are the roles a project screen may grant (§11.3.1 "Gán ở tầng").
var projectRoles = map[authz.Role]bool{authz.RoleMaintainer: true, authz.RoleMember: true, authz.RoleViewer: true}

type memberDTO struct {
	UserID string     `json:"userId"`
	Email  string     `json:"email"`
	Role   authz.Role `json:"role"`
	// Level is where the role is held; anything but "project" is inherited
	// and cannot be changed from the project.
	Level     authz.Level `json:"level"`
	CreatedAt time.Time   `json:"createdAt"`
}

func memberOut(m authz.Member) memberDTO {
	return memberDTO{UserID: m.UserID, Email: m.Email, Role: m.Role, Level: m.Scope.Level, CreatedAt: m.CreatedAt}
}

func resourceOf(s tenancy.Scope) authz.Resource {
	return authz.Resource{OrgID: string(s.OrgID), WorkspaceID: string(s.WorkspaceID), ProjectID: string(s.ProjectID)}
}

// The cursor is project id, email and level of the last row, so a cursor
// from another project's list is refused rather than silently reused.
func memberCursor(project string, k authz.MemberKey) string {
	return base64.RawURLEncoding.EncodeToString([]byte(project + "\x00" + k.Email + "\x00" + string(k.Level)))
}

func parseMemberCursor(project, c string) (authz.MemberKey, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	parts := strings.Split(string(raw), "\x00")
	if err != nil || len(parts) != 3 || parts[0] != project || parts[1] == "" {
		return authz.MemberKey{}, false
	}
	return authz.MemberKey{Email: parts[1], Level: authz.Level(parts[2])}, true
}

func (d *dashboard) members(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionMemberManage)
	if !ok {
		return
	}
	var after authz.MemberKey
	if c := r.URL.Query().Get("cursor"); c != "" {
		if after, ok = parseMemberCursor(string(scope.ProjectID), c); !ok {
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_CURSOR", "cursor is not valid for this list; restart from the first page", nil)
			return
		}
	}
	limit := limitParam(r, 50)
	if limit > 100 {
		limit = 100
	}
	// One extra row says whether another page exists.
	ms, err := d.authz.Members(r.Context(), resourceOf(scope), after, limit+1)
	if errors.Is(err, authz.ErrInvalid) {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_CURSOR", "cursor is not valid for this list; restart from the first page", nil)
		return
	}
	if err != nil {
		d.internal(w, r, err)
		return
	}
	out := page{}
	if len(ms) > limit {
		ms = ms[:limit]
		last := ms[len(ms)-1]
		c := memberCursor(string(scope.ProjectID), authz.MemberKey{Email: last.Email, Level: last.Scope.Level})
		out.NextCursor = &c
	}
	items := make([]memberDTO, 0, len(ms))
	for _, m := range ms {
		items = append(items, memberOut(m))
	}
	out.Items = items
	writeJSON(w, http.StatusOK, out)
}

func (d *dashboard) memberSet(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionMemberManage)
	if !ok {
		return
	}
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	role := authz.Role(req.Role)
	if !projectRoles[role] {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "role must be maintainer, member or viewer", map[string]any{"field": "role"})
		return
	}
	email := strings.TrimSpace(req.Email)
	if email == "" || len(email) > 254 {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "email is required", map[string]any{"field": "email"})
		return
	}
	// An unverified account may not join an org (§11.2); to the manager it
	// is the same as no account.
	u, err := d.authz.GetUserByEmail(r.Context(), email)
	if errors.Is(err, authz.ErrNotFound) || (err == nil && u.EmailVerifiedAt.IsZero()) {
		httpx.WriteError(w, http.StatusNotFound, "USER_NOT_FOUND", "no verified account uses that email", map[string]any{"field": "email"})
		return
	}
	if err != nil {
		d.internal(w, r, err)
		return
	}
	m, err := d.authz.SetRole(r.Context(), u.ID, authz.ScopeRef{Level: authz.LevelProject, ID: string(scope.ProjectID)}, role, d.actor(r, p))
	if err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, memberOut(authz.Member{UserID: u.ID, Email: u.Email, Role: m.Role, Scope: m.Scope, CreatedAt: m.CreatedAt}))
}

func (d *dashboard) memberRevoke(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionMemberManage)
	if !ok {
		return
	}
	// Scoped to this project's node: another tenant's user id, or a role
	// held only on the workspace/org, is simply not a row here.
	err := d.authz.RevokeRole(r.Context(), r.PathValue("userId"), authz.ScopeRef{Level: authz.LevelProject, ID: string(scope.ProjectID)}, d.actor(r, p))
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, authz.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "no project-level role for that user here", nil)
	default:
		d.internal(w, r, err)
	}
}
