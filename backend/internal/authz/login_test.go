package authz_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

func TestPasswordHashing(t *testing.T) {
	h, err := authz.HashPasswordForTest("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("hash format: %s", h)
	}
	if !authz.VerifyPasswordForTest(h, "correct horse battery staple") || authz.VerifyPasswordForTest(h, "correct horse battery stapl") {
		t.Error("verify")
	}
	h2, _ := authz.HashPasswordForTest("correct horse battery staple")
	if h2 == h {
		t.Error("salt reuse")
	}
	if _, err := authz.HashPasswordForTest("short"); !errors.Is(err, authz.ErrInvalid) {
		t.Errorf("policy: %v", err)
	}
	if authz.VerifyPasswordForTest("$bcrypt$garbage", "x") || authz.VerifyPasswordForTest("", "x") {
		t.Error("malformed stored hash must verify false, not panic")
	}
}

func TestLoginSessionsAndRateLimit(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	clock := time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC)
	svc := authz.NewForTest(db, pepper, func() time.Time { return clock })
	ctx := context.Background()
	meta := authz.SessionMeta{IP: "203.0.113.7", UserAgent: "test"}

	u, err := svc.CreateUser(ctx, "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPassword(ctx, u.ID, "correct horse battery staple", ""); err != nil {
		t.Fatal(err)
	}
	// Unverified email: refused with the same error as a wrong password.
	if _, _, err := svc.Login(ctx, "alice@example.com", "correct horse battery staple", meta); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("unverified login: %v", err)
	}
	if err := svc.VerifyEmail(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if u, _ := svc.GetUser(ctx, u.ID); u.EmailVerifiedAt.IsZero() || !u.HasPassword {
		t.Errorf("user after verify: %+v", u)
	}
	if _, _, err := svc.Login(ctx, "alice@example.com", "wrong password here", meta); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("wrong password: %v", err)
	}
	if _, _, err := svc.Login(ctx, "nobody@example.com", "correct horse battery staple", meta); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("unknown email must look like a wrong password: %v", err)
	}

	sess, secret, err := svc.Login(ctx, " Alice@Example.com ", "correct horse battery staple", meta)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, "atp_sess_") || sess.UserID != u.ID || sess.IP != meta.IP || sess.ExpiresAt.Sub(clock) != 30*24*time.Hour {
		t.Errorf("session: %+v secret=%s", sess, secret[:12])
	}
	p, got, err := svc.AuthenticateSession(ctx, secret)
	if err != nil || p.Kind != authz.PrincipalUser || p.UserID != u.ID || got.ID != sess.ID {
		t.Errorf("authenticate: %+v %+v %v", p, got, err)
	}
	if _, _, err := svc.AuthenticateSession(ctx, "atp_sess_"+strings.Repeat("0", 64)); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("unknown session: %v", err)
	}
	// The secret is stored hashed: it never appears in the table.
	var leaked bool
	_ = db.Q(ctx).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sessions WHERE encode(token_hash, 'hex') = $1)`, strings.TrimPrefix(secret, "atp_sess_")).Scan(&leaked)
	if leaked {
		t.Fatal("session secret stored in clear")
	}

	// Password change revokes other sessions but keeps the current one.
	sess2, secret2, _ := svc.Login(ctx, "alice@example.com", "correct horse battery staple", meta)
	if err := svc.SetPassword(ctx, u.ID, "another long passphrase!", sess2.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AuthenticateSession(ctx, secret); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Error("old session survived a password change")
	}
	if _, _, err := svc.AuthenticateSession(ctx, secret2); err != nil {
		t.Errorf("kept session revoked: %v", err)
	}
	if _, _, err := svc.Login(ctx, "alice@example.com", "correct horse battery staple", meta); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Error("old password still works")
	}
	if err := svc.Logout(ctx, sess2.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AuthenticateSession(ctx, secret2); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Error("session usable after logout")
	}
	// Expiry.
	_, secret3, _ := svc.Login(ctx, "alice@example.com", "another long passphrase!", meta)
	clock = clock.Add(31 * 24 * time.Hour)
	if _, _, err := svc.AuthenticateSession(ctx, secret3); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Error("expired session accepted")
	}
	clock = clock.Add(-31 * 24 * time.Hour)

	// Rate limit (§11.2): five failures in 15 min lock the account AND the
	// IP; a correct password is refused while locked; the lock lifts.
	for i := 0; i < 5; i++ {
		if _, _, err := svc.Login(ctx, "alice@example.com", "wrong wrong wrong wrong", meta); !errors.Is(err, authz.ErrUnauthenticated) {
			t.Fatalf("failure %d: %v", i, err)
		}
	}
	if _, _, err := svc.Login(ctx, "alice@example.com", "another long passphrase!", meta); !errors.Is(err, authz.ErrRateLimited) {
		t.Errorf("6th attempt with the right password: %v, want rate limited", err)
	}
	if _, _, err := svc.Login(ctx, "alice@example.com", "another long passphrase!", authz.SessionMeta{IP: "198.51.100.9"}); !errors.Is(err, authz.ErrRateLimited) {
		t.Errorf("account lock must hold from another IP: %v", err)
	}
	if _, _, err := svc.Login(ctx, "bob@example.com", "whatever whatever", meta); !errors.Is(err, authz.ErrRateLimited) {
		t.Errorf("IP lock must hold for another account: %v", err)
	}
	clock = clock.Add(16 * time.Minute)
	if _, _, err := svc.Login(ctx, "alice@example.com", "another long passphrase!", meta); err != nil {
		t.Errorf("after the lock: %v", err)
	}
	sessions, _ := svc.ListSessions(ctx, u.ID)
	if len(sessions) < 4 {
		t.Errorf("sessions listed: %d", len(sessions))
	}
}

func TestWebLoginFlowWithCSRF(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	svc := authz.New(db, pepper)
	ctx := context.Background()
	u, _ := svc.CreateUser(ctx, "alice@example.com")
	_ = svc.SetPassword(ctx, u.ID, "correct horse battery staple", "")
	_ = svc.VerifyEmail(ctx, u.ID)

	opts := authz.WebOptions{AllowedOrigins: []string{"https://app.example"}, Secure: true}
	mux := http.NewServeMux()
	authz.AuthRoutes(mux, svc, opts)
	mux.Handle("POST /v1/thing", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authz.PrincipalFrom(r.Context()); !ok {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(204)
	}))
	srv := httptest.NewServer(authz.SessionMiddleware(svc, opts)(mux))
	defer srv.Close()

	do := func(method, path string, body any, cookies []*http.Cookie, headers map[string]string) (*http.Response, map[string]any) {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req, _ := http.NewRequest(method, srv.URL+path, &buf)
		req.Header.Set("Content-Type", "application/json")
		for _, c := range cookies {
			req.AddCookie(c)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		return res, m
	}
	creds := map[string]string{"email": "alice@example.com", "password": "correct horse battery staple"}

	// Anonymous bootstrap: 200, user null, and it issues the CSRF cookie the
	// login POST must echo (double submit from the very first request).
	res, body := do("GET", "/v1/session", nil, nil, nil)
	if res.StatusCode != 200 || body["user"] != nil || body["csrfToken"] == "" {
		t.Fatalf("bootstrap: %d %v", res.StatusCode, body)
	}
	var bootCSRF *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == authz.CSRFCookie {
			bootCSRF = c
		}
	}
	if bootCSRF == nil || bootCSRF.HttpOnly || bootCSRF.Value != body["csrfToken"] {
		t.Fatalf("bootstrap csrf cookie: %+v", bootCSRF)
	}
	// A second bootstrap with the cookie reuses the token so parallel tabs agree.
	if res, m := do("GET", "/v1/session", nil, []*http.Cookie{bootCSRF}, nil); res.StatusCode != 200 || m["csrfToken"] != bootCSRF.Value {
		t.Errorf("bootstrap reuse: %d %v", res.StatusCode, m)
	}
	loginHdr := map[string]string{"Origin": "https://app.example", authz.CSRFHeader: bootCSRF.Value}
	pre := []*http.Cookie{bootCSRF}

	// Cross-site login attempt is refused by the origin allowlist.
	if res, _ := do("POST", "/v1/session", creds, pre, map[string]string{"Origin": "https://evil.example", authz.CSRFHeader: bootCSRF.Value}); res.StatusCode != 403 {
		t.Errorf("foreign origin login: %d", res.StatusCode)
	}
	// Login without the bootstrap token is refused even from the right origin.
	if res, _ := do("POST", "/v1/session", creds, nil, map[string]string{"Origin": "https://app.example"}); res.StatusCode != 403 {
		t.Errorf("login without CSRF: %d", res.StatusCode)
	}
	if res, m := do("POST", "/v1/session", map[string]string{"email": "alice@example.com", "password": "nope nope nope nope"}, pre, loginHdr); res.StatusCode != 401 || m["error"].(map[string]any)["code"] != "INVALID_CREDENTIALS" {
		t.Errorf("bad password: %d %v", res.StatusCode, m)
	}
	res, body = do("POST", "/v1/session", creds, pre, loginHdr)
	user, _ := body["user"].(map[string]any)
	if res.StatusCode != 200 || user["id"] != u.ID || user["email"] != "alice@example.com" || user["name"] != "alice" || body["csrfToken"] == "" || body["csrfToken"] == bootCSRF.Value {
		t.Fatalf("login: %d %v", res.StatusCode, body)
	}
	var sessionCookie, csrfCookie *http.Cookie
	for _, c := range res.Cookies() {
		switch c.Name {
		case authz.SessionCookie:
			sessionCookie = c
		case authz.CSRFCookie:
			csrfCookie = c
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || !sessionCookie.Secure || sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie: %+v", sessionCookie)
	}
	if csrfCookie == nil || csrfCookie.HttpOnly || csrfCookie.Value != body["csrfToken"] {
		t.Fatalf("csrf cookie: %+v", csrfCookie)
	}
	cookies := []*http.Cookie{sessionCookie, csrfCookie}

	if res, m := do("GET", "/v1/session", nil, cookies, nil); res.StatusCode != 200 || m["user"] == nil || m["user"].(map[string]any)["email"] != "alice@example.com" || m["csrfToken"] != csrfCookie.Value {
		t.Errorf("session after login: %d %v", res.StatusCode, m)
	}
	if res, m := do("GET", "/v1/session", nil, nil, nil); res.StatusCode != 200 || m["user"] != nil {
		t.Errorf("session without cookie: %d %v", res.StatusCode, m)
	}

	// State change with the session: origin + CSRF token both required.
	if res, _ := do("POST", "/v1/thing", nil, cookies, map[string]string{"Origin": "https://app.example", authz.CSRFHeader: csrfCookie.Value}); res.StatusCode != 204 {
		t.Errorf("proper CSRF: %d", res.StatusCode)
	}
	if res, _ := do("POST", "/v1/thing", nil, cookies, map[string]string{"Origin": "https://app.example"}); res.StatusCode != 403 {
		t.Errorf("missing token: %d", res.StatusCode)
	}
	if res, _ := do("POST", "/v1/thing", nil, cookies, map[string]string{"Origin": "https://evil.example", authz.CSRFHeader: csrfCookie.Value}); res.StatusCode != 403 {
		t.Errorf("foreign origin with token: %d", res.StatusCode)
	}
	if res, _ := do("POST", "/v1/thing", nil, cookies, map[string]string{"Referer": "https://app.example/runs", authz.CSRFHeader: csrfCookie.Value}); res.StatusCode != 204 {
		t.Errorf("referer fallback: %d", res.StatusCode)
	}
	if res, _ := do("POST", "/v1/thing", nil, []*http.Cookie{sessionCookie}, map[string]string{"Origin": "https://app.example", authz.CSRFHeader: csrfCookie.Value}); res.StatusCode != 403 {
		t.Errorf("token without cookie: %d", res.StatusCode)
	}

	// Logout revokes and clears.
	res, _ = do("POST", "/v1/session/logout", nil, cookies, map[string]string{"Origin": "https://app.example", authz.CSRFHeader: csrfCookie.Value})
	if res.StatusCode != 204 {
		t.Errorf("logout: %d", res.StatusCode)
	}
	for _, c := range res.Cookies() {
		if c.MaxAge >= 0 {
			t.Errorf("cookie %s not cleared: %+v", c.Name, c)
		}
	}
	if res, m := do("GET", "/v1/session", nil, cookies, nil); res.StatusCode != 200 || m["user"] != nil {
		t.Errorf("session after logout: %d %v", res.StatusCode, m)
	}
}
