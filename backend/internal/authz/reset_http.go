package authz

import (
	"errors"
	"net/http"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
)

// Forgot password over HTTP (§11.2), before any session — guarded like the
// login. Both answers are the same for a known and an unknown email: the
// mail, when there is one, leaves in the background (WebOptions.SendReset),
// so neither the body nor the time taken tells them apart. The mailed link
// is /reset#<token>; the token only travels in a body.
//
//	POST /v1/session/password/forgot {email}           → 202, always
//	POST /v1/session/password/reset  {token, password} → 204

func resetRoutes(mux httpx.Mux, h *webHandlers) {
	mux.Handle("POST /v1/session/password/forgot", http.HandlerFunc(h.passwordForgot))
	mux.Handle("POST /v1/session/password/reset", http.HandlerFunc(h.passwordReset))
}

func (h *webHandlers) passwordForgot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !h.anonymousWrite(w, r, &req) {
		return
	}
	if h.opts.SendReset == nil {
		httpx.WriteError(w, http.StatusNotFound, "PASSWORD_RESET_OFF", "password reset by email is not set up here; ask an admin", nil)
		return
	}
	to, token, err := h.svc.RequestPasswordReset(r.Context(), req.Email, clientIP(r))
	switch {
	case errors.Is(err, ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		httpx.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests; try again later", nil)
		return
	case err != nil:
		httpx.Cause(r.Context(), err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "authentication temporarily unavailable", nil)
		return
	}
	if token != "" {
		h.opts.SendReset(to, token)
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "if an account uses that email, a link to choose a new password is on its way"})
}

func (h *webHandlers) passwordReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !h.anonymousWrite(w, r, &req) {
		return
	}
	switch err := h.svc.ResetPassword(r.Context(), req.Token, req.Password, clientIP(r)); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "RESET_NOT_FOUND", "this link is not valid: used, replaced by a newer one, or older than 30 minutes", nil)
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PASSWORD", "choose a password of at least 12 characters", map[string]any{"field": "password"})
	case errors.Is(err, ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		httpx.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many attempts; try again later", nil)
	default:
		httpx.Cause(r.Context(), err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "authentication temporarily unavailable", nil)
	}
}
