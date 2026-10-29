package authz

import (
	"errors"
	"net/http"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
)

// Self-serve sign-up (ADR-001, variant A "join_org") — before any session
// exists, so guarded like the login (allow-listed Origin + double-submit
// CSRF). Mounted only when SIGNUP=on.
//
//	POST /v1/session/signup {email, password, scopeLevel, scopeId, requestedRole, note?}
//	  → creates the account, files a pending access request, opens a session
//	    (the session reaches nothing until an admin approves the request).

func (h *webHandlers) signup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email         string `json:"email"`
		Password      string `json:"password"`
		ScopeLevel    Level  `json:"scopeLevel"`
		ScopeID       string `json:"scopeId"`
		RequestedRole Role   `json:"requestedRole"`
		Note          string `json:"note"`
	}
	if !h.anonymousWrite(w, r, &req) {
		return
	}
	scope := ScopeRef{Level: req.ScopeLevel, ID: req.ScopeID}
	sess, secret, err := h.svc.Signup(r.Context(), req.Email, req.Password, scope, req.RequestedRole, req.Note,
		SessionMeta{IP: clientIP(r), UserAgent: r.UserAgent()})
	if err != nil {
		h.signupErr(w, r, err)
		return
	}
	h.signedIn(w, r, sess, secret)
}

func (h *webHandlers) signupErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrAccountExists):
		httpx.WriteError(w, http.StatusConflict, "ACCOUNT_EXISTS", "that email already has an account; sign in instead", nil)
	case errors.Is(err, ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		httpx.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many attempts; try again later", nil)
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_SCOPE", "the organization, workspace or project to join was not found", map[string]any{"field": "scopeId"})
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_SIGNUP", "check the email, a password of at least 12 characters, and the requested role", nil)
	default:
		httpx.Cause(r.Context(), err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "authentication temporarily unavailable", nil)
	}
}
