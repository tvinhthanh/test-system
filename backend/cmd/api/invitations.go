package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/mail"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

// Invitations by mail (§11.2 "mời member", §11.6 API6), the manager's side.
// With MAIL=smtp, adding an email that has no account to a project,
// workspace or organization (POST …/members) invites it instead of
// answering USER_NOT_FOUND: authz makes a single-use token, this file mails
// the link, and a mail that cannot be sent withdraws the invitation. The
// same rules as a grant apply — member.manage on the node, the owner role
// only by an owner. Accepting is authz's anonymous half (invite_http.go).
//
//	GET    /v1/{projects|workspaces|organizations}/{id}/invitations
//	DELETE /v1/{projects|workspaces|organizations}/{id}/invitations/{invitationId}

func (d *dashboard) invitationRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	for prefix, resolve := range map[string]nodeResolver{
		"/v1/projects/{projectId}":           d.projectNode,
		"/v1/workspaces/{workspaceId}":       d.workspaceNode,
		"/v1/organizations/{organizationId}": d.organizationNode,
	} {
		handle("GET "+prefix+"/invitations", d.invitations(resolve))
		handle("DELETE "+prefix+"/invitations/{invitationId}", d.invitationRevoke(resolve))
	}
}

type invitationDTO struct {
	ID        string      `json:"id"`
	Email     string      `json:"email"`
	Role      authz.Role  `json:"role"`
	Level     authz.Level `json:"level"`
	InvitedBy string      `json:"invitedBy"`
	CreatedAt time.Time   `json:"createdAt"`
	ExpiresAt time.Time   `json:"expiresAt"`
}

func invitationOut(i authz.Invitation) invitationDTO {
	return invitationDTO{ID: i.ID, Email: i.Email, Role: i.Role, Level: i.Scope.Level, InvitedBy: i.InvitedBy, CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt}
}

// oneLine keeps a name user-chosen (an org or project name) to one line of
// printable text before it goes near a mail header.
func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// scopeName names a node for people: "checkout (Payments, Acme)".
func scopeName(ctx context.Context, ten tenancy.Service, s authz.ScopeRef) string {
	switch s.Level {
	case authz.LevelOrganization:
		if o, err := ten.GetOrganization(ctx, tenancy.OrganizationID(s.ID)); err == nil {
			return oneLine(o.Name)
		}
	case authz.LevelWorkspace:
		if w, err := ten.GetWorkspace(ctx, tenancy.WorkspaceID(s.ID)); err == nil {
			if o, err := ten.GetOrganization(ctx, w.OrgID); err == nil {
				return oneLine(w.Name + " (" + o.Name + ")")
			}
			return oneLine(w.Name)
		}
	case authz.LevelProject:
		if sc, err := ten.ProjectScope(ctx, tenancy.ProjectID(s.ID)); err == nil {
			if p, err := ten.GetProject(ctx, sc.ProjectID); err == nil {
				if w, err := ten.GetWorkspace(ctx, sc.WorkspaceID); err == nil {
					return oneLine(p.Name + " (" + w.Name + ")")
				}
				return oneLine(p.Name)
			}
		}
	}
	return "a team"
}

var roleWords = map[authz.Role]string{
	authz.RoleOrgOwner: "organization owner", authz.RoleOrgAdmin: "organization admin",
	authz.RoleWorkspaceAdmin: "workspace admin", authz.RoleMaintainer: "maintainer",
	authz.RoleMember: "member", authz.RoleViewer: "viewer",
}

// invite is POST …/members for an email with no account, mail on. The
// owner rule has already been applied by the caller.
func (d *dashboard) invite(w http.ResponseWriter, r *http.Request, n node, email string, role authz.Role, actor string) {
	inv, token, err := d.authz.Invite(r.Context(), email, n.scope, role, actor)
	switch {
	case errors.Is(err, authz.ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error(), map[string]any{"field": "email"})
		return
	case errors.Is(err, authz.ErrConflict):
		// An account appeared in between (or is unverified): not a mail case.
		httpx.WriteError(w, http.StatusNotFound, "USER_NOT_FOUND", "that email has an account that is not verified yet", map[string]any{"field": "email"})
		return
	case err != nil:
		d.internal(w, r, err)
		return
	}
	name := scopeName(r.Context(), d.tenancy, n.scope)
	msg := mail.Message{
		To:      inv.Email,
		Subject: oneLine("You're invited to " + name + " on ATP"),
		Text: fmt.Sprintf("%s invited you to %s on ATP, the test report center, as %s.\n\n"+
			"Accept the invitation and choose a password:\n%s/invite#%s\n\n"+
			"The link works once and expires on %s (UTC).\n"+
			"If you did not expect this, ignore this email: nothing happens unless the link is used.\n",
			actor, name, roleWords[role], d.publicURL, token, inv.ExpiresAt.Format("2 January 2006, 15:04")),
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := d.mailer.Send(ctx, msg); err != nil {
		httpx.Cause(r.Context(), fmt.Errorf("invitation mail: %w", err))
		// An invitation nobody received must not stay usable.
		_ = d.authz.RevokeInvitation(r.Context(), n.scope, inv.ID, "system: the invitation mail could not be sent")
		httpx.WriteError(w, http.StatusBadGateway, "MAIL_FAILED", "the invitation email could not be sent; nothing was created — try again later", nil)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"invitation": invitationOut(inv)})
}

func (d *dashboard) invitations(resolve nodeResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := d.user(w, r)
		if !ok {
			return
		}
		n, ok := resolve(w, r, p)
		if !ok {
			return
		}
		invs, err := d.authz.Invitations(r.Context(), n.scope)
		if err != nil {
			d.internal(w, r, err)
			return
		}
		items := make([]invitationDTO, 0, len(invs))
		for _, i := range invs {
			items = append(items, invitationOut(i))
		}
		writeJSON(w, http.StatusOK, page{Items: items})
	}
}

func (d *dashboard) invitationRevoke(resolve nodeResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := d.user(w, r)
		if !ok {
			return
		}
		n, ok := resolve(w, r, p)
		if !ok {
			return
		}
		id := r.PathValue("invitationId")
		invs, err := d.authz.Invitations(r.Context(), n.scope)
		if err != nil {
			d.internal(w, r, err)
			return
		}
		// An invitation to be owner is the owners' to withdraw (§11.3.1).
		for _, i := range invs {
			if i.ID == id && !d.ownerOnly(w, r, n, "", i.Role) {
				return
			}
		}
		switch err := d.authz.RevokeInvitation(r.Context(), n.scope, id, d.actor(r, p)); {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, authz.ErrNotFound):
			httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "no open invitation with that id here", nil)
		default:
			d.internal(w, r, err)
		}
	}
}
