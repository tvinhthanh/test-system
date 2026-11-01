package main

import (
	"context"
	"fmt"
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

// The bodies the contract allows reach the service: a plan scope of up to
// 5000 case ids and an execution of up to 200 step results of 4000 bytes
// each are far past the 16 KiB every small form is held to. Before the
// limits were per route, a scope of ~420 cases or a few long "actual"
// notes answered 400 "malformed JSON".
func TestPlanAndExecutionBodiesUpToTheContract(t *testing.T) {
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
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Testing")
	project, _ := ten.CreateProject(ctx, ws.ID, "Manual", "")
	u, err := az.CreateUser(ctx, "tester@acme.example")
	if err != nil {
		t.Fatal(err)
	}
	if err := az.SetPassword(ctx, u.ID, "tester passphrase", ""); err != nil {
		t.Fatal(err)
	}
	if err := az.VerifyEmail(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := az.GrantRole(ctx, u.ID, authz.ScopeRef{Level: authz.LevelProject, ID: string(project.ID)}, authz.RoleMember); err != nil {
		t.Fatal(err)
	}
	web := newClient(t, srv)
	web.bootstrap()
	if r := web.post("/v1/session", map[string]string{"email": "tester@acme.example", "password": "tester passphrase"}); r.code != 200 {
		t.Fatalf("login: %d %s", r.code, r.raw)
	}
	web.bootstrap()
	base := "/v1/projects/" + string(project.ID)

	// Ten steps first (p0 sorts it to the top of the list), then 499 more.
	steps := make([]map[string]string, 10)
	for i := range steps {
		steps[i] = map[string]string{"action": fmt.Sprintf("step %d", i+1), "expected": "works"}
	}
	stepped := web.post(base+"/test-cases", map[string]any{"title": "Long manual case", "priority": "p0", "steps": steps})
	if stepped.code != 201 {
		t.Fatalf("create case: %d %s", stepped.code, stepped.raw)
	}
	var sheet strings.Builder
	sheet.WriteString("key,title\n")
	for i := 1; i < 500; i++ {
		fmt.Fprintf(&sheet, "TC-%d,Case %d\n", i, i)
	}
	if r := web.post(base+"/test-cases/import", map[string]string{"csv": sheet.String()}); r.code != 200 || r.body["created"] != float64(499) {
		t.Fatalf("import: %d %s", r.code, r.raw)
	}
	list := web.get(base + "/test-cases?limit=500")
	ids := make([]string, 0, 500)
	for _, it := range list.items() {
		ids = append(ids, it.(map[string]any)["id"].(string))
	}
	if len(ids) != 500 || ids[0] != stepped.body["id"] {
		t.Fatalf("listed %d cases, first %v", len(ids), ids[0])
	}

	plan := web.post(base+"/test-plans", map[string]any{"name": "Regression", "caseIds": ids})
	if plan.code != 201 || len(plan.body["cases"].([]any)) != 500 {
		t.Fatalf("plan of 500 cases (%d bytes of ids): %d %.200s", 39*len(ids), plan.code, plan.raw)
	}
	planID := plan.body["id"].(string)
	patched := web.do("PATCH", base+"/test-plans/"+planID, map[string]any{"caseIds": ids[:450], "expectedUpdatedAt": plan.body["updatedAt"]}, nil)
	if patched.code != 200 || len(patched.body["cases"].([]any)) != 450 {
		t.Fatalf("scope of 450 cases: %d %.200s", patched.code, patched.raw)
	}

	// 10 × 2000 bytes of observed result: well inside 200 × 4000.
	results := make([]map[string]any, 10)
	for i := range results {
		results[i] = map[string]any{"stepIndex": i, "status": "passed", "actual": strings.Repeat("a", 2000)}
	}
	exec := web.post(base+"/test-plans/"+planID+"/executions", map[string]any{"testCaseId": ids[0], "status": "passed", "note": strings.Repeat("n", 4000), "stepResults": results})
	if exec.code != 201 || len(exec.body["stepResults"].([]any)) != 10 {
		t.Fatalf("execution with 20 KB of step results: %d %.200s", exec.code, exec.raw)
	}

	// Past the route's own bound the answer says so, not "malformed JSON".
	huge := web.post(base+"/test-plans/"+planID+"/executions", map[string]any{"testCaseId": ids[0], "status": "passed", "note": strings.Repeat("n", 3<<20)})
	if huge.code != 413 || huge.errCode() != "PAYLOAD_TOO_LARGE" {
		t.Fatalf("3 MiB execution body: %d %s", huge.code, huge.raw)
	}
}
