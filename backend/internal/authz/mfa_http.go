package authz

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
)

// Second-factor routes (§11.2). The first runs before a session exists and
// is guarded like the login; the rest ride on the session.
//
//	POST /v1/session/mfa          {challenge, code} → session, like POST /v1/session
//	GET  /v1/session/mfa          → {enabled, required, recoveryCodesLeft}
//	POST /v1/session/mfa/setup    → {secret, otpauthUri}   (MFA off only)
//	POST /v1/session/mfa/enable   {code} → {recoveryCodes} (shown once)
//	POST /v1/session/mfa/disable  {code} → 204             (not for org owners)
func mfaRoutes(mux httpx.Mux, h *webHandlers) {
	mux.Handle("POST /v1/session/mfa", http.HandlerFunc(h.mfaComplete))
	mux.Handle("GET /v1/session/mfa", http.HandlerFunc(h.mfaStatus))
	mux.Handle("POST /v1/session/mfa/setup", http.HandlerFunc(h.mfaSetup))
	mux.Handle("POST /v1/session/mfa/enable", http.HandlerFunc(h.mfaEnable))
	mux.Handle("POST /v1/session/mfa/disable", http.HandlerFunc(h.mfaDisable))
}

// mfaExempt: what an org owner without MFA may still reach — the bootstrap,
// logout and the MFA routes themselves.
func mfaExempt(path string) bool {
	return path == "/v1/session" || path == "/v1/session/logout" || path == "/v1/session/mfa" ||
		strings.HasPrefix(path, "/v1/session/mfa/") || strings.HasPrefix(path, "/v1/session/oauth/")
}

type mfaFlags struct {
	Enabled  bool `json:"enabled"`
	Required bool `json:"required"`
}

// userOut is the session user with its MFA flags, so the client can send an
// owner without MFA straight to the setup screen.
func (h *webHandlers) userOut(r *http.Request, u User) *sessionUser {
	out := userDTO(u)
	if st, err := h.svc.MFAStatus(r.Context(), u.ID); err == nil {
		out.MFA = &mfaFlags{Enabled: st.Enabled, Required: st.Required}
	}
	return out
}

func (h *webHandlers) mfaComplete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
	}
	if !h.anonymousWrite(w, r, &req) {
		return
	}
	// After an OAuth sign-in the challenge is in an HttpOnly cookie the page
	// cannot read (oauth_http.go); after a password it is in the body.
	fromCookie := false
	if req.Challenge == "" {
		if c, err := r.Cookie(mfaCookie); err == nil {
			req.Challenge, fromCookie = c.Value, true
		}
	}
	sess, secret, err := h.svc.CompleteMFALogin(r.Context(), req.Challenge, req.Code, SessionMeta{IP: clientIP(r), UserAgent: r.UserAgent()})
	if fromCookie && (err == nil || errors.Is(err, ErrUnauthenticated)) {
		http.SetCookie(w, &http.Cookie{Name: mfaCookie, Value: "", Path: "/v1/session/mfa", HttpOnly: true, Secure: h.opts.Secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	}
	switch {
	case err == nil:
		h.signedIn(w, r, sess, secret)
	case errors.Is(err, ErrUnauthenticated):
		httpx.WriteError(w, http.StatusUnauthorized, "MFA_CHALLENGE_EXPIRED", "sign in again", nil)
	default:
		h.codeErr(w, r, err)
	}
}

// sessionUserFor is the prologue of the session-bound MFA routes.
func sessionUserFor(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	p, ok := PrincipalFrom(r.Context())
	if !ok || p.Kind != PrincipalUser {
		httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "log in first", nil)
		return Principal{}, false
	}
	return p, true
}

func (h *webHandlers) mfaStatus(w http.ResponseWriter, r *http.Request) {
	p, ok := sessionUserFor(w, r)
	if !ok {
		return
	}
	st, err := h.svc.MFAStatus(r.Context(), p.UserID)
	if err != nil {
		h.codeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": st.Enabled, "required": st.Required, "recoveryCodesLeft": st.RecoveryCodesLeft})
}

func (h *webHandlers) mfaSetup(w http.ResponseWriter, r *http.Request) {
	p, ok := sessionUserFor(w, r)
	if !ok {
		return
	}
	setup, err := h.svc.BeginMFASetup(r.Context(), p.UserID)
	if err != nil {
		h.codeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"secret": setup.Secret, "otpauthUri": setup.URI})
}

func decodeCode(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		Code string `json:"code"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Code) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "code is required", map[string]any{"field": "code"})
		return "", false
	}
	return req.Code, true
}

func (h *webHandlers) mfaEnable(w http.ResponseWriter, r *http.Request) {
	p, ok := sessionUserFor(w, r)
	if !ok {
		return
	}
	code, ok := decodeCode(w, r)
	if !ok {
		return
	}
	sess, _ := SessionFrom(r.Context())
	codes, err := h.svc.EnableMFA(r.Context(), p.UserID, code, sess.ID)
	if err != nil {
		h.codeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recoveryCodes": codes})
}

func (h *webHandlers) mfaDisable(w http.ResponseWriter, r *http.Request) {
	p, ok := sessionUserFor(w, r)
	if !ok {
		return
	}
	code, ok := decodeCode(w, r)
	if !ok {
		return
	}
	if err := h.svc.DisableMFA(r.Context(), p.UserID, code); err != nil {
		h.codeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *webHandlers) codeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalidCode):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_CODE", "that code is not valid; codes change every 30 seconds and each works once", map[string]any{"field": "code"})
	case errors.Is(err, ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		httpx.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many attempts; try again later", nil)
	case errors.Is(err, ErrMFAMandatory):
		httpx.WriteError(w, http.StatusConflict, "MFA_MANDATORY", "org owners must keep two-factor authentication on", nil)
	case errors.Is(err, ErrConflict):
		httpx.WriteError(w, http.StatusConflict, "CONFLICT", strings.TrimPrefix(err.Error(), "authz: already exists: "), nil)
	default:
		httpx.Cause(r.Context(), err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "authentication temporarily unavailable", nil)
	}
}
