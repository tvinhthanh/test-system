package main

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
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

// ROI over HTTP (§6.8.3, QĐ-13): every viewer reads it; without a measured
// baseline it says so (null savings, never a default); only project.configure
// records a baseline, as the session user; months are validated.
func TestROIOverHTTP(t *testing.T) {
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
	viewer := signIn("vi@acme.example", authz.RoleViewer)
	member := signIn("mo@acme.example", authz.RoleMember)
	maint := signIn("ma@acme.example", authz.RoleMaintainer)
	base := "/v1/projects/" + string(project.ID)

	// No baseline: the numbers that exist, and an explicit null for savings.
	r := viewer.get(base + "/roi")
	if r.code != 200 || r.body["baseline"] != nil || r.body["savedHours"] != nil || r.body["month"] != time.Now().UTC().Format("2006-01") {
		t.Fatalf("roi without baseline: %d %s", r.code, r.raw)
	}
	if r.body["triageCoverage"] == nil || r.body["untriagedRate"] == nil || r.body["fewTriaged"] != true || r.body["minTriaged"] != float64(10) {
		t.Errorf("rule 1 fields missing: %s", r.raw)
	}

	body := map[string]any{
		"kind": "prospective", "failures": 14, "medianMinutes": 180, "p90Minutes": 1560,
		"rerunPassRate": 0.3, "flakyInvestigationMinutes": 25,
		"periodFrom": "2026-08-01", "periodTo": "2026-08-14", "note": "2 weeks, 4-cell form",
	}
	if r := member.post(base+"/roi/baselines", body); r.code != 403 {
		t.Errorf("member records a baseline: %d %s", r.code, r.raw)
	}
	if r := maint.post(base+"/roi/baselines", map[string]any{"kind": "guess", "failures": 1, "medianMinutes": 5, "periodFrom": "2026-08-01", "periodTo": "2026-08-02"}); r.code != 400 {
		t.Errorf("bad kind: %d %s", r.code, r.raw)
	}
	if r := maint.post(base+"/roi/baselines", map[string]any{"kind": "prospective", "failures": 1, "medianMinutes": 5, "periodFrom": "01/08/2026", "periodTo": "2026-08-02"}); r.code != 400 {
		t.Errorf("bad date: %d %s", r.code, r.raw)
	}
	created := maint.post(base+"/roi/baselines", body)
	if created.code != 201 || created.body["enteredBy"] != "ma@acme.example" || created.body["medianMinutes"] != float64(180) || created.body["flakyInvestigationMinutes"] != float64(25) {
		t.Fatalf("maintainer records: %d %s", created.code, created.raw)
	}

	r = viewer.get(base + "/roi")
	if b, _ := r.body["baseline"].(map[string]any); b == nil || b["id"] != created.body["id"] || r.body["savedHours"] != float64(0) {
		t.Errorf("roi with a baseline and no failures this month: %s", r.raw)
	}
	if list := viewer.get(base + "/roi/baselines"); len(list.items()) != 1 {
		t.Errorf("baselines: %s", list.raw)
	}
	for _, bad := range []string{"2026-13", "Sept", time.Now().UTC().AddDate(0, 2, 0).Format("2006-01")} {
		if r := viewer.get(base + "/roi?month=" + bad); r.code != 400 || r.errCode() != "INVALID_QUERY" {
			t.Errorf("month=%s: %d %s", bad, r.code, r.raw)
		}
	}
	if r := viewer.get(base + "/roi?month=2026-08"); r.code != 200 || r.body["month"] != "2026-08" {
		t.Errorf("a past month: %d %s", r.code, r.raw)
	}
}
