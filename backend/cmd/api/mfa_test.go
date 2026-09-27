package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
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

// authenticator is what a phone app does with the setup secret — written
// here from RFC 6238 on purpose, so the test checks interoperability rather
// than the server agreeing with itself.
func authenticator(t *testing.T, secretB32 string, at time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretB32)
	if err != nil {
		t.Fatalf("secret is not base32: %v", err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(at.Unix()/30))
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[19] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[off:off+4])&0x7fffffff)%1_000_000)
}

// §11.2 "MFA TOTP cho org owner" over HTTP: an owner without MFA is held at
// the setup screen, enrols with an authenticator, then signs in in two steps.
func TestOwnerMFAOverHTTP(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	store := memory.New()
	t.Cleanup(store.Close)
	cfg := config.Config{Env: "dev", TokenPepper: "test-pepper-test-pepper-test-pepper!", DashboardOrigins: []string{origin}, CookieSecure: false}
	handler, _ := buildHandler(cfg, db, store, metric.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	ten, az := tenancy.New(db), authz.New(db, []byte(cfg.TokenPepper))
	org, _ := ten.CreateOrganization(ctx, "Acme", "acme")
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Payments")
	p, _ := ten.CreateProject(ctx, ws.ID, "checkout", "")
	u, _ := az.CreateUser(ctx, "owner@example.com")
	const pw = "owner passphrase long enough"
	_ = az.SetPassword(ctx, u.ID, pw, "")
	_ = az.VerifyEmail(ctx, u.ID)
	if _, err := az.GrantRole(ctx, u.ID, authz.ScopeRef{Level: authz.LevelOrganization, ID: string(org.ID)}, authz.RoleOrgOwner); err != nil {
		t.Fatal(err)
	}
	signIn := func(c *client) resp {
		c.bootstrap()
		return c.post("/v1/session", map[string]string{"email": "owner@example.com", "password": pw})
	}

	anon := newClient(t, srv)
	if r := anon.get("/v1/session/mfa"); r.code != 401 {
		t.Errorf("status without a session: %d %s", r.code, r.raw)
	}

	// Without MFA the owner signs in but reaches only session + MFA routes.
	c := newClient(t, srv)
	if r := signIn(c); r.code != 200 {
		t.Fatalf("login: %d %s", r.code, r.raw)
	}
	c.csrf, _ = c.get("/v1/session").body["csrfToken"].(string)
	if r := c.get("/v1/session"); !strings.Contains(r.raw, `"mfa":{"enabled":false,"required":true}`) {
		t.Errorf("bootstrap flags: %s", r.raw)
	}
	for _, path := range []string{"/v1/projects", "/v1/projects/" + string(p.ID) + "/runs"} {
		if r := c.get(path); r.code != 403 || r.errCode() != "MFA_ENROLLMENT_REQUIRED" {
			t.Errorf("%s before enrolment: %d %s", path, r.code, r.raw)
		}
	}
	setup := c.post("/v1/session/mfa/setup", map[string]any{})
	secret, _ := setup.body["secret"].(string)
	if setup.code != 200 || len(secret) != 32 || !strings.HasPrefix(setup.body["otpauthUri"].(string), "otpauth://totp/ATP:owner@example.com?") {
		t.Fatalf("setup: %d %s", setup.code, setup.raw)
	}
	if r := c.post("/v1/session/mfa/enable", map[string]any{"code": "000000"}); r.code != 400 || r.errCode() != "INVALID_CODE" {
		t.Errorf("enable with a wrong code: %d %s", r.code, r.raw)
	}
	en := c.post("/v1/session/mfa/enable", map[string]any{"code": authenticator(t, secret, time.Now())})
	codes, _ := en.body["recoveryCodes"].([]any)
	if en.code != 200 || len(codes) != 10 {
		t.Fatalf("enable: %d %s", en.code, en.raw)
	}
	// The session that enrolled survives and now reaches the tenant.
	if r := c.get("/v1/projects"); r.code != 200 {
		t.Errorf("projects after enrolment: %d %s", r.code, r.raw)
	}
	if r := c.post("/v1/session/mfa/disable", map[string]any{"code": codes[0]}); r.code != 409 || r.errCode() != "MFA_MANDATORY" {
		t.Errorf("owner disabling: %d %s", r.code, r.raw)
	}

	// A new browser: password → 202 + challenge, no cookie; code → session.
	b := newClient(t, srv)
	r := signIn(b)
	challenge, _ := r.body["challenge"].(string)
	if r.code != 202 || r.body["mfa"] != "required" || challenge == "" {
		t.Fatalf("login with MFA on: %d %s", r.code, r.raw)
	}
	if r := b.get("/v1/projects"); r.code != 401 {
		t.Errorf("projects after the password alone: %d %s", r.code, r.raw)
	}
	if r := b.post("/v1/session/mfa", map[string]any{"challenge": challenge, "code": "000000"}); r.code != 400 || r.errCode() != "INVALID_CODE" {
		t.Errorf("wrong code: %d %s", r.code, r.raw)
	}
	// A recovery code stands in for the app (the TOTP step was spent on enable).
	done := b.post("/v1/session/mfa", map[string]any{"challenge": challenge, "code": strings.ToUpper(codes[1].(string))})
	if done.code != 200 || done.body["user"] == nil {
		t.Fatalf("complete: %d %s", done.code, done.raw)
	}
	if r := b.get("/v1/projects"); r.code != 200 {
		t.Errorf("projects after the second factor: %d %s", r.code, r.raw)
	}
	if r := b.get("/v1/session/mfa"); r.body["enabled"] != true || r.body["required"] != true || r.body["recoveryCodesLeft"] != float64(9) {
		t.Errorf("status: %s", r.raw)
	}
	// The challenge is single use; the second step is CSRF-guarded like the first.
	x := newClient(t, srv)
	x.bootstrap()
	if r := x.post("/v1/session/mfa", map[string]any{"challenge": challenge, "code": codes[2]}); r.code != 401 || r.errCode() != "MFA_CHALLENGE_EXPIRED" {
		t.Errorf("challenge reused: %d %s", r.code, r.raw)
	}
	x.csrf = ""
	if r := x.post("/v1/session/mfa", map[string]any{"challenge": challenge, "code": codes[2]}); r.code != 403 || r.errCode() != "CSRF" {
		t.Errorf("second step without CSRF: %d %s", r.code, r.raw)
	}
}
