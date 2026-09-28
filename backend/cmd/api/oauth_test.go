package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// fakeProvider plays Google and GitHub: it hands out a code for whoever the
// test says is signing in, checks PKCE on the exchange, and answers the
// identity endpoints for the token it issued.
type fakeProvider struct {
	mu        sync.Mutex
	challenge map[string]string // code → code_challenge seen at authorize
	who       map[string]fakeUser
}

type fakeUser struct {
	sub, email string
	verified   bool
}

func (f *fakeProvider) issue(challenge string, u fakeUser) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	code := "code-" + u.sub + "-" + challenge[:8]
	f.challenge[code] = challenge
	f.who["tok-"+code] = u
	return code
}

func (f *fakeProvider) handler() http.Handler {
	mux := http.NewServeMux()
	token := func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		want, ok := f.challenge[r.Form.Get("code")]
		f.mu.Unlock()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != want || r.Form.Get("client_secret") == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "tok-" + r.Form.Get("code"), "token_type": "bearer"})
	}
	user := func(r *http.Request) (fakeUser, bool) {
		f.mu.Lock()
		defer f.mu.Unlock()
		u, ok := f.who[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		return u, ok
	}
	mux.HandleFunc("POST /google/token", token)
	mux.HandleFunc("POST /github/token", token)
	mux.HandleFunc("GET /google/v1/userinfo", func(w http.ResponseWriter, r *http.Request) {
		u, ok := user(r)
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sub": u.sub, "email": u.email, "email_verified": u.verified})
	})
	mux.HandleFunc("GET /github/user", func(w http.ResponseWriter, r *http.Request) {
		u, ok := user(r)
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"id":` + u.sub + `,"login":"x"}`))
	})
	mux.HandleFunc("GET /github/user/emails", func(w http.ResponseWriter, r *http.Request) {
		u, _ := user(r)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"email": "old@example.com", "primary": false, "verified": true},
			{"email": u.email, "primary": true, "verified": u.verified},
		})
	})
	return mux
}

// §11.2 "OAuth Google/GitHub": switched on per provider, sign-in by the
// provider's stable subject, first link by a verified email only, no
// self-signup, MFA still asked, PKCE and state enforced.
func TestOAuthSignIn(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	store := memory.New()
	t.Cleanup(store.Close)

	fake := &fakeProvider{challenge: map[string]string{}, who: map[string]fakeUser{}}
	fsrv := httptest.NewServer(fake.handler())
	t.Cleanup(fsrv.Close)
	prev := oauthProviders
	t.Cleanup(func() { oauthProviders = prev })
	oauthProviders = func(cfg config.Config) []*authz.OAuthProvider {
		out := prev(cfg)
		for _, p := range out {
			p.AuthURL, p.TokenURL, p.APIBase = fsrv.URL+"/"+p.Name+"/authorize", fsrv.URL+"/"+p.Name+"/token", fsrv.URL+"/"+p.Name
		}
		return out
	}
	cfg := config.Config{
		Env: "dev", TokenPepper: "test-pepper-test-pepper-test-pepper!", DashboardOrigins: []string{origin}, CookieSecure: false,
		OAuthGoogle: config.OAuthClient{Enabled: true, ClientID: "gid", ClientSecret: "gsecret"},
		OAuthGitHub: config.OAuthClient{Enabled: true, ClientID: "hid", ClientSecret: "hsecret"},
		PublicURL:   "http://atp.test",
	}
	handler, _ := buildHandler(cfg, db, store, metric.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	ten, az := tenancy.New(db), authz.New(db, []byte(cfg.TokenPepper))
	org, _ := ten.CreateOrganization(ctx, "Acme", "acme")
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Payments")
	p, _ := ten.CreateProject(ctx, ws.ID, "checkout", "")
	account := func(email string, verified bool) authz.User {
		u, _ := az.CreateUser(ctx, email)
		if verified {
			_ = az.VerifyEmail(ctx, u.ID)
		}
		_, _ = az.GrantRole(ctx, u.ID, authz.ScopeRef{Level: authz.LevelProject, ID: string(p.ID)}, authz.RoleMember)
		return u
	}
	alice := account("alice@example.com", true)
	bob := account("bob@example.com", true)
	account("unverified@example.com", false)

	// signIn runs the browser's side: start → (provider) → callback. It
	// returns the callback's Location and the browser.
	signIn := func(provider string, u fakeUser, redirect string, tamper func(q url.Values)) (string, *client) {
		t.Helper()
		c := newClient(t, srv)
		c.bootstrap()
		resp, err := c.http.Get(srv.URL + "/v1/session/oauth/" + provider + "/start?redirect=" + url.QueryEscape(redirect))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		loc, _ := url.Parse(resp.Header.Get("Location"))
		q := loc.Query()
		if resp.StatusCode != 302 || !strings.HasPrefix(loc.String(), fsrv.URL+"/"+provider+"/authorize?") ||
			q.Get("redirect_uri") != "http://atp.test/v1/session/oauth/"+provider+"/callback" || q.Get("code_challenge_method") != "S256" || q.Get("state") == "" {
			t.Fatalf("start %s: %d %s", provider, resp.StatusCode, loc)
		}
		cb := url.Values{"code": {fake.issue(q.Get("code_challenge"), u)}, "state": {q.Get("state")}}
		if tamper != nil {
			tamper(cb)
		}
		resp, err = c.http.Get(srv.URL + "/v1/session/oauth/" + provider + "/callback?" + cb.Encode())
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 303 {
			t.Fatalf("callback %s: %d", provider, resp.StatusCode)
		}
		c.csrf, _ = c.get("/v1/session").body["csrfToken"].(string)
		return resp.Header.Get("Location"), c
	}

	if r := newClient(t, srv).get("/v1/session"); !strings.Contains(r.raw, `"providers":["google","github"]`) {
		t.Errorf("bootstrap providers: %s", r.raw)
	}

	// Google, first time: linked by the verified email, lands where it was going.
	loc, c := signIn("google", fakeUser{sub: "g-1001", email: "Alice@Example.com", verified: true}, "/p/"+string(p.ID)+"/runs", nil)
	if loc != "/p/"+string(p.ID)+"/runs" {
		t.Errorf("landing: %s", loc)
	}
	if r := c.get("/v1/session"); r.body["user"] == nil || r.body["user"].(map[string]any)["id"] != alice.ID {
		t.Fatalf("google sign-in: %s", r.raw)
	}
	if r := c.get("/v1/projects"); r.code != 200 {
		t.Errorf("projects after google sign-in: %d", r.code)
	}
	// Later: the subject decides, not the email the provider reports now.
	loc, c = signIn("google", fakeUser{sub: "g-1001", email: "changed@elsewhere.example", verified: true}, "", nil)
	if r := c.get("/v1/session"); loc != "/" || r.body["user"].(map[string]any)["id"] != alice.ID {
		t.Errorf("returning subject: %s %s", loc, r.raw)
	}
	// A second Google account with alice's email cannot take her over.
	if loc, _ := signIn("google", fakeUser{sub: "g-2002", email: "alice@example.com", verified: true}, "", nil); loc != "/sign-in?oauth=conflict" {
		t.Errorf("second google account on the same email: %s", loc)
	}
	// GitHub: the primary verified address links bob.
	loc, c = signIn("github", fakeUser{sub: "4242", email: "bob@example.com", verified: true}, "", nil)
	if r := c.get("/v1/session"); r.body["user"] == nil || r.body["user"].(map[string]any)["id"] != bob.ID {
		t.Errorf("github sign-in: %s %s", loc, r.raw)
	}

	// Linked sign-ins: each person sees and unlinks only their own, and the
	// last way in (alice has no password) stays.
	_, ac := signIn("github", fakeUser{sub: "5150", email: "alice@example.com", verified: true}, "", nil)
	ids := ac.get("/v1/session/identities")
	if len(ids.items()) != 2 {
		t.Fatalf("alice's identities: %s", ids.raw)
	}
	byProvider := map[string]string{}
	for _, it := range ids.items() {
		m := it.(map[string]any)
		byProvider[m["provider"].(string)] = m["id"].(string)
	}
	if r := c.do("DELETE", "/v1/session/identities/"+byProvider["google"], nil, nil); r.code != 404 {
		t.Errorf("bob unlinking alice's google: %d %s", r.code, r.raw)
	}
	if r := ac.do("DELETE", "/v1/session/identities/"+byProvider["google"], nil, nil); r.code != 204 {
		t.Errorf("unlink google: %d %s", r.code, r.raw)
	}
	if r := ac.do("DELETE", "/v1/session/identities/"+byProvider["github"], nil, nil); r.code != 409 || r.errCode() != "LAST_SIGN_IN" {
		t.Errorf("unlink the last way in: %d %s", r.code, r.raw)
	}
	if r := ac.get("/v1/session/identities"); len(r.items()) != 1 {
		t.Errorf("after unlinking: %s", r.raw)
	}
	if r := newClient(t, srv).get("/v1/session/identities"); r.code != 401 {
		t.Errorf("identities without a session: %d", r.code)
	}

	// Who is refused, and how the page is told.
	for name, tc := range map[string]struct {
		provider string
		u        fakeUser
		tamper   func(url.Values)
		want     string
	}{
		"no account for the email":         {"google", fakeUser{sub: "g-9", email: "stranger@example.com", verified: true}, nil, "no_account"},
		"provider email not verified":      {"google", fakeUser{sub: "g-10", email: "bob@example.com", verified: false}, nil, "no_account"},
		"account here not verified":        {"github", fakeUser{sub: "77", email: "unverified@example.com", verified: true}, nil, "no_account"},
		"state from another flow":          {"google", fakeUser{sub: "g-1001"}, func(q url.Values) { q.Set("state", "forged") }, "state"},
		"the person said no":               {"google", fakeUser{sub: "g-1001"}, func(q url.Values) { q.Del("code"); q.Set("error", "access_denied") }, "denied"},
		"a code the provider never issued": {"google", fakeUser{sub: "g-1001"}, func(q url.Values) { q.Set("code", "made-up") }, "provider"},
	} {
		if loc, _ := signIn(tc.provider, tc.u, "", tc.tamper); loc != "/sign-in?oauth="+tc.want {
			t.Errorf("%s: %s", name, loc)
		}
	}
	// A callback without the state cookie (another browser) is refused.
	resp, _ := newClient(t, srv).http.Get(srv.URL + "/v1/session/oauth/google/callback?code=x&state=y")
	if resp.Header.Get("Location") != "/sign-in?oauth=state" {
		t.Errorf("callback without the cookie: %s", resp.Header.Get("Location"))
	}
	// Redirects stay inside the app.
	for _, evil := range []string{"https://evil.example/", "//evil.example", "/v1/projects"} {
		if loc, _ := signIn("google", fakeUser{sub: "g-1001", email: "alice@example.com", verified: true}, evil, nil); loc != "/" {
			t.Errorf("redirect %q landed on %s", evil, loc)
		}
	}
	// An unknown provider is simply off.
	resp, _ = newClient(t, srv).http.Get(srv.URL + "/v1/session/oauth/gitlab/start")
	if resp.Header.Get("Location") != "/sign-in?oauth=disabled" {
		t.Errorf("unknown provider: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}

	// MFA still applies: the callback lands on the code step with the
	// challenge in an HttpOnly cookie; the code alone then opens the session.
	setup, _ := az.BeginMFASetup(ctx, alice.ID)
	codes, err := az.EnableMFA(ctx, alice.ID, authenticator(t, setup.Secret, time.Now()), "")
	if err != nil {
		t.Fatal(err)
	}
	loc, c = signIn("google", fakeUser{sub: "g-1001", email: "alice@example.com", verified: true}, "/p/"+string(p.ID)+"/runs", nil)
	if !strings.HasPrefix(loc, "/sign-in?step=mfa&redirect=%2Fp%2F") {
		t.Fatalf("oauth with MFA on: %s", loc)
	}
	if r := c.get("/v1/projects"); r.code != 401 {
		t.Errorf("projects before the second factor: %d", r.code)
	}
	if r := c.post("/v1/session/mfa", map[string]any{"code": codes[0]}); r.code != 200 || r.body["user"] == nil {
		t.Fatalf("code step after oauth: %d %s", r.code, r.raw)
	}
	if r := c.get("/v1/projects"); r.code != 200 {
		t.Errorf("projects after the second factor: %d", r.code)
	}

	// Every link is on the record.
	audit, _ := az.ListAudit(ctx, "", 50)
	count := map[string]int{}
	for _, a := range audit {
		count[a.Action]++
	}
	// alice/google, bob/github, alice/github, alice/google again after unlinking.
	if count["identity.link"] != 4 || count["identity.unlink"] != 1 {
		t.Errorf("identity audit: %v", count)
	}
}

// A switched-off provider has no routes that work and no button.
func TestOAuthSwitchedOff(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	store := memory.New()
	t.Cleanup(store.Close)
	cfg := config.Config{
		Env: "dev", TokenPepper: "test-pepper-test-pepper-test-pepper!", DashboardOrigins: []string{origin}, CookieSecure: false,
		OAuthGoogle: config.OAuthClient{Enabled: true, ClientID: "gid", ClientSecret: "gsecret"},
		PublicURL:   "http://atp.test",
	}
	handler, _ := buildHandler(cfg, db, store, metric.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	if r := newClient(t, srv).get("/v1/session"); !strings.Contains(r.raw, `"providers":["google"]`) {
		t.Errorf("bootstrap with github off: %s", r.raw)
	}
	resp, _ := newClient(t, srv).http.Get(srv.URL + "/v1/session/oauth/github/start")
	if resp.Header.Get("Location") != "/sign-in?oauth=disabled" {
		t.Errorf("github start while off: %s", resp.Header.Get("Location"))
	}
	resp, _ = newClient(t, srv).http.Get(srv.URL + "/v1/session/oauth/google/start")
	if !strings.HasPrefix(resp.Header.Get("Location"), "https://accounts.google.com/o/oauth2/v2/auth?") {
		t.Errorf("google start while on: %s", resp.Header.Get("Location"))
	}
	cfg.OAuthGoogle.Enabled = false
	handler, _ = buildHandler(cfg, db, store, metric.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	off := httptest.NewServer(handler)
	t.Cleanup(off.Close)
	if r := newClient(t, off).get("/v1/session"); !strings.Contains(r.raw, `"providers":[]`) {
		t.Errorf("bootstrap with everything off: %s", r.raw)
	}
}
