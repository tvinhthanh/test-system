package main

import (
	"encoding/base64"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

// Member management (§11.3.2 "Quản lý member (tầng tương ứng)") at each
// node of the tree: a project (maintainer/member/viewer), a workspace
// (workspace_admin) and an organization (org_admin/org_owner). Every route
// needs member.manage on the node — workspace admin and up; a maintainer
// cannot. The owner role is the owner's alone (§11.3.1 "quản lý owner"):
// granting it, or changing or removing someone who holds it, needs
// org.manage; and authz refuses to leave an organization without an owner.
// Roles held higher up show in each list as inherited and are changed at
// their own level.
//
// A person is added by the email of an existing verified account (an email
// with none is invited instead when MAIL=smtp). The answer tells the manager
// whether that account exists — an enumeration only an admin of some tenant
// can run, through the per-user rate limit; recorded in §11.6 API6. The
// actor recorded is the session user's email.
//
//	GET    /v1/member-scopes                                 organizations and workspaces the caller manages
//	GET    /v1/{projects|workspaces|organizations}/{id}/members?limit&cursor
//	POST   /v1/{projects|workspaces|organizations}/{id}/members   {email, role}
//	DELETE /v1/{projects|workspaces|organizations}/{id}/members/{userId}

func (d *dashboard) memberRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("GET /v1/member-scopes", d.memberScopes)
	for prefix, resolve := range map[string]nodeResolver{
		"/v1/projects/{projectId}":           d.projectNode,
		"/v1/workspaces/{workspaceId}":       d.workspaceNode,
		"/v1/organizations/{organizationId}": d.organizationNode,
	} {
		handle("GET "+prefix+"/members", d.members(resolve))
		handle("POST "+prefix+"/members", d.memberSet(resolve))
		handle("DELETE "+prefix+"/members/{userId}", d.memberRevoke(resolve))
	}
}

// node is one place in the tree whose members a screen manages.
type node struct {
	scope authz.ScopeRef  // where roles granted here are stored
	chain authz.Resource  // the ancestry, for Authorize and the list
	roles []authz.Role    // what may be granted here (§11.3.1 "Gán ở tầng")
	p     authz.Principal // the caller, already allowed member.manage
}

// nodeResolver finds the node in the path and authorizes member.manage on
// it: 404 when the caller has no standing in the tenant, 403 when the role
// is too low.
type nodeResolver func(w http.ResponseWriter, r *http.Request, p authz.Principal) (node, bool)

func (d *dashboard) projectNode(w http.ResponseWriter, r *http.Request, p authz.Principal) (node, bool) {
	scope, ok := d.project(w, r, p, authz.ActionMemberManage)
	if !ok {
		return node{}, false
	}
	return node{
		scope: authz.ScopeRef{Level: authz.LevelProject, ID: string(scope.ProjectID)},
		chain: resourceOf(scope),
		roles: []authz.Role{authz.RoleMaintainer, authz.RoleMember, authz.RoleViewer},
		p:     p,
	}, true
}

func (d *dashboard) workspaceNode(w http.ResponseWriter, r *http.Request, p authz.Principal) (node, bool) {
	ws, err := d.tenancy.GetWorkspace(r.Context(), tenancy.WorkspaceID(r.PathValue("workspaceId")))
	if err != nil {
		d.nodeErr(w, r, err, "WORKSPACE_NOT_FOUND", "workspace not found")
		return node{}, false
	}
	n := node{
		scope: authz.ScopeRef{Level: authz.LevelWorkspace, ID: string(ws.ID)},
		chain: authz.Resource{OrgID: string(ws.OrgID), WorkspaceID: string(ws.ID)},
		roles: []authz.Role{authz.RoleWorkspaceAdmin},
		p:     p,
	}
	return n, d.authorizeNode(w, r, p, n.chain, authz.ActionMemberManage, "WORKSPACE_NOT_FOUND", "workspace not found")
}

func (d *dashboard) organizationNode(w http.ResponseWriter, r *http.Request, p authz.Principal) (node, bool) {
	org, err := d.tenancy.GetOrganization(r.Context(), tenancy.OrganizationID(r.PathValue("organizationId")))
	if err != nil {
		d.nodeErr(w, r, err, "ORGANIZATION_NOT_FOUND", "organization not found")
		return node{}, false
	}
	n := node{
		scope: authz.ScopeRef{Level: authz.LevelOrganization, ID: string(org.ID)},
		chain: authz.Resource{OrgID: string(org.ID)},
		roles: []authz.Role{authz.RoleOrgAdmin, authz.RoleOrgOwner},
		p:     p,
	}
	return n, d.authorizeNode(w, r, p, n.chain, authz.ActionMemberManage, "ORGANIZATION_NOT_FOUND", "organization not found")
}

func (d *dashboard) nodeErr(w http.ResponseWriter, r *http.Request, err error, code, msg string) {
	if errors.Is(err, tenancy.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, code, msg, nil)
		return
	}
	d.internal(w, r, err)
}

// authorizeNode is d.project's tenant check for a workspace or an org.
func (d *dashboard) authorizeNode(w http.ResponseWriter, r *http.Request, p authz.Principal, res authz.Resource, a authz.Action, notFound, msg string) bool {
	switch err := d.authz.Authorize(r.Context(), p, res, a); {
	case err == nil:
		return true
	case errors.Is(err, authz.ErrNoAccess):
		httpx.WriteError(w, http.StatusNotFound, notFound, msg, nil)
	case errors.Is(err, authz.ErrForbidden):
		httpx.WriteError(w, http.StatusForbidden, "FORBIDDEN", "your role does not allow this", nil)
	default:
		d.internal(w, r, err)
	}
	return false
}

// ownerOnly: the owner role is managed by owners (§11.3.1). It applies when
// the request grants org_owner or touches someone who holds it at n.
func (d *dashboard) ownerOnly(w http.ResponseWriter, r *http.Request, n node, target string, grant authz.Role) bool {
	if n.scope.Level != authz.LevelOrganization {
		return true
	}
	touches := grant == authz.RoleOrgOwner
	if !touches && target != "" {
		held, err := d.authz.Roles(r.Context(), authz.Principal{Kind: authz.PrincipalUser, UserID: target}, n.chain)
		if err != nil {
			d.internal(w, r, err)
			return false
		}
		touches = slices.Contains(held, authz.RoleOrgOwner)
	}
	if !touches {
		return true
	}
	switch err := d.authz.Authorize(r.Context(), n.p, n.chain, authz.ActionOrgManage); {
	case err == nil:
		return true
	case errors.Is(err, authz.ErrForbidden), errors.Is(err, authz.ErrNoAccess):
		httpx.WriteError(w, http.StatusForbidden, "OWNER_ONLY", "only an organization owner can grant, change or remove the owner role", nil)
	default:
		d.internal(w, r, err)
	}
	return false
}

func resourceOf(s tenancy.Scope) authz.Resource {
	return authz.Resource{OrgID: string(s.OrgID), WorkspaceID: string(s.WorkspaceID), ProjectID: string(s.ProjectID)}
}

type memberDTO struct {
	UserID string     `json:"userId"`
	Email  string     `json:"email"`
	Role   authz.Role `json:"role"`
	// Level is where the role is held; anything but the node's own level is
	// inherited and cannot be changed from here.
	Level     authz.Level `json:"level"`
	CreatedAt time.Time   `json:"createdAt"`
}

func memberOut(m authz.Member) memberDTO {
	return memberDTO{UserID: m.UserID, Email: m.Email, Role: m.Role, Level: m.Scope.Level, CreatedAt: m.CreatedAt}
}

// The cursor is node id, email and level of the last row, so a cursor from
// another node's list is refused rather than silently reused.
func memberCursor(nodeID string, k authz.MemberKey) string {
	return base64.RawURLEncoding.EncodeToString([]byte(nodeID + "\x00" + k.Email + "\x00" + string(k.Level)))
}

func parseMemberCursor(nodeID, c string) (authz.MemberKey, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	parts := strings.Split(string(raw), "\x00")
	if err != nil || len(parts) != 3 || parts[0] != nodeID || parts[1] == "" {
		return authz.MemberKey{}, false
	}
	return authz.MemberKey{Email: parts[1], Level: authz.Level(parts[2])}, true
}

func (d *dashboard) members(resolve nodeResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := d.user(w, r)
		if !ok {
			return
		}
		n, ok := resolve(w, r, p)
		if !ok {
			return
		}
		var after authz.MemberKey
		if c := r.URL.Query().Get("cursor"); c != "" {
			if after, ok = parseMemberCursor(n.scope.ID, c); !ok {
				httpx.WriteError(w, http.StatusBadRequest, "INVALID_CURSOR", "cursor is not valid for this list; restart from the first page", nil)
				return
			}
		}
		limit := min(limitParam(r, 50), 100)
		// One extra row says whether another page exists.
		ms, err := d.authz.Members(r.Context(), n.chain, after, limit+1)
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
			c := memberCursor(n.scope.ID, authz.MemberKey{Email: last.Email, Level: last.Scope.Level})
			out.NextCursor = &c
		}
		items := make([]memberDTO, 0, len(ms))
		for _, m := range ms {
			items = append(items, memberOut(m))
		}
		out.Items = items
		writeJSON(w, http.StatusOK, out)
	}
}

func (d *dashboard) memberSet(resolve nodeResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := d.user(w, r)
		if !ok {
			return
		}
		n, ok := resolve(w, r, p)
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
		if !slices.Contains(n.roles, role) {
			names := make([]string, len(n.roles))
			for i, x := range n.roles {
				names[i] = string(x)
			}
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "role must be one of "+strings.Join(names, ", ")+" here", map[string]any{"field": "role"})
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
		// No account at all and mail on: invite the address instead (§11.2).
		if errors.Is(err, authz.ErrNotFound) && d.mailer != nil {
			if d.ownerOnly(w, r, n, "", role) {
				d.invite(w, r, n, email, role, d.actor(r, p))
			}
			return
		}
		if errors.Is(err, authz.ErrNotFound) || (err == nil && u.EmailVerifiedAt.IsZero()) {
			httpx.WriteError(w, http.StatusNotFound, "USER_NOT_FOUND", "no verified account uses that email", map[string]any{"field": "email"})
			return
		}
		if err != nil {
			d.internal(w, r, err)
			return
		}
		if !d.ownerOnly(w, r, n, u.ID, role) {
			return
		}
		m, err := d.authz.SetRole(r.Context(), u.ID, n.scope, role, d.actor(r, p))
		if !d.memberWriteErr(w, r, err) {
			return
		}
		writeJSON(w, http.StatusOK, memberOut(authz.Member{UserID: u.ID, Email: u.Email, Role: m.Role, Scope: m.Scope, CreatedAt: m.CreatedAt}))
	}
}

func (d *dashboard) memberRevoke(resolve nodeResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := d.user(w, r)
		if !ok {
			return
		}
		n, ok := resolve(w, r, p)
		if !ok {
			return
		}
		target := r.PathValue("userId")
		if !d.ownerOnly(w, r, n, target, "") {
			return
		}
		// Scoped to this node: another tenant's user id, or a role held only
		// higher up, is simply not a row here.
		err := d.authz.RevokeRole(r.Context(), target, n.scope, d.actor(r, p))
		if errors.Is(err, authz.ErrNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "no role for that user held here", nil)
			return
		}
		if !d.memberWriteErr(w, r, err) {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (d *dashboard) memberWriteErr(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, authz.ErrLastOwner):
		httpx.WriteError(w, http.StatusConflict, "LAST_OWNER", "an organization keeps at least one owner; make someone else owner first", nil)
	default:
		d.internal(w, r, err)
	}
	return false
}

type memberScopeDTO struct {
	Level            authz.Level    `json:"level"` // organization | workspace
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	OrganizationName string         `json:"organizationName"`
	Actions          []authz.Action `json:"actions"`
}

// memberScopes lists the organizations and workspaces whose members the
// caller manages — the way into the team screens. Org admins and owners see
// their organization and every workspace in it; a workspace admin sees that
// workspace. Bounded by the tenant's size, like the projects list.
func (d *dashboard) memberScopes(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	ms, err := d.authz.ListMemberships(r.Context(), p.UserID)
	if err != nil {
		d.internal(w, r, err)
		return
	}
	out := []memberScopeDTO{}
	seen := map[string]bool{}
	add := func(s memberScopeDTO, roles []authz.Role) {
		s.Actions = authz.ActionsFor(roles)
		if !seen[s.ID] && slices.Contains(s.Actions, authz.ActionMemberManage) {
			seen[s.ID] = true
			out = append(out, s)
		}
	}
	// Roles by organization, so a workspace inherits its org's.
	orgRoles := map[string][]authz.Role{}
	for _, m := range ms {
		if m.Scope.Level == authz.LevelOrganization {
			orgRoles[m.Scope.ID] = append(orgRoles[m.Scope.ID], m.Role)
		}
	}
	for _, m := range ms {
		switch m.Scope.Level {
		case authz.LevelOrganization:
			org, err := d.tenancy.GetOrganization(r.Context(), tenancy.OrganizationID(m.Scope.ID))
			if err != nil {
				continue // an org deleted under a stale membership
			}
			add(memberScopeDTO{Level: authz.LevelOrganization, ID: string(org.ID), Name: org.Name, OrganizationName: org.Name}, orgRoles[m.Scope.ID])
			wss, err := d.tenancy.ListWorkspaces(r.Context(), org.ID)
			if err != nil {
				d.internal(w, r, err)
				return
			}
			for _, ws := range wss {
				add(memberScopeDTO{Level: authz.LevelWorkspace, ID: string(ws.ID), Name: ws.Name, OrganizationName: org.Name}, orgRoles[m.Scope.ID])
			}
		case authz.LevelWorkspace:
			ws, err := d.tenancy.GetWorkspace(r.Context(), tenancy.WorkspaceID(m.Scope.ID))
			if err != nil {
				continue
			}
			org, err := d.tenancy.GetOrganization(r.Context(), ws.OrgID)
			if err != nil {
				continue
			}
			add(memberScopeDTO{Level: authz.LevelWorkspace, ID: string(ws.ID), Name: ws.Name, OrganizationName: org.Name}, append([]authz.Role{m.Role}, orgRoles[string(ws.OrgID)]...))
		}
	}
	// Organizations first, then workspaces, each by name.
	slices.SortStableFunc(out, func(a, b memberScopeDTO) int {
		if a.Level != b.Level {
			if a.Level == authz.LevelOrganization {
				return -1
			}
			return 1
		}
		return strings.Compare(a.OrganizationName+"\x00"+a.Name, b.OrganizationName+"\x00"+b.Name)
	})
	writeJSON(w, http.StatusOK, page{Items: out})
}
