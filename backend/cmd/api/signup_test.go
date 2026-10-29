package main

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/mail"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

var signupLink = regexp.MustCompile(`/signup#(atp_sgn_[A-Za-z0-9_-]+)`)

// Self-serve sign-up over HTTP (QĐ-17): the form answers the same for any
// address and never waits for the mail; only the mailed link creates the
// account; the admin's queue shows it only then; off answers SIGNUP_OFF.
func TestSignup(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	store := memory.New()
	t.Cleanup(store.Close)
	box := &outbox{}
	prev := newMailer
	t.Cleanup(func() { newMailer = prev })
	newMailer = func(cfg config.Config) mail.Sender {
		if !cfg.Mail.Enabled {
			return nil
		}
		return box
	}
	cfg := config.Config{
		Env: "dev", TokenPepper: "test-pepper-test-pepper-test-pepper!", DashboardOrigins: []string{origin}, CookieSecure: false,
		Mail: config.Mail{Enabled: true}, PublicURL: "https://atp.example", Signup: true,
	}
	handler, _ := buildHandler(cfg, db, store, metric.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	ten, az := tenancy.New(db), authz.New(db, []byte(cfg.TokenPepper))
	org, _ := ten.CreateOrganization(ctx, "Acme", "acme")
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Payments")
	p, _ := ten.CreateProject(ctx, ws.ID, "checkout", "")
	admin, _ := az.CreateUser(ctx, "dana@example.com")
	_ = az.SetPassword(ctx, admin.ID, "dana passphrase long", "")
	_ = az.VerifyEmail(ctx, admin.ID)
	_, _ = az.GrantRole(ctx, admin.ID, authz.ScopeRef{Level: authz.LevelWorkspace, ID: string(ws.ID)}, authz.RoleWorkspaceAdmin)
	queue := "/v1/projects/" + string(p.ID) + "/access-requests"
	form := func(email string) map[string]any {
		return map[string]any{"email": email, "scopeLevel": "project", "scopeId": string(p.ID), "requestedRole": "member", "note": "QA contractor"}
	}

	anon := newClient(t, srv)
	if r := anon.bootstrap(); r.body["signup"] != true {
		t.Errorf("bootstrap: %s", r.raw)
	}
	// The answer never waits for the mail.
	box.gate = make(chan struct{})
	started := time.Now()
	if r := anon.post("/v1/session/signup", form("new.person@example.com")); r.code != 202 {
		t.Fatalf("signup with the mail server held: %d %s", r.code, r.raw)
	}
	if waited := time.Since(started); waited > 2*time.Second {
		t.Errorf("signup waited %s for work that depends on the address", waited)
	}
	close(box.gate)
	m := box.waitMail(t, 1)
	box.gate = nil
	link := signupLink.FindStringSubmatch(m.Text)
	if m.To != "new.person@example.com" || link == nil || !strings.Contains(m.Text, "https://atp.example/signup#atp_sgn_") || strings.Contains(m.Text, "checkout") {
		t.Fatalf("signup mail: %+v", m)
	}

	// An address with an account gets the same answer and no mail — no
	// "account exists" for anyone to read.
	known := anon.post("/v1/session/signup", form("Dana@Example.com"))
	unknown := anon.post("/v1/session/signup", form("someone.else@example.com"))
	if known.code != 202 || unknown.code != 202 || known.raw != unknown.raw {
		t.Errorf("answers differ: %d %s / %d %s", known.code, known.raw, unknown.code, unknown.raw)
	}
	box.waitMail(t, 2)
	time.Sleep(100 * time.Millisecond)
	if box.count() != 2 {
		t.Errorf("mails: %d, want 2 (none for the address with an account)", box.count())
	}
	if r := anon.post("/v1/session/signup", map[string]any{"email": "x@example.com", "scopeLevel": "project", "scopeId": string(p.ID), "requestedRole": "org_owner"}); r.code != 400 || r.errCode() != "INVALID_SIGNUP" {
		t.Errorf("role at the wrong level: %d %s", r.code, r.raw)
	}

	// Nothing for the admin to decide until the link is opened.
	dana := newClient(t, srv)
	dana.bootstrap()
	dana.post("/v1/session", map[string]string{"email": "dana@example.com", "password": "dana passphrase long"})
	dana.csrf, _ = dana.get("/v1/session").body["csrfToken"].(string)
	if r := dana.get(queue); r.code != 200 || len(r.items()) != 0 {
		t.Fatalf("queue before the link: %d %s", r.code, r.raw)
	}

	// The link: weak password spends nothing, then signed in, reaching nothing.
	guest := newClient(t, srv)
	guest.bootstrap()
	if r := guest.post("/v1/session/signup/complete", map[string]any{"token": link[1], "password": "short"}); r.code != 400 || r.errCode() != "INVALID_PASSWORD" {
		t.Errorf("weak password: %d %s", r.code, r.raw)
	}
	if r := guest.post("/v1/session/signup/complete", map[string]any{"token": link[1], "password": "a long enough passphrase"}); r.code != 200 || r.body["user"] == nil {
		t.Fatalf("complete: %d %s", r.code, r.raw)
	}
	if r := guest.get("/v1/projects"); r.code != 200 || len(r.items()) != 0 {
		t.Errorf("projects before approval: %d %s", r.code, r.raw)
	}
	again := newClient(t, srv)
	again.bootstrap()
	if r := again.post("/v1/session/signup/complete", map[string]any{"token": link[1], "password": "a long enough passphrase"}); r.code != 404 || r.errCode() != "SIGNUP_NOT_FOUND" {
		t.Errorf("link used twice: %d %s", r.code, r.raw)
	}

	// The admin approves; the new account sees the project.
	r := dana.get(queue)
	items := r.items()
	if len(items) != 1 {
		t.Fatalf("queue after the link: %s", r.raw)
	}
	item := items[0].(map[string]any)
	if item["email"] != "new.person@example.com" || item["requestedRole"] != "member" || item["note"] != "QA contractor" {
		t.Errorf("request: %+v", item)
	}
	if r := dana.post(queue+"/"+item["id"].(string)+"/decide", map[string]any{"decision": "approved"}); r.code != 204 {
		t.Fatalf("approve: %d %s", r.code, r.raw)
	}
	if r := guest.get("/v1/projects"); len(r.items()) != 1 {
		t.Errorf("projects after approval: %s", r.raw)
	}

	// Off (the default): no flag, and both routes say so.
	cfg.Signup = false
	h2, _ := buildHandler(cfg, db, store, metric.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	off := httptest.NewServer(h2)
	t.Cleanup(off.Close)
	c := newClient(t, off)
	if r := c.bootstrap(); r.body["signup"] != false {
		t.Errorf("bootstrap with signup off: %s", r.raw)
	}
	if r := c.post("/v1/session/signup", form("late@example.com")); r.code != 404 || r.errCode() != "SIGNUP_OFF" {
		t.Errorf("signup off: %d %s", r.code, r.raw)
	}
	if r := c.post("/v1/session/signup/complete", map[string]any{"token": "atp_sgn_x", "password": "a long enough passphrase"}); r.code != 404 || r.errCode() != "SIGNUP_OFF" {
		t.Errorf("complete with signup off: %d %s", r.code, r.raw)
	}
}
