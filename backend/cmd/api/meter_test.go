package main

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/billing"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/query"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// The shadow meter over HTTP (§10.6, QĐ-13): the owner's only; recorded
// days come back summed with §10.6's verdict; another org sees nothing.
func TestOrgMeter(t *testing.T) {
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
	_, _ = ten.CreateProject(ctx, ws.ID, "checkout", "")
	other, _ := ten.CreateOrganization(ctx, "Other", "other")
	signIn := func(email string, scope authz.ScopeRef, role authz.Role) *client {
		u, _ := az.CreateUser(ctx, email)
		_ = az.SetPassword(ctx, u.ID, email+" passphrase", "")
		_ = az.VerifyEmail(ctx, u.ID)
		if _, err := az.GrantRole(ctx, u.ID, scope, role); err != nil {
			t.Fatal(err)
		}
		// Owners must have MFA (§11.2): enrolled here, a recovery code at sign-in.
		var codes []string
		if role == authz.RoleOrgOwner {
			setup, _ := az.BeginMFASetup(ctx, u.ID)
			var err error
			if codes, err = az.EnableMFA(ctx, u.ID, authenticator(t, setup.Secret, time.Now()), ""); err != nil {
				t.Fatal(err)
			}
		}
		c := newClient(t, srv)
		c.bootstrap()
		r := c.post("/v1/session", map[string]string{"email": email, "password": email + " passphrase"})
		if r.code == 202 {
			r = c.post("/v1/session/mfa", map[string]any{"challenge": r.body["challenge"], "code": codes[0]})
		}
		if r.code != 200 {
			t.Fatalf("login %s: %d %s", email, r.code, r.raw)
		}
		c.csrf, _ = c.get("/v1/session").body["csrfToken"].(string)
		return c
	}
	orgScope := authz.ScopeRef{Level: authz.LevelOrganization, ID: string(org.ID)}
	owner := signIn("owner@acme.example", orgScope, authz.RoleOrgOwner)
	admin := signIn("admin@acme.example", orgScope, authz.RoleOrgAdmin)
	stranger := signIn("owner@other.example", authz.ScopeRef{Level: authz.LevelOrganization, ID: string(other.ID)}, authz.RoleOrgOwner)
	path := "/v1/organizations/" + string(org.ID) + "/meter"

	// Nothing recorded yet: an empty history, not ready, and it says why.
	r := owner.get(path)
	if r.code != 200 || r.body["ready"] != false || len(r.body["days"].([]any)) != 0 || r.body["requiredDays"] != float64(28) {
		t.Fatalf("empty meter: %d %s", r.code, r.raw)
	}
	// What the worker does each night, for yesterday.
	if _, err := billing.New(db, meterSource{query.New(db)}).Record(ctx, time.Now().UTC().AddDate(0, 0, -1)); err != nil {
		t.Fatal(err)
	}
	r = owner.get(path + "?days=7")
	days, _ := r.body["days"].([]any)
	if r.code != 200 || len(days) != 1 || days[0].(map[string]any)["day"] != time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02") {
		t.Fatalf("after one night: %d %s", r.code, r.raw)
	}
	if reasons, _ := r.body["reasons"].([]any); len(reasons) == 0 {
		t.Errorf("one day is not four weeks, and the answer must say so: %s", r.raw)
	}

	if r := admin.get(path); r.code != 403 {
		t.Errorf("org admin reads the meter: %d %s", r.code, r.raw)
	}
	if r := stranger.get(path); r.code != 404 {
		t.Errorf("another org's owner: %d %s", r.code, r.raw)
	}
	if r := owner.get(path + "?days=0"); r.code != 400 || r.errCode() != "INVALID_QUERY" {
		t.Errorf("days=0: %d %s", r.code, r.raw)
	}
}
