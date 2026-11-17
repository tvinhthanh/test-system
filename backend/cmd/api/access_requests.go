package main

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
)

// Access requests (ADR-001, variant A "join_org"): the admin side of
// self-serve sign-up. A visitor signs up asking for a role on a node; it
// lands here pending until an admin of that node decides. Same gate as member
// management — member.manage on the node — and the owner role is still the
// owner's alone (granting org_owner on approval needs org.manage). Reuses the
// member nodeResolver, so the 404/403 tenant rules are identical.
//
//	GET  /v1/{projects|workspaces|organizations}/{id}/access-requests
//	POST /v1/{projects|workspaces|organizations}/{id}/access-requests/{requestId}/decide {decision, role?}

func (d *dashboard) accessRequestRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	for prefix, resolve := range map[string]nodeResolver{
		"/v1/projects/{projectId}":           d.projectNode,
		"/v1/workspaces/{workspaceId}":       d.workspaceNode,
		"/v1/organizations/{organizationId}": d.organizationNode,
	} {
		handle("GET "+prefix+"/access-requests", d.accessRequests(resolve))
		handle("POST "+prefix+"/access-requests/{requestId}/decide", d.accessRequestDecide(resolve))
	}
}

type accessRequestDTO struct {
	ID            string      `json:"id"`
	UserID        string      `json:"userId"`
	Email         string      `json:"email"`
	RequestedRole authz.Role  `json:"requestedRole"`
	Level         authz.Level `json:"level"`
	Note          string      `json:"note"`
	CreatedAt     time.Time   `json:"createdAt"`
}

func accessRequestOut(a authz.AccessRequest) accessRequestDTO {
	return accessRequestDTO{
		ID: a.ID, UserID: a.UserID, Email: a.Email, RequestedRole: a.RequestedRole,
		Level: a.Scope.Level, Note: a.Note, CreatedAt: a.CreatedAt,
	}
}

func (d *dashboard) accessRequests(resolve nodeResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := d.user(w, r)
		if !ok {
			return
		}
		n, ok := resolve(w, r, p)
		if !ok {
			return
		}
		reqs, err := d.authz.AccessRequests(r.Context(), n.scope)
		if err != nil {
			d.internal(w, r, err)
			return
		}
		items := make([]accessRequestDTO, 0, len(reqs))
		for _, a := range reqs {
			items = append(items, accessRequestOut(a))
		}
		writeJSON(w, http.StatusOK, page{Items: items})
	}
}

func (d *dashboard) accessRequestDecide(resolve nodeResolver) http.HandlerFunc {
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
			Decision string `json:"decision"` // approved | denied
			Role     string `json:"role"`     // optional override when approving
		}
		if !decodeSmall(w, r, &req) {
			return
		}
		if req.Decision != "approved" && req.Decision != "denied" {
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "decision must be approved or denied", map[string]any{"field": "decision"})
			return
		}
		// An approval that grants a role must pick a role valid at this node,
		// and the owner role stays the owner's alone — also when the role is
		// the one the visitor asked for (the form accepts org_owner), so the
		// role checked is the one the approval would grant.
		grant := authz.Role(req.Role)
		if req.Decision == "approved" {
			if grant != "" && !slices.Contains(n.roles, grant) {
				names := make([]string, len(n.roles))
				for i, x := range n.roles {
					names[i] = string(x)
				}
				httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "role must be one of "+strings.Join(names, ", ")+" here", map[string]any{"field": "role"})
				return
			}
			granted := grant
			if granted == "" {
				pending, err := d.authz.PendingAccessRequest(r.Context(), n.scope, r.PathValue("requestId"))
				switch {
				case errors.Is(err, authz.ErrNotFound):
					httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "no pending request with that id here", nil)
					return
				case err != nil:
					d.internal(w, r, err)
					return
				}
				granted = pending.RequestedRole
			}
			if !d.ownerOnly(w, r, n, "", granted) {
				return
			}
		}
		err := d.authz.DecideAccessRequest(r.Context(), n.scope, r.PathValue("requestId"), req.Decision, grant, d.actor(r, p))
		switch {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, authz.ErrNotFound):
			httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "no pending request with that id here", nil)
		case errors.Is(err, authz.ErrLastOwner):
			httpx.WriteError(w, http.StatusConflict, "LAST_OWNER", "an organization keeps at least one owner", nil)
		case errors.Is(err, authz.ErrInvalid):
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "the requested role is not valid at this node", nil)
		default:
			d.internal(w, r, err)
		}
	}
}
