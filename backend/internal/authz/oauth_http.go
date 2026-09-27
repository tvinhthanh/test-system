package authz

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
)

// OAuth sign-in over HTTP (§11.2). Both routes are browser navigations, so
// they answer with redirects, never JSON: success lands where the user was
// going, MFA goes to the sign-in page's code step, and every failure goes to
// /sign-in?oauth=<reason>.
//
//	GET /v1/session/oauth/{provider}/start?redirect=/p/…  → 302 to the provider
//	GET /v1/session/oauth/{provider}/callback?code&state  → 303 into the app
//
// The state, the PKCE verifier and the landing path travel in one short-
// lived cookie signed with a key derived from the pepper — nothing is
// stored server-side before the provider answers, so the anonymous start
// route cannot fill a table.

const (
	oauthCookie = "atp_oauth"
	// mfaCookie carries an MFA challenge after an OAuth sign-in: the code
	// step reads it server-side, the page never sees it.
	mfaCookie = "atp_mfa"
)

func oauthRoutes(mux httpx.Mux, h *webHandlers) {
	mux.Handle("GET /v1/session/oauth/{provider}/start", http.HandlerFunc(h.oauthStart))
	mux.Handle("GET /v1/session/oauth/{provider}/callback", http.HandlerFunc(h.oauthCallback))
}

func (h *webHandlers) provider(name string) *OAuthProvider {
	for _, p := range h.opts.OAuth {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// providerNames is what the sign-in page offers: only switched-on providers.
func (h *webHandlers) providerNames() []string {
	out := []string{}
	for _, p := range h.opts.OAuth {
		out = append(out, p.Name)
	}
	return out
}

// landing keeps a redirect inside the app: a path, not a URL, and never
// the api itself. Anything else lands on the projects page.
func landing(s string) string {
	if !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/\\") || strings.HasPrefix(s, "/v1/") || len(s) > 512 {
		return "/"
	}
	return s
}

func oauthFail(w http.ResponseWriter, r *http.Request, reason string) {
	http.Redirect(w, r, "/sign-in?oauth="+url.QueryEscape(reason), http.StatusSeeOther)
}

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("authz: crypto/rand: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func (h *webHandlers) signState(s oauthState) string {
	payload, _ := json.Marshal(s)
	p := base64.RawURLEncoding.EncodeToString(payload)
	m := hmac.New(sha256.New, h.opts.StateKey)
	m.Write([]byte(p))
	return p + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (h *webHandlers) openState(v string) (oauthState, bool) {
	p, mac, ok := strings.Cut(v, ".")
	if !ok {
		return oauthState{}, false
	}
	m := hmac.New(sha256.New, h.opts.StateKey)
	m.Write([]byte(p))
	want := base64.RawURLEncoding.EncodeToString(m.Sum(nil))
	if !hmac.Equal([]byte(mac), []byte(want)) {
		return oauthState{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(p)
	var s oauthState
	if err != nil || json.Unmarshal(raw, &s) != nil || time.Now().Unix() > s.Expires {
		return oauthState{}, false
	}
	return s, true
}

func (h *webHandlers) oauthStart(w http.ResponseWriter, r *http.Request) {
	p := h.provider(r.PathValue("provider"))
	if p == nil {
		oauthFail(w, r, "disabled")
		return
	}
	target := landing(r.URL.Query().Get("redirect"))
	if _, ok := PrincipalFrom(r.Context()); ok {
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	st := oauthState{Provider: p.Name, State: randomToken(), Verifier: randomToken(), Redirect: target, Expires: time.Now().Add(oauthStateTTL).Unix()}
	sum := sha256.Sum256([]byte(st.Verifier))
	http.SetCookie(w, &http.Cookie{
		Name: oauthCookie, Value: h.signState(st), Path: "/v1/session/oauth/",
		HttpOnly: true, Secure: h.opts.Secure, SameSite: http.SameSiteLaxMode, MaxAge: int(oauthStateTTL.Seconds()),
	})
	http.Redirect(w, r, p.authorizeURL(st.State, base64.RawURLEncoding.EncodeToString(sum[:])), http.StatusFound)
}

func (h *webHandlers) oauthCallback(w http.ResponseWriter, r *http.Request) {
	// The state cookie is single use, whatever happens next.
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: "", Path: "/v1/session/oauth/", HttpOnly: true, Secure: h.opts.Secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	p := h.provider(r.PathValue("provider"))
	if p == nil {
		oauthFail(w, r, "disabled")
		return
	}
	q := r.URL.Query()
	if q.Get("error") != "" {
		oauthFail(w, r, "denied") // the person said no at the provider
		return
	}
	c, err := r.Cookie(oauthCookie)
	if err != nil {
		oauthFail(w, r, "state")
		return
	}
	st, ok := h.openState(c.Value)
	if !ok || st.Provider != p.Name || subtle.ConstantTimeCompare([]byte(st.State), []byte(q.Get("state"))) != 1 || q.Get("code") == "" {
		oauthFail(w, r, "state")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	client := h.opts.OAuthHTTP
	token, err := p.exchange(ctx, client, q.Get("code"), st.Verifier)
	if err != nil {
		httpx.Cause(r.Context(), err)
		oauthFail(w, r, "provider")
		return
	}
	id, err := p.identity(ctx, client, p, token)
	if err != nil {
		httpx.Cause(r.Context(), err)
		oauthFail(w, r, "provider")
		return
	}
	sess, secret, err := h.svc.LoginWithIdentity(r.Context(), id, SessionMeta{IP: clientIP(r), UserAgent: r.UserAgent()})
	var ch *MFARequired
	switch {
	case err == nil:
		h.setSessionCookies(w, sess, secret)
		http.Redirect(w, r, st.Redirect, http.StatusSeeOther)
	case errors.As(err, &ch):
		http.SetCookie(w, &http.Cookie{
			Name: mfaCookie, Value: ch.Challenge, Path: "/v1/session/mfa",
			HttpOnly: true, Secure: h.opts.Secure, SameSite: http.SameSiteLaxMode, Expires: ch.ExpiresAt,
		})
		http.Redirect(w, r, "/sign-in?step=mfa&redirect="+url.QueryEscape(st.Redirect), http.StatusSeeOther)
	case errors.Is(err, ErrNoAccount):
		oauthFail(w, r, "no_account")
	case errors.Is(err, ErrConflict):
		oauthFail(w, r, "conflict")
	default:
		httpx.Cause(r.Context(), err)
		oauthFail(w, r, "unavailable")
	}
}
