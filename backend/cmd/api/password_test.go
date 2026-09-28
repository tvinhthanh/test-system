package main

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// §11.2 "đổi password → revoke mọi session khác": the signed-in person
// proves the current password, the session that asked stays, every other
// ends; the old password stops working; wrong guesses are rate limited
// like a login; an account without a password is sent to the reset.
func TestChangePassword(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	store := memory.New()
	t.Cleanup(store.Close)
	cfg := config.Config{Env: "dev", TokenPepper: "test-pepper-test-pepper-test-pepper!", DashboardOrigins: []string{origin}, CookieSecure: false}
	handler, _ := buildHandler(cfg, db, store, metric.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	az := authz.New(db, []byte(cfg.TokenPepper))
	u, _ := az.CreateUser(ctx, "pat@example.com")
	_ = az.SetPassword(ctx, u.ID, "the old passphrase", "")
	_ = az.VerifyEmail(ctx, u.ID)
	signIn := func(email, pw string) (*client, resp) {
		c := newClient(t, srv)
		c.bootstrap()
		r := c.post("/v1/session", map[string]string{"email": email, "password": pw})
		c.csrf, _ = c.get("/v1/session").body["csrfToken"].(string)
		return c, r
	}
	here, r := signIn("pat@example.com", "the old passphrase")
	if r.code != 200 {
		t.Fatalf("sign in: %d %s", r.code, r.raw)
	}
	elsewhere, _ := signIn("pat@example.com", "the old passphrase")
	change := func(c *client, current, next string) resp {
		return c.post("/v1/session/password", map[string]string{"current": current, "password": next})
	}

	if r := change(here, "not the passphrase", "a brand new passphrase"); r.code != 400 || r.errCode() != "WRONG_PASSWORD" {
		t.Errorf("wrong current: %d %s", r.code, r.raw)
	}
	if r := change(here, "the old passphrase", "short"); r.code != 400 || r.errCode() != "INVALID_PASSWORD" {
		t.Errorf("too short: %d %s", r.code, r.raw)
	}
	if r := change(here, "the old passphrase", "the old passphrase"); r.code != 400 || r.errCode() != "INVALID_PASSWORD" {
		t.Errorf("same as before: %d %s", r.code, r.raw)
	}
	noCSRF := *here
	noCSRF.csrf = ""
	if r := change(&noCSRF, "the old passphrase", "a brand new passphrase"); r.code != 403 || r.errCode() != "CSRF" {
		t.Errorf("without CSRF: %d %s", r.code, r.raw)
	}
	anon := newClient(t, srv)
	anon.bootstrap()
	anon.csrf, _ = anon.get("/v1/session").body["csrfToken"].(string)
	if r := change(anon, "the old passphrase", "a brand new passphrase"); r.code != 401 {
		t.Errorf("anonymous: %d %s", r.code, r.raw)
	}

	if r := change(here, "the old passphrase", "a brand new passphrase"); r.code != 204 {
		t.Fatalf("change: %d %s", r.code, r.raw)
	}
	// This session stays; the other one is signed out.
	if r := here.get("/v1/session"); r.body["user"] == nil {
		t.Error("the session that changed the password was signed out")
	}
	if r := elsewhere.get("/v1/session"); r.body["user"] != nil {
		t.Error("another session survived the change")
	}
	if _, r := signIn("pat@example.com", "the old passphrase"); r.code != 401 {
		t.Errorf("old password still works: %d", r.code)
	}
	if _, r := signIn("pat@example.com", "a brand new passphrase"); r.code != 200 {
		t.Errorf("new password: %d %s", r.code, r.raw)
	}
	var audited int
	if err := db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM authz_audit WHERE action = 'password.change' AND subject_id = $1`, u.ID).Scan(&audited); err != nil || audited != 1 {
		t.Errorf("audit rows: %d (%v)", audited, err)
	}

	// Guessing the current password from a stolen session is a login in
	// disguise: five wrong tries lock it, even the right one then.
	for i := 0; i < 5; i++ {
		change(here, "guess number one", "whatever passphrase")
	}
	if r := change(here, "a brand new passphrase", "yet another passphrase"); r.code != 429 {
		t.Errorf("after five wrong tries: %d %s", r.code, r.raw)
	}

	// An account with no password (signs in with Google/GitHub only).
	o, _ := az.CreateUser(ctx, "oauth@example.com")
	_ = az.VerifyEmail(ctx, o.ID)
	if err := az.ChangePassword(ctx, o.ID, "", "", "a first passphrase", "198.51.100.7"); err != authz.ErrNoPassword {
		t.Errorf("no password: %v", err)
	}
}
