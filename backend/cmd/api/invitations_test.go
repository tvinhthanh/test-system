package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
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

// outbox catches mail instead of sending it; fail makes the next send fail.
type outbox struct {
	mu   sync.Mutex
	sent []mail.Message
	fail bool
}

func (o *outbox) Send(_ context.Context, m mail.Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.fail {
		o.fail = false
		return errors.New("smtp: 451 try later")
	}
	o.sent = append(o.sent, m)
	return nil
}

func (o *outbox) last(t *testing.T) (mail.Message, string) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.sent) == 0 {
		t.Fatal("no mail sent")
	}
	m := o.sent[len(o.sent)-1]
	token := regexp.MustCompile(`/invite#(atp_inv_[A-Za-z0-9_-]+)`).FindStringSubmatch(m.Text)
	if token == nil {
		t.Fatalf("no invitation link in the mail:\n%s", m.Text)
	}
	return m, token[1]
}

// §11.2 "mời member" with MAIL=smtp: an unknown email is invited, the mail
// carries a fragment link, accepting makes a verified account with the role
// and a session; the token is single use, wrong tokens are counted, and a
// mail that cannot be sent leaves nothing behind.
func TestInvitations(t *testing.T) {
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
		Mail: config.Mail{Enabled: true}, PublicURL: "https://atp.example",
	}
	handler, _ := buildHandler(cfg, db, store, metric.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	ten, az := tenancy.New(db), authz.New(db, []byte(cfg.TokenPepper))
	org, _ := ten.CreateOrganization(ctx, "Acme\r\nBcc: eve@evil.example", "acme")
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Payments")
	p, _ := ten.CreateProject(ctx, ws.ID, "checkout", "")
	admin, _ := az.CreateUser(ctx, "dana@example.com")
	_ = az.SetPassword(ctx, admin.ID, "dana passphrase long", "")
	_ = az.VerifyEmail(ctx, admin.ID)
	_, _ = az.GrantRole(ctx, admin.ID, authz.ScopeRef{Level: authz.LevelWorkspace, ID: string(ws.ID)}, authz.RoleWorkspaceAdmin)
	dana := newClient(t, srv)
	dana.bootstrap()
	dana.post("/v1/session", map[string]string{"email": "dana@example.com", "password": "dana passphrase long"})
	dana.csrf, _ = dana.get("/v1/session").body["csrfToken"].(string)
	if r := dana.get("/v1/session"); r.body["invitations"] != true {
		t.Errorf("bootstrap with mail on: %s", r.raw)
	}
	members := "/v1/projects/" + string(p.ID) + "/members"

	// An unknown email is invited: 202, one mail, a fragment link.
	r := dana.post(members, map[string]any{"email": "New.Person@Example.com", "role": "member"})
	if r.code != 202 || r.body["invitation"].(map[string]any)["email"] != "new.person@example.com" {
		t.Fatalf("invite: %d %s", r.code, r.raw)
	}
	msg, token := box.last(t)
	if msg.To != "new.person@example.com" || strings.ContainsAny(msg.Subject, "\r\n") || !strings.Contains(msg.Text, "https://atp.example/invite#atp_inv_") ||
		!strings.Contains(msg.Text, "dana@example.com invited you to checkout (Payments) on ATP") || !strings.Contains(msg.Text, "as member") {
		t.Errorf("mail: %+v", msg)
	}
	if r := dana.get("/v1/projects/" + string(p.ID) + "/invitations"); len(r.items()) != 1 {
		t.Errorf("open invitations: %s", r.raw)
	}

	// The invitee's side: look up, refuse a weak password, accept, signed in.
	guest := newClient(t, srv)
	guest.bootstrap()
	if r := guest.post("/v1/invitations/lookup", map[string]any{"token": token}); r.code != 200 || r.body["email"] != "new.person@example.com" || r.body["scopeName"] != "checkout (Payments)" || r.body["role"] != "member" {
		t.Errorf("lookup: %d %s", r.code, r.raw)
	}
	if r := guest.post("/v1/invitations/accept", map[string]any{"token": token, "password": "short"}); r.code != 400 || r.errCode() != "INVALID_PASSWORD" {
		t.Errorf("weak password: %d %s", r.code, r.raw)
	}
	if r := guest.post("/v1/invitations/accept", map[string]any{"token": token, "password": "a long enough passphrase"}); r.code != 200 || r.body["user"] == nil {
		t.Fatalf("accept: %d %s", r.code, r.raw)
	}
	guest.csrf, _ = guest.get("/v1/session").body["csrfToken"].(string)
	if r := guest.get("/v1/projects/" + string(p.ID) + "/runs"); r.code != 200 {
		t.Errorf("the new member reads the project: %d %s", r.code, r.raw)
	}
	if u, _ := az.GetUserByEmail(ctx, "new.person@example.com"); u.EmailVerifiedAt.IsZero() || !u.HasPassword {
		t.Errorf("new account: %+v", u)
	}
	// Single use.
	other := newClient(t, srv)
	other.bootstrap()
	if r := other.post("/v1/invitations/accept", map[string]any{"token": token, "password": "another long passphrase"}); r.code != 404 || r.errCode() != "INVITATION_NOT_FOUND" {
		t.Errorf("token reused: %d %s", r.code, r.raw)
	}
	// Someone with an account is not invited: the role is granted directly.
	if r := dana.post(members, map[string]any{"email": "new.person@example.com", "role": "viewer"}); r.code != 200 || r.body["role"] != "viewer" {
		t.Errorf("existing account: %d %s", r.code, r.raw)
	}

	// Revoke: the link stops working.
	dana.post(members, map[string]any{"email": "later@example.com", "role": "viewer"})
	_, t2 := box.last(t)
	invs := dana.get("/v1/projects/" + string(p.ID) + "/invitations").items()
	if len(invs) != 1 {
		t.Fatalf("open invitations: %v", invs)
	}
	id := invs[0].(map[string]any)["id"].(string)
	if r := dana.do("DELETE", "/v1/projects/"+string(p.ID)+"/invitations/"+id, nil, nil); r.code != 204 {
		t.Errorf("revoke: %d %s", r.code, r.raw)
	}
	if r := other.post("/v1/invitations/lookup", map[string]any{"token": t2}); r.code != 404 {
		t.Errorf("revoked token: %d %s", r.code, r.raw)
	}

	// A mail that cannot be sent withdraws the invitation.
	box.fail = true
	if r := dana.post(members, map[string]any{"email": "bounce@example.com", "role": "viewer"}); r.code != 502 || r.errCode() != "MAIL_FAILED" {
		t.Errorf("mail failure: %d %s", r.code, r.raw)
	}
	if r := dana.get("/v1/projects/" + string(p.ID) + "/invitations"); len(r.items()) != 0 {
		t.Errorf("invitation left behind after a failed mail: %s", r.raw)
	}

	// Owner invitations are the owners' (§11.3.1), and the org name — with a
	// CR/LF in it — never reaches a header.
	orgMembers := "/v1/organizations/" + string(org.ID) + "/members"
	if r := dana.post(orgMembers, map[string]any{"email": "boss@example.com", "role": "org_owner"}); r.code != 404 {
		t.Errorf("workspace admin inviting at the org: %d %s", r.code, r.raw)
	}
	_, _ = az.GrantRole(ctx, admin.ID, authz.ScopeRef{Level: authz.LevelOrganization, ID: string(org.ID)}, authz.RoleOrgAdmin)
	if r := dana.post(orgMembers, map[string]any{"email": "boss@example.com", "role": "org_owner"}); r.code != 403 || r.errCode() != "OWNER_ONLY" {
		t.Errorf("org admin inviting an owner: %d %s", r.code, r.raw)
	}
	if r := dana.post(orgMembers, map[string]any{"email": "ops@example.com", "role": "org_admin"}); r.code != 202 {
		t.Fatalf("org admin invite: %d %s", r.code, r.raw)
	}
	if m, _ := box.last(t); strings.ContainsAny(m.Subject, "\r\n") {
		t.Errorf("subject from an org name with CR/LF: %q", m.Subject)
	}

	// Guessing tokens is counted like guessing passwords.
	g := newClient(t, srv)
	g.bootstrap()
	var last resp
	for i := 0; i < 6; i++ {
		last = g.post("/v1/invitations/lookup", map[string]any{"token": "atp_inv_guess"})
	}
	if last.code != 429 {
		t.Errorf("sixth wrong token: %d %s", last.code, last.raw)
	}
	// Locked out, even the cheap refusal waits: nothing is hashed (argon2,
	// 19 MiB) for a client the limit has stopped — the 28-09 review's finding.
	if r := g.post("/v1/invitations/accept", map[string]any{"token": "atp_inv_guess", "password": "short"}); r.code != 429 {
		t.Errorf("accept while locked: %d %s, want 429 before any password work", r.code, r.raw)
	}

	// The record.
	audit, _ := az.ListAudit(ctx, "", 50)
	count := map[string]int{}
	for _, a := range audit {
		count[a.Action]++
	}
	if count["invitation.create"] != 4 || count["invitation.accept"] != 1 || count["invitation.revoke"] != 2 {
		t.Errorf("audit: %v", count)
	}
}

// Mail off: nothing changes — an unknown email is USER_NOT_FOUND.
func TestInvitationsNeedMail(t *testing.T) {
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
	u, _ := az.CreateUser(ctx, "dana@example.com")
	_ = az.SetPassword(ctx, u.ID, "dana passphrase long", "")
	_ = az.VerifyEmail(ctx, u.ID)
	_, _ = az.GrantRole(ctx, u.ID, authz.ScopeRef{Level: authz.LevelWorkspace, ID: string(ws.ID)}, authz.RoleWorkspaceAdmin)
	c := newClient(t, srv)
	c.bootstrap()
	c.post("/v1/session", map[string]string{"email": "dana@example.com", "password": "dana passphrase long"})
	c.csrf, _ = c.get("/v1/session").body["csrfToken"].(string)
	if r := c.get("/v1/session"); r.body["invitations"] != false {
		t.Errorf("bootstrap with mail off: %s", r.raw)
	}
	if r := c.post("/v1/projects/"+string(p.ID)+"/members", map[string]any{"email": "new@example.com", "role": "member"}); r.code != 404 || r.errCode() != "USER_NOT_FOUND" {
		t.Errorf("mail off: %d %s", r.code, r.raw)
	}
}

// waitMail waits for the n-th mail (the reset mail leaves in the background).
func (o *outbox) waitMail(t *testing.T, n int) mail.Message {
	t.Helper()
	for i := 0; i < 100; i++ {
		o.mu.Lock()
		if len(o.sent) >= n {
			m := o.sent[n-1]
			o.mu.Unlock()
			return m
		}
		o.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("mail %d never arrived", n)
	return mail.Message{}
}

func (o *outbox) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.sent)
}

var resetLink = regexp.MustCompile(`/reset#(atp_rst_[A-Za-z0-9_-]+)`)

// §11.2 forgot password: the same answer for known and unknown emails, a
// single-use 30-minute link, every session revoked, 5 requests per email
// per 15 minutes, off without mail.
func TestPasswordReset(t *testing.T) {
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
		Mail: config.Mail{Enabled: true}, PublicURL: "https://atp.example",
	}
	handler, _ := buildHandler(cfg, db, store, metric.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	az := authz.New(db, []byte(cfg.TokenPepper))
	u, _ := az.CreateUser(ctx, "rita@example.com")
	_ = az.SetPassword(ctx, u.ID, "the old passphrase", "")
	_ = az.VerifyEmail(ctx, u.ID)
	signIn := func(pw string) (*client, resp) {
		c := newClient(t, srv)
		c.bootstrap()
		return c, c.post("/v1/session", map[string]string{"email": "rita@example.com", "password": pw})
	}
	old, r := signIn("the old passphrase")
	if r.code != 200 {
		t.Fatalf("login: %d", r.code)
	}
	anon := newClient(t, srv)
	anon.bootstrap()
	if r := anon.get("/v1/session"); r.body["passwordReset"] != true {
		t.Errorf("bootstrap: %s", r.raw)
	}

	// Known and unknown emails get the same answer; only one gets a mail.
	unknown := anon.post("/v1/session/password/forgot", map[string]any{"email": "nobody@example.com"})
	known := anon.post("/v1/session/password/forgot", map[string]any{"email": " Rita@Example.com "})
	if unknown.code != 202 || known.code != 202 || unknown.raw != known.raw {
		t.Errorf("answers differ: %d %s / %d %s", unknown.code, unknown.raw, known.code, known.raw)
	}
	m := box.waitMail(t, 1)
	first := resetLink.FindStringSubmatch(m.Text)
	if m.To != "rita@example.com" || first == nil || !strings.Contains(m.Text, "https://atp.example/reset#atp_rst_") {
		t.Fatalf("reset mail: %+v", m)
	}
	time.Sleep(100 * time.Millisecond)
	if box.count() != 1 {
		t.Errorf("mails: %d, want 1 (none for the unknown email)", box.count())
	}
	// Asking again replaces the link.
	anon.post("/v1/session/password/forgot", map[string]any{"email": "rita@example.com"})
	second := resetLink.FindStringSubmatch(box.waitMail(t, 2).Text)
	if r := anon.post("/v1/session/password/reset", map[string]any{"token": first[1], "password": "a brand new passphrase"}); r.code != 404 || r.errCode() != "RESET_NOT_FOUND" {
		t.Errorf("replaced link: %d %s", r.code, r.raw)
	}
	// A weak password spends nothing; a good one resets and signs everyone out.
	if r := anon.post("/v1/session/password/reset", map[string]any{"token": second[1], "password": "short"}); r.code != 400 || r.errCode() != "INVALID_PASSWORD" {
		t.Errorf("weak password: %d %s", r.code, r.raw)
	}
	if r := anon.post("/v1/session/password/reset", map[string]any{"token": second[1], "password": "a brand new passphrase"}); r.code != 204 {
		t.Fatalf("reset: %d %s", r.code, r.raw)
	}
	if r := old.get("/v1/projects"); r.code != 401 {
		t.Errorf("old session after reset: %d", r.code)
	}
	if _, r := signIn("the old passphrase"); r.code != 401 {
		t.Errorf("old password after reset: %d", r.code)
	}
	if _, r := signIn("a brand new passphrase"); r.code != 200 {
		t.Errorf("new password: %d %s", r.code, r.raw)
	}
	if r := anon.post("/v1/session/password/reset", map[string]any{"token": second[1], "password": "yet another passphrase"}); r.code != 404 {
		t.Errorf("link used twice: %d %s", r.code, r.raw)
	}

	// Five requests per email per 15 minutes, counted whether or not the
	// account exists (so the limit tells nothing either).
	flood := newClient(t, srv)
	flood.bootstrap()
	var last resp
	for i := 0; i < 6; i++ {
		last = flood.post("/v1/session/password/forgot", map[string]any{"email": "ghost@example.com"})
	}
	if last.code != 429 {
		t.Errorf("sixth request: %d %s", last.code, last.raw)
	}
	audit, _ := az.ListAudit(ctx, "", 20)
	found := false
	for _, a := range audit {
		found = found || (a.Action == "password.reset" && a.Actor == "rita@example.com")
	}
	if !found {
		t.Errorf("no password.reset audit: %+v", audit)
	}

	// Mail off: no reset by email, and the page is told.
	cfg.Mail.Enabled = false
	h2, _ := buildHandler(cfg, db, store, metric.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	off := httptest.NewServer(h2)
	t.Cleanup(off.Close)
	c := newClient(t, off)
	if r := c.bootstrap(); r.body["passwordReset"] != false {
		t.Errorf("bootstrap with mail off: %s", r.raw)
	}
	if r := c.post("/v1/session/password/forgot", map[string]any{"email": "rita@example.com"}); r.code != 404 || r.errCode() != "PASSWORD_RESET_OFF" {
		t.Errorf("forgot with mail off: %d %s", r.code, r.raw)
	}
}
