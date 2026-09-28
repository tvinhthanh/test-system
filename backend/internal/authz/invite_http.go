package authz

import (
	"errors"
	"net/http"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
)

// Accepting an invitation (§11.2) — before any session exists, so guarded
// like the login (allow-listed Origin + double-submit CSRF). The token never
// travels in a URL the server sees: the mail links to /invite#<token> (a
// fragment stays in the browser) and the page posts it in the body, so it
// cannot land in an access log or a proxy's.
//
//	POST /v1/invitations/lookup {token}           → who is invited to what
//	POST /v1/invitations/accept {token, password} → account + role + session

func invitationRoutes(mux httpx.Mux, h *webHandlers) {
	mux.Handle("POST /v1/invitations/lookup", http.HandlerFunc(h.invitationLookup))
	mux.Handle("POST /v1/invitations/accept", http.HandlerFunc(h.invitationAccept))
}

type invitationDTO struct {
	Email     string    `json:"email"`
	Role      Role      `json:"role"`
	Level     Level     `json:"level"`
	ScopeName string    `json:"scopeName"`
	InvitedBy string    `json:"invitedBy"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (h *webHandlers) invitationErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "INVITATION_NOT_FOUND", "this invitation is not valid: used, withdrawn or older than seven days", nil)
	case errors.Is(err, ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		httpx.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many attempts; try again later", nil)
	case errors.Is(err, ErrAccountExists):
		httpx.WriteError(w, http.StatusConflict, "ACCOUNT_EXISTS", "that email has an account; the role is added — sign in with it", nil)
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PASSWORD", "choose a password of at least 12 characters", map[string]any{"field": "password"})
	default:
		httpx.Cause(r.Context(), err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "authentication temporarily unavailable", nil)
	}
}

func (h *webHandlers) invitationLookup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !h.anonymousWrite(w, r, &req) {
		return
	}
	inv, err := h.svc.LookupInvitation(r.Context(), req.Token, clientIP(r))
	if err != nil {
		h.invitationErr(w, r, err)
		return
	}
	name := ""
	if h.opts.DescribeScope != nil {
		name = h.opts.DescribeScope(r.Context(), inv.Scope)
	}
	writeJSON(w, http.StatusOK, invitationDTO{Email: inv.Email, Role: inv.Role, Level: inv.Scope.Level, ScopeName: name, InvitedBy: inv.InvitedBy, ExpiresAt: inv.ExpiresAt})
}

func (h *webHandlers) invitationAccept(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !h.anonymousWrite(w, r, &req) {
		return
	}
	sess, secret, err := h.svc.AcceptInvitation(r.Context(), req.Token, req.Password, SessionMeta{IP: clientIP(r), UserAgent: r.UserAgent()})
	if err != nil {
		h.invitationErr(w, r, err)
		return
	}
	h.signedIn(w, r, sess, secret)
}
