package authz

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
)

// Dashboard authentication over HTTP (§11.2), shaped by
// frontend/API_CONTRACT.md:
//
//   - GET  /v1/session         → bootstrap: user or null, plus the CSRF token
//   - POST /v1/session         {email, password} → session cookie, rotated CSRF
//   - POST /v1/session/logout  → revokes the session
//   - /v1/session/mfa…        → second factor (mfa_http.go)
//
// Session cookie: httpOnly, Secure, SameSite=Lax. CSRF: double submit — a
// readable cookie whose value must be echoed in X-CSRF-Token on every
// state-changing request that rides on a session — PLUS an Origin/Referer
// allowlist, because §11.2 says SameSite is not the only CSRF layer.
// Failures answer the same 401 whatever the cause (no enumeration).

const (
	SessionCookie = "atp_session"
	CSRFCookie    = "atp_csrf"
	CSRFHeader    = "X-CSRF-Token"
)

// WebOptions configures the cookie and CSRF behaviour.
type WebOptions struct {
	// AllowedOrigins are the dashboard origins (scheme://host[:port]) that
	// may make state-changing requests. Empty means none — every
	// cookie-authenticated POST is refused, which is the safe default.
	AllowedOrigins []string
	// Secure sets the cookie Secure flag. Always true outside dev; dev over
	// http://localhost still works because browsers treat localhost as a
	// secure context.
	Secure bool
	// OAuth are the switched-on sign-in providers (config OAUTH_GOOGLE /
	// OAUTH_GITHUB); empty means password only. StateKey signs the flow's
	// state cookie (OAuthStateKey). OAuthHTTP reaches the providers (a
	// platform/outbound client allowed only their endpoints).
	OAuth     []*OAuthProvider
	StateKey  []byte
	OAuthHTTP OAuthHTTP
	// Invitations says mail is on, so granting a role to an unknown email
	// invites it (the bootstrap tells the members screen). DescribeScope
	// names a node for the accept page — authz does not know tenancy.
	Invitations   bool
	DescribeScope func(ctx context.Context, s ScopeRef) string
	// SendReset mails a password-reset link (§11.2); nil = reset by email
	// is off. It must not block: the caller sends in the background so the
	// answer takes the same time for a known and an unknown email.
	SendReset func(to, token string)
}

type sessionKey struct{}

// SessionFrom returns the session placed in ctx by SessionMiddleware.
func SessionFrom(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(Session)
	return s, ok
}

// SessionMiddleware authenticates the session cookie into a user Principal
// and enforces CSRF on state-changing requests. A request without a
// session cookie passes through unauthenticated (a later Bearer or an
// endpoint that allows anonymous access decides); a request with a bad one
// is a 401 — except the bootstrap GET /v1/session, where a stale cookie is
// just an anonymous visitor: the cookie is cleared and the bootstrap answers
// {user: null} (API_CONTRACT: bootstrap is 200 even when anonymous).
func SessionMiddleware(svc Service, opts WebOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(SessionCookie)
			if err != nil || c.Value == "" {
				next.ServeHTTP(w, r)
				return
			}
			p, sess, err := svc.AuthenticateSession(r.Context(), c.Value)
			if err != nil {
				if errors.Is(err, ErrUnauthenticated) {
					if isBootstrap(r) {
						// Only the dead session cookie goes; the bootstrap handler
						// keeps or mints the CSRF cookie itself.
						clearCookie(w, opts, SessionCookie)
						next.ServeHTTP(w, r)
						return
					}
					clearCookies(w, opts)
					httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "session expired or invalid", nil)
					return
				}
				httpx.Cause(r.Context(), err)
				httpx.WriteError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "authentication temporarily unavailable", nil)
				return
			}
			if !safeMethod(r.Method) {
				if reason := csrfCheck(r, opts); reason != "" {
					httpx.WriteError(w, http.StatusForbidden, "CSRF", reason, nil)
					return
				}
			}
			// §11.2: an org owner without MFA reaches nothing but the session
			// and MFA routes until they turn it on.
			if p.MFAEnrollmentRequired && !mfaExempt(r.URL.Path) {
				httpx.WriteError(w, http.StatusForbidden, "MFA_ENROLLMENT_REQUIRED", "org owners must turn on two-factor authentication first", nil)
				return
			}
			ctx := context.WithValue(WithPrincipal(r.Context(), p), sessionKey{}, sess)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// isBootstrap reports the anonymous-capable session bootstrap.
func isBootstrap(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/v1/session"
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// csrfCheck applies both layers: origin allowlist, then the double-submit
// token. Returns "" when the request passes.
func csrfCheck(r *http.Request, opts WebOptions) string {
	if reason := originCheck(r, opts, true); reason != "" {
		return reason
	}
	c, err := r.Cookie(CSRFCookie)
	if err != nil || c.Value == "" {
		return "missing CSRF cookie"
	}
	if subtle.ConstantTimeCompare([]byte(c.Value), []byte(r.Header.Get(CSRFHeader))) != 1 {
		return "CSRF token missing or mismatched"
	}
	return ""
}

// originCheck refuses a request whose Origin (or, failing that, Referer)
// is not on the allowlist. A request carrying neither header is not a
// browser form post; it passes when requireHeader is false (login from a
// CLI) and is refused otherwise.
func originCheck(r *http.Request, opts WebOptions, requireHeader bool) string {
	src := r.Header.Get("Origin")
	if src == "" {
		if ref := r.Header.Get("Referer"); ref != "" {
			if u, err := url.Parse(ref); err == nil {
				src = u.Scheme + "://" + u.Host
			}
		}
	}
	if src == "" {
		if requireHeader {
			return "missing Origin"
		}
		return ""
	}
	for _, allowed := range opts.AllowedOrigins {
		if strings.EqualFold(strings.TrimSuffix(allowed, "/"), src) {
			return ""
		}
	}
	return "origin not allowed"
}

// AuthRoutes mounts the session endpoints of frontend/API_CONTRACT.md:
//
//	GET  /v1/session         → {user: null | {id,name,email}, csrfToken}; anonymous is 200
//	POST /v1/session         {email, password} → same shape with the user and a rotated token
//	POST /v1/session/logout  {} → 204
//
// and, when the account has MFA on, POST /v1/session answers 202
// {mfa: "required", challenge, expiresAt} and the session comes from
// POST /v1/session/mfa (mfa_http.go).
//
// The CSRF cookie is issued on the anonymous bootstrap so the login POST
// itself can carry X-CSRF-Token (double submit) — the browser never
// submits credentials without first having been handed a token by us.
// Mount SessionMiddleware around the whole mux separately.
func AuthRoutes(mux httpx.Mux, svc Service, opts WebOptions) {
	h := &webHandlers{svc: svc, opts: opts}
	mux.Handle("GET /v1/session", http.HandlerFunc(h.session))
	mux.Handle("POST /v1/session", http.HandlerFunc(h.login))
	mux.Handle("POST /v1/session/logout", http.HandlerFunc(h.logout))
	mfaRoutes(mux, h)
	oauthRoutes(mux, h)
	invitationRoutes(mux, h)
	resetRoutes(mux, h)
}

type webHandlers struct {
	svc  Service
	opts WebOptions
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type sessionUser struct {
	ID    string    `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
	MFA   *mfaFlags `json:"mfa,omitempty"`
}

type sessionResponse struct {
	User      *sessionUser `json:"user"`
	CSRFToken string       `json:"csrfToken"`
	ExpiresAt *time.Time   `json:"expiresAt,omitempty"`
	// Providers are the OAuth sign-ins switched on (§11.2) — the sign-in
	// page shows a button for each. Always present, possibly empty.
	Providers []string `json:"providers"`
	// Invitations: mail is on; adding an unknown email to a team invites it.
	Invitations bool `json:"invitations"`
	// PasswordReset: "forgot password" by email is available.
	PasswordReset bool `json:"passwordReset"`
}

// session is the bootstrap: who is logged in (if anyone) plus a CSRF
// token. An existing CSRF cookie is reused so parallel tabs agree; a
// missing one is minted here.
func (h *webHandlers) session(w http.ResponseWriter, r *http.Request) {
	csrf := ""
	if c, err := r.Cookie(CSRFCookie); err == nil && validCSRF(c.Value) {
		csrf = c.Value
	}
	if csrf == "" {
		csrf = newCSRFToken()
		h.setCSRFCookie(w, csrf, time.Time{})
	}
	resp := sessionResponse{CSRFToken: csrf, Providers: h.providerNames(), Invitations: h.opts.Invitations, PasswordReset: h.opts.SendReset != nil}
	if p, ok := PrincipalFrom(r.Context()); ok && p.Kind == PrincipalUser {
		if u, err := h.svc.GetUser(r.Context(), p.UserID); err == nil {
			resp.User = h.userOut(r, u)
			if s, ok := SessionFrom(r.Context()); ok {
				exp := s.ExpiresAt
				resp.ExpiresAt = &exp
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// anonymousWrite is the CSRF gate for the two requests that run before a
// session exists (password, then second factor): allow-listed Origin plus
// the double-submit token from GET /v1/session; and nobody already signed in.
func (h *webHandlers) anonymousWrite(w http.ResponseWriter, r *http.Request, v any) bool {
	if reason := originCheck(r, h.opts, false); reason != "" {
		httpx.WriteError(w, http.StatusForbidden, "CSRF", reason, nil)
		return false
	}
	if c, err := r.Cookie(CSRFCookie); err != nil || c.Value == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(r.Header.Get(CSRFHeader))) != 1 {
		httpx.WriteError(w, http.StatusForbidden, "CSRF", "CSRF token missing or mismatched; call GET /v1/session first", nil)
		return false
	}
	if _, ok := PrincipalFrom(r.Context()); ok {
		httpx.WriteError(w, http.StatusBadRequest, "ALREADY_LOGGED_IN", "log out first", nil)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "malformed JSON", nil)
		return false
	}
	return true
}

// mfaChallengeResponse is the 202 of a correct password on an account with
// MFA on: no session yet, only the challenge for POST /v1/session/mfa.
type mfaChallengeResponse struct {
	MFA       string    `json:"mfa"` // always "required"
	Challenge string    `json:"challenge"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (h *webHandlers) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !h.anonymousWrite(w, r, &req) {
		return
	}
	sess, secret, err := h.svc.Login(r.Context(), req.Email, req.Password, SessionMeta{IP: clientIP(r), UserAgent: r.UserAgent()})
	var ch *MFARequired
	switch {
	case err == nil:
		h.signedIn(w, r, sess, secret)
	case errors.As(err, &ch):
		writeJSON(w, http.StatusAccepted, mfaChallengeResponse{MFA: "required", Challenge: ch.Challenge, ExpiresAt: ch.ExpiresAt})
	case errors.Is(err, ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		httpx.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many attempts; try again later", nil)
	case errors.Is(err, ErrUnauthenticated):
		// One message for every cause (§11.2).
		httpx.WriteError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "email or password is incorrect", nil)
	default:
		httpx.Cause(r.Context(), err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "authentication temporarily unavailable", nil)
	}
}

// signedIn sets the session cookie and answers like the bootstrap.
func (h *webHandlers) signedIn(w http.ResponseWriter, r *http.Request, sess Session, secret string) {
	csrf := h.setSessionCookies(w, sess, secret)
	u, _ := h.svc.GetUser(r.Context(), sess.UserID)
	exp := sess.ExpiresAt
	writeJSON(w, http.StatusOK, sessionResponse{User: h.userOut(r, u), CSRFToken: csrf, ExpiresAt: &exp})
}

// setSessionCookies sets the session cookie and rotates the CSRF token
// with it; it returns the new token.
func (h *webHandlers) setSessionCookies(w http.ResponseWriter, sess Session, secret string) string {
	csrf := newCSRFToken()
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: secret, Path: "/", HttpOnly: true, Secure: h.opts.Secure, SameSite: http.SameSiteLaxMode, Expires: sess.ExpiresAt})
	h.setCSRFCookie(w, csrf, sess.ExpiresAt)
	return csrf
}

func (h *webHandlers) logout(w http.ResponseWriter, r *http.Request) {
	sess, ok := SessionFrom(r.Context())
	if ok {
		if err := h.svc.Logout(r.Context(), sess.ID); err != nil && !errors.Is(err, ErrNotFound) {
			httpx.Cause(r.Context(), err)
			httpx.WriteError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "could not revoke session", nil)
			return
		}
	}
	clearCookies(w, h.opts)
	w.WriteHeader(http.StatusNoContent)
}

func (h *webHandlers) setCSRFCookie(w http.ResponseWriter, token string, expires time.Time) {
	c := &http.Cookie{Name: CSRFCookie, Value: token, Path: "/", HttpOnly: false, Secure: h.opts.Secure, SameSite: http.SameSiteLaxMode}
	if !expires.IsZero() {
		c.Expires = expires
	}
	http.SetCookie(w, c)
}

// userDTO: the contract wants a display name; users carry none yet, so the
// local part of the email stands in until profiles exist.
func userDTO(u User) *sessionUser {
	name := u.Email
	if at := strings.IndexByte(u.Email, '@'); at > 0 {
		name = u.Email[:at]
	}
	return &sessionUser{ID: u.ID, Name: name, Email: u.Email}
}

func validCSRF(s string) bool {
	if len(s) != 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func clearCookies(w http.ResponseWriter, opts WebOptions) {
	for _, name := range []string{SessionCookie, CSRFCookie} {
		clearCookie(w, opts, name)
	}
}

func clearCookie(w http.ResponseWriter, opts WebOptions, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", HttpOnly: name == SessionCookie, Secure: opts.Secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func newCSRFToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// clientIP is RemoteAddr without the port. Proxy headers are not trusted
// here; when a reverse proxy fronts the api, terminate it so RemoteAddr is
// the client (or add an explicit trusted-proxy setting — not silently).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
