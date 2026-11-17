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
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// Environments with an address (QĐ-14): a member records the site under
// test, a viewer only reads it, bad or credentialed URLs are refused, a
// plan shows its environment's address and never points at another
// project's environment, and a closed plan keeps the one it ran against.
func TestEnvironments(t *testing.T) {
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
	other, _ := ten.CreateProject(ctx, ws.ID, "search", "")
	foreignEnv, err := ten.CreateEnvironment(ctx, other.ID, "staging", "https://search.example")
	if err != nil {
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
	tester := signIn("tia@acme.example", authz.RoleMember)
	viewer := signIn("vic@acme.example", authz.RoleViewer)
	base := "/v1/projects/" + string(project.ID)

	if r := viewer.post(base+"/environments", map[string]any{"key": "staging"}); r.code != 403 {
		t.Errorf("viewer adds an environment: %d %s", r.code, r.raw)
	}
	for _, bad := range []map[string]any{
		{"key": "Staging Site", "baseUrl": "https://staging.example"},
		{"key": "staging", "baseUrl": "ftp://staging.example"},
		{"key": "staging", "baseUrl": "staging.example"},
		{"key": "staging", "baseUrl": "https://qa:secret@staging.example"},
	} {
		if r := tester.post(base+"/environments", bad); r.code != 400 || r.errCode() != "INVALID_PAYLOAD" {
			t.Errorf("bad environment %v: %d %s", bad, r.code, r.raw)
		}
	}
	created := tester.post(base+"/environments", map[string]any{"key": "staging", "baseUrl": " HTTPS://Staging.Example/shop/ "})
	if created.code != 201 || created.body["baseUrl"] != "https://staging.example/shop" || created.body["key"] != "staging" {
		t.Fatalf("tester adds staging: %d %s", created.code, created.raw)
	}
	envID := created.body["id"].(string)
	if r := tester.post(base+"/environments", map[string]any{"key": "staging"}); r.code != 409 {
		t.Errorf("duplicate key: %d %s", r.code, r.raw)
	}
	// A reporter-created environment (no address yet) is listed beside it.
	if _, err := ten.EnsureEnvironment(ctx, project.ID, "ci"); err != nil {
		t.Fatal(err)
	}
	list := viewer.get(base + "/environments")
	if list.code != 200 || len(list.items()) != 2 || list.items()[0].(map[string]any)["key"] != "ci" || list.items()[0].(map[string]any)["baseUrl"] != "" {
		t.Fatalf("viewer lists environments: %d %s", list.code, list.raw)
	}
	envPath := base + "/environments/" + envID
	if r := viewer.do("PATCH", envPath, map[string]any{"baseUrl": "https://x.example"}, nil); r.code != 403 {
		t.Errorf("viewer changes the address: %d %s", r.code, r.raw)
	}
	if r := tester.do("PATCH", base+"/environments/"+string(foreignEnv.ID), map[string]any{"baseUrl": "https://x.example"}, nil); r.code != 404 {
		t.Errorf("another project's environment: %d %s", r.code, r.raw)
	}
	if r := tester.do("PATCH", envPath, map[string]any{}, nil); r.code != 400 {
		t.Errorf("patch without baseUrl: %d %s", r.code, r.raw)
	}
	if r := tester.do("PATCH", envPath, map[string]any{"baseUrl": "https://staging2.example"}, nil); r.code != 200 || r.body["baseUrl"] != "https://staging2.example" {
		t.Fatalf("tester moves staging: %d %s", r.code, r.raw)
	}
	var audits int
	if err := db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM authz_audit WHERE action = 'project.configure' AND subject_id = $1`, string(project.ID)).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 2 {
		t.Errorf("audit rows for create + change: %d", audits)
	}

	// Plans carry their environment and its address.
	if r := tester.post(base+"/test-plans", map[string]any{"name": "Wrong env", "environmentId": string(foreignEnv.ID)}); r.code != 400 {
		t.Errorf("plan on another project's environment: %d %s", r.code, r.raw)
	}
	plan := tester.post(base+"/test-plans", map[string]any{"name": "Sprint 12", "environmentId": envID, "caseIds": []string{}})
	env, _ := plan.body["environment"].(map[string]any)
	if plan.code != 201 || env["baseUrl"] != "https://staging2.example" || plan.body["environmentId"] != envID {
		t.Fatalf("plan on staging: %d %s", plan.code, plan.raw)
	}
	planPath := base + "/test-plans/" + plan.body["id"].(string)
	if r := viewer.get(planPath); r.body["environment"].(map[string]any)["key"] != "staging" {
		t.Errorf("plan detail environment: %s", r.raw)
	}
	if r := viewer.get(base + "/test-plans"); r.items()[0].(map[string]any)["environment"].(map[string]any)["baseUrl"] != "https://staging2.example" {
		t.Errorf("plan list environment: %s", r.raw)
	}
	if r := tester.do("PATCH", planPath, map[string]any{"environmentId": ""}, nil); r.code != 200 || r.body["environment"] != nil || r.body["environmentId"] != "" {
		t.Fatalf("plan clears its environment: %d %s", r.code, r.raw)
	}
	if r := tester.do("PATCH", planPath, map[string]any{"environmentId": string(foreignEnv.ID)}, nil); r.code != 400 {
		t.Errorf("plan moved to another project's environment: %d %s", r.code, r.raw)
	}
	if r := tester.do("PATCH", planPath, map[string]any{"environmentId": envID}, nil); r.code != 200 || r.body["environmentId"] != envID {
		t.Fatalf("plan set back on staging: %d %s", r.code, r.raw)
	}
	if err := db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM test_plan_audit WHERE action = 'plan.environment' AND test_plan_id = $1`, plan.body["id"]).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 2 {
		t.Errorf("plan.environment audit rows: %d", audits)
	}
	if r := tester.do("PATCH", planPath, map[string]any{"status": "closed"}, nil); r.code != 200 {
		t.Fatalf("close plan: %d %s", r.code, r.raw)
	}
	if r := tester.do("PATCH", planPath, map[string]any{"environmentId": ""}, nil); r.code != 409 {
		t.Errorf("closed plan changes environment: %d %s", r.code, r.raw)
	}
}
