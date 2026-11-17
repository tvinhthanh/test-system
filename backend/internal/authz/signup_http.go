package authz

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
)

// Self-serve sign-up (ADR-001, variant A "join_org") over HTTP, before any
// session — guarded like the login (allow-listed Origin + double-submit
// CSRF). Shaped like forgot password: the form gets the same 202 for an
// address with or without an account; only the limit runs before the answer,
// the lookup, the request row and the mail (WebOptions.SendSignup) after it.
// The mailed link is /signup#<token>; the token only travels in a body.
// SendSignup nil (SIGNUP=off, the default) answers 404 SIGNUP_OFF.
//
//	POST /v1/session/signup          {email, scopeLevel, scopeId, requestedRole, note?} → 202, always
//	POST /v1/session/signup/complete {token, password} → signed in; the session
//	  reaches nothing until an admin approves the access request

func signupRoutes(mux httpx.Mux, h *webHandlers) {
	mux.Handle("POST /v1/session/signup", http.HandlerFunc(h.signup))
	mux.Handle("POST /v1/session/signup/complete", http.HandlerFunc(h.signupComplete))
}

func (h *webHandlers) signupOff(w http.ResponseWriter) bool {
	if h.opts.SendSignup != nil {
		return false
	}
	httpx.WriteError(w, http.StatusNotFound, "SIGNUP_OFF", "sign-up is not open here; ask an admin to invite you", nil)
	return true
}

func (h *webHandlers) signup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email         string `json:"email"`
		ScopeLevel    Level  `json:"scopeLevel"`
		ScopeID       string `json:"scopeId"`
		RequestedRole Role   `json:"requestedRole"`
		Note          string `json:"note"`
	}
	if !h.anonymousWrite(w, r, &req) || h.signupOff(w) {
		return
	}
	form := SignupRequest{Email: req.Email, Scope: ScopeRef{Level: req.ScopeLevel, ID: req.ScopeID}, Role: req.RequestedRole, Note: req.Note}
	switch err := h.svc.CountSignupRequest(r.Context(), form, clientIP(r)); {
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_SIGNUP", "check the email, the note (at most 1000 characters), and a role that exists at the chosen level", nil)
		return
	case errors.Is(err, ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		httpx.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests; try again later", nil)
		return
	case err != nil:
		httpx.Cause(r.Context(), err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "authentication temporarily unavailable", nil)
		return
	}
	// Whatever differs between an address with and without an account — the
	// lookup, the row, the mail — happens after the answer, on its own clock.
	go func(form SignupRequest) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		to, token, err := h.svc.IssueSignup(ctx, form)
		if err == nil && token != "" {
			err = h.opts.SendSignup(ctx, to, token)
		}
		if err != nil {
			h.logger().Error("sign-up", "err", err)
		}
	}(form)
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "if that email can sign up, a link to finish is on its way"})
}

func (h *webHandlers) signupComplete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !h.anonymousWrite(w, r, &req) || h.signupOff(w) {
		return
	}
	sess, secret, err := h.svc.CompleteSignup(r.Context(), req.Token, req.Password, SessionMeta{IP: clientIP(r), UserAgent: r.UserAgent()})
	switch {
	case err == nil:
		h.signedIn(w, r, sess, secret)
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "SIGNUP_NOT_FOUND", "this link is not valid: used, replaced by a newer one, or older than 24 hours", nil)
	case errors.Is(err, ErrAccountExists):
		httpx.WriteError(w, http.StatusConflict, "ACCOUNT_EXISTS", "that email has an account by now; sign in with it", nil)
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
