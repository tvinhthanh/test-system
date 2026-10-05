package main

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// Project settings (§11.3.2 "Sửa retention/cấu hình project"): a member
// reads them, a maintainer changes them, retention only ever shortens the
// plan, a refused retention lands nothing, and every change is audited.
func TestProjectSettings(t *testing.T) {
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
	project, _ := ten.CreateProject(ctx, ws.ID, "checkout", "")
	if _, err := ten.CreateProject(ctx, ws.ID, "search", ""); err != nil {
		t.Fatal(err)
	}
	signIn := func(email string, role authz.Role) *client {
		u, _ := az.CreateUser(ctx, email)
		_ = az.SetPassword(ctx, u.ID, email+" passphrase", "")
		_ = az.VerifyEmail(ctx, u.ID)
		if _, err := az.GrantRole(ctx, u.ID, authz.ScopeRef{Level: authz.LevelProject, ID: string(project.ID)}, role); err != nil {
			t.Fatal(err)
		}
		c := newClient(t, srv)
		c.bootstrap()
		if r := c.post("/v1/session", map[string]string{"email": email, "password": email + " passphrase"}); r.code != 200 {
			t.Fatalf("login %s: %d %s", email, r.code, r.raw)
		}
		c.csrf, _ = c.get("/v1/session").body["csrfToken"].(string)
		return c
	}
	maint := signIn("mia@acme.example", authz.RoleMaintainer)
	member := signIn("max@acme.example", authz.RoleMember)
	path := "/v1/projects/" + string(project.ID) + "/settings"
	kindDays := func(r resp, kind string) (days, plan float64, override bool) {
		for _, x := range r.body["retention"].([]any) {
			m := x.(map[string]any)
			if m["kind"] == kind {
				return m["days"].(float64), m["planDays"].(float64), m["override"].(bool)
			}
		}
		t.Fatalf("no %s in %s", kind, r.raw)
		return
	}

	got := member.get(path)
	if got.code != 200 || got.body["name"] != "checkout" || got.body["defaultBranch"] != "main" || got.body["plan"] != "free" {
		t.Fatalf("member reads: %d %s", got.code, got.raw)
	}
	if d, p, o := kindDays(got, "video"); d != 7 || p != 7 || o {
		t.Errorf("free video: %v %v %v", d, p, o)
	}
	if r := member.do("PATCH", path, map[string]any{"name": "x"}, nil); r.code != 403 {
		t.Errorf("member changes settings: %d %s", r.code, r.raw)
	}

	// Refused retention: nothing lands, not even the rename in the same body.
	r := maint.do("PATCH", path, map[string]any{"name": "checkout-web", "retention": map[string]int{"video": 8}}, nil)
	if r.code != 400 || r.errCode() != "RETENTION_ABOVE_PLAN" {
		t.Errorf("above plan: %d %s", r.code, r.raw)
	}
	if r := maint.do("PATCH", path, map[string]any{"retention": map[string]int{"pdf": 1}}, nil); r.code != 400 {
		t.Errorf("unknown kind: %d %s", r.code, r.raw)
	}
	if r := maint.do("PATCH", path, map[string]any{"name": "search"}, nil); r.code != 409 {
		t.Errorf("name taken: %d %s", r.code, r.raw)
	}
	if r := maint.do("PATCH", path, map[string]any{"name": "  "}, nil); r.code != 400 {
		t.Errorf("empty name: %d %s", r.code, r.raw)
	}
	if got := maint.get(path); got.body["name"] != "checkout" {
		t.Errorf("a refused patch renamed the project: %s", got.raw)
	}

	r = maint.do("PATCH", path, map[string]any{"name": "checkout-web", "defaultBranch": "develop", "retention": map[string]int{"video": 3, "trace": 0}}, nil)
	if r.code != 200 || r.body["name"] != "checkout-web" || r.body["defaultBranch"] != "develop" {
		t.Fatalf("patch: %d %s", r.code, r.raw)
	}
	if d, p, o := kindDays(r, "video"); d != 3 || p != 7 || !o {
		t.Errorf("video after: %v %v %v", d, p, o)
	}
	if d, _, o := kindDays(r, "trace"); d != 7 || o {
		t.Errorf("trace (0 = plan) after: %v %v", d, o)
	}
	// The same patch again changes nothing and audits nothing.
	if r := maint.do("PATCH", path, map[string]any{"name": "checkout-web", "retention": map[string]int{"video": 3}}, nil); r.code != 200 {
		t.Errorf("repeat: %d %s", r.code, r.raw)
	}
	var audited int
	var details string
	if err := db.Q(ctx).QueryRow(ctx, `SELECT count(*), max(details::text) FROM authz_audit WHERE action = 'project.configure' AND subject_id = $1 AND actor = 'mia@acme.example'`, string(project.ID)).Scan(&audited, &details); err != nil || audited != 1 {
		t.Errorf("audit rows: %d (%v)", audited, err)
	}
	if want := `"retention": {"video": [7, 3]}`; !strings.Contains(details, want) || !strings.Contains(details, `"name": ["checkout", "checkout-web"]`) {
		t.Errorf("audit details: %s", details)
	}
}
